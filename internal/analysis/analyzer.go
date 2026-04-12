package analysis

import (
	"log"
	"sync"
	"time"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/config"
	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/internal/library"
	"github.com/janyksteenbeek/boom/pkg/model"
)

const (
	defaultWorkers  = 2
	queueBufferSize = 256
)

type analysisJob struct {
	track      model.Track
	samples    [][2]float32 // nil = decode from file
	sampleRate int
	deckID     int // 0=batch, 1/2=deck
}

// Service manages asynchronous track analysis with a worker pool.
type Service struct {
	bus    *event.Bus
	store  *library.Store
	cfg    *config.Config
	queue  chan analysisJob
	stopCh chan struct{}
	wg     sync.WaitGroup

	// Batch tracking
	mu           sync.Mutex
	batchTotal   int
	batchCurrent int
	cancelled    bool
}

// NewService creates and starts the analysis service.
func NewService(bus *event.Bus, store *library.Store, cfg *config.Config) *Service {
	s := &Service{
		bus:    bus,
		store:  store,
		cfg:    cfg,
		queue:  make(chan analysisJob, queueBufferSize),
		stopCh: make(chan struct{}),
	}

	// Start workers
	for i := 0; i < defaultWorkers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	s.subscribeEvents()
	return s
}

// Stop drains the queue and waits for workers to finish.
func (s *Service) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Service) subscribeEvents() {
	// Handle batch analysis requests
	s.bus.Subscribe(event.TopicAnalysis, func(ev event.Event) error {
		switch ev.Action {
		case event.ActionAnalyzeRequest:
			tracks, ok := ev.Payload.([]model.Track)
			if !ok {
				return nil
			}
			go s.analyzeBatch(tracks)

		case event.ActionAnalyzeCancel:
			s.mu.Lock()
			s.cancelled = true
			s.mu.Unlock()
			// Drain remaining queue items
			for {
				select {
				case <-s.queue:
				default:
					return nil
				}
			}
		}
		return nil
	})

	// Auto-analyze on deck load — fires on ActionTrackDecoded (not
	// TrackLoaded) so we get a reference to the deck's fully-decoded PCM
	// buffer and avoid a second file decode pass in the analysis worker.
	s.bus.Subscribe(event.TopicEngine, func(ev event.Event) error {
		if ev.Action != event.ActionTrackDecoded {
			return nil
		}
		if !s.cfg.AutoAnalyzeOnDeckLoad {
			return nil
		}
		payload, ok := ev.Payload.(*event.TrackDecodedPayload)
		if !ok || payload == nil || payload.Track == nil {
			return nil
		}
		track := payload.Track
		// Skip if already analyzed
		if track.BPM > 0 && track.Key != "" {
			return nil
		}
		go s.submitJob(analysisJob{
			track:      *track,
			samples:    payload.Samples,
			sampleRate: payload.SampleRate,
			deckID:     ev.DeckID,
		})
		return nil
	})
}

func (s *Service) analyzeBatch(tracks []model.Track) {
	// Filter out already-analyzed tracks
	var toAnalyze []model.Track
	for _, t := range tracks {
		if t.BPM == 0 || t.Key == "" {
			toAnalyze = append(toAnalyze, t)
		}
	}
	if len(toAnalyze) == 0 {
		return
	}

	s.mu.Lock()
	s.batchTotal = len(toAnalyze)
	s.batchCurrent = 0
	s.cancelled = false
	s.mu.Unlock()

	for _, t := range toAnalyze {
		s.mu.Lock()
		if s.cancelled {
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()

		s.submitJob(analysisJob{
			track:  t,
			deckID: 0, // batch
		})
	}
}

func (s *Service) submitJob(job analysisJob) {
	select {
	case s.queue <- job:
	case <-s.stopCh:
	}
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case job := <-s.queue:
			s.processJob(job)
		case <-s.stopCh:
			return
		}
	}
}

func (s *Service) processJob(job analysisJob) {
	samples := job.samples
	sampleRate := job.sampleRate

	// If no PCM provided, decode from file
	if samples == nil {
		var err error
		samples, sampleRate, err = decodeTrackPCM(job.track.Path)
		if err != nil {
			log.Printf("analysis: decode %s: %v", job.track.Path, err)
			return
		}
	}

	log.Printf("analysis: analyzing '%s' (%d samples @ %d Hz)", job.track.Title, len(samples), sampleRate)

	// Run BPM detection with configured DJ range
	rangeMin, rangeMax := s.cfg.ResolveBPMRange()
	bpm := DetectBPM(samples, sampleRate, rangeMin, rangeMax)

	// Run key detection
	key := DetectKey(samples, sampleRate)

	// Compute beat grid
	beatGrid := ComputeBeatGrid(samples, sampleRate, bpm)

	// Compute track gain (dB offset vs. target loudness)
	gain := ComputeTrackGain(samples)

	log.Printf("analysis: '%s' -> BPM=%.2f Key=%s gain=%+.1fdB beats=%d",
		job.track.Title, bpm, key, gain, len(beatGrid))

	// Persist to database
	if err := s.store.UpdateAnalysis(job.track.ID, bpm, key, beatGrid, gain, time.Now()); err != nil {
		log.Printf("analysis: persist %s: %v", job.track.ID, err)
	}

	// Publish completion event synchronously — analysis runs on its own
	// goroutine so this won't block the audio thread, and avoids being
	// dropped when the async channel is saturated by position updates.
	s.bus.Publish(event.Event{
		Topic:  event.TopicAnalysis,
		Action: event.ActionAnalyzeComplete,
		DeckID: job.deckID,
		Payload: &event.AnalysisResult{
			TrackID:  job.track.ID,
			BPM:      bpm,
			Key:      key,
			Gain:     gain,
			BeatGrid: beatGrid,
			DeckID:   job.deckID,
		},
	})

	// Update batch progress
	if job.deckID == 0 {
		s.mu.Lock()
		s.batchCurrent++
		current := s.batchCurrent
		total := s.batchTotal
		done := current >= total
		s.mu.Unlock()

		s.bus.PublishAsync(event.Event{
			Topic:  event.TopicAnalysis,
			Action: event.ActionAnalyzeProgress,
			Payload: &event.AnalysisProgress{
				Current: current,
				Total:   total,
				TrackID: job.track.ID,
			},
		})

		if done {
			s.bus.PublishAsync(event.Event{
				Topic:  event.TopicAnalysis,
				Action: event.ActionAnalyzeBatchDone,
			})
		}
	}

	// Also publish BPM/Key detected for deck updates (sync for reliability)
	if job.deckID > 0 {
		if bpm > 0 {
			s.bus.Publish(event.Event{
				Topic:  event.TopicEngine,
				Action: event.ActionBPMDetected,
				DeckID: job.deckID,
				Value:  bpm,
			})
		}
		if key != "" {
			s.bus.Publish(event.Event{
				Topic:   event.TopicEngine,
				Action:  event.ActionKeyDetected,
				DeckID:  job.deckID,
				Payload: key,
			})
		}
	}
}

// decodeTrackPCM decodes an audio file to stereo float32 PCM.
func decodeTrackPCM(path string) ([][2]float32, int, error) {
	src, format, err := audio.Decode(path)
	if err != nil {
		return nil, 0, err
	}

	sampleRate := int(format.SampleRate)

	// Decode to float32 PCM
	estimatedSamples := src.Len()
	pcm := make([][2]float32, 0, estimatedSamples+4096)

	buf := make([][2]float64, 8192)
	for {
		n, ok := src.Stream(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				pcm = append(pcm, [2]float32{float32(buf[i][0]), float32(buf[i][1])})
			}
		}
		if !ok {
			break
		}
	}
	src.Close()

	return pcm, sampleRate, nil
}

// AnalyzeWithPCM submits a track for analysis using pre-decoded PCM data.
// Used when the deck already has the PCM buffer available.
func (s *Service) AnalyzeWithPCM(track *model.Track, samples [][2]float32, sampleRate int, deckID int) {
	if track.BPM > 0 && track.Key != "" {
		return
	}
	s.submitJob(analysisJob{
		track:      *track,
		samples:    samples,
		sampleRate: sampleRate,
		deckID:     deckID,
	})
}

