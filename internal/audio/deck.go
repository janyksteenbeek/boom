package audio

import (
	"log"
	"math"
	"sync/atomic"

	"github.com/gopxl/beep/v2"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// pcmBuffer holds decoded audio samples at the target sample rate.
//
// Two length fields cover the streaming-decode lifecycle:
//
//   - totalLen is the expected final length of the track, known at LoadTrack
//     time from the decoder's reported length (adjusted for resample ratio).
//     It is fixed for the lifetime of the buffer and is what duration /
//     normalization math (loop, cue, posSnapshot) uses so the UI doesn't
//     jump while more samples stream in.
//
//   - decodedLen is what has actually been written so far. It grows
//     monotonically as the decode goroutine fills samples and is the only
//     bound the audio thread may read up to. Atomic so Stream() can Load()
//     without a lock.
//
// If the decoder produces more samples than estimated (VBR MP3 etc.), the
// write path allocates a new, larger pcmBuffer and swaps it via d.pcm.Store
// — the in-flight audio callback continues reading from the old snapshot
// until its next Stream() call.
type pcmBuffer struct {
	samples    [][2]float32 // pre-allocated to at least totalLen
	totalLen   int          // expected total samples (fixed)
	decodedLen atomic.Int64 // samples written so far (grows)
}

// Len returns the number of samples currently decoded (safe for audio thread).
func (p *pcmBuffer) Len() int { return int(p.decodedLen.Load()) }

// Total returns the expected total sample count for the track.
// Duration math should use this, not Len(), so the UI doesn't rescale.
func (p *pcmBuffer) Total() int {
	if p.totalLen > 0 {
		return p.totalLen
	}
	return p.Len()
}

// WaveformCache is satisfied by library.Store. The audio package defines
// the interface here so it doesn't need to import library (which would
// create a cycle: library → model, audio → library → model).
//
// GetWaveform returns nil if no valid cache entry exists. The returned
// WaveformData is owned by the caller.
type WaveformCache interface {
	GetWaveform(trackID string, sampleRate int, mtime int64) (*WaveformData, bool)
	PutWaveform(trackID string, data *WaveformData, mtime int64) error
}

// Jog wheel decay tunables. Halflives are kept as constants — they affect
// the *feel* of platter inertia and pitch bend rebound and rarely need user
// tuning. The per-tick gains are exposed as per-deck atomics so the engine
// can apply user settings.
const (
	jogScratchHalflifeMs = 100.0 // platter inertia decay
	jogPitchHalflifeMs   = 220.0 // pitch bend rubber-band

	defaultJogScratchSensitivity = 0.4 // dimensionless gain (matches tempo units)
	defaultJogPitchSensitivity   = 0.04
)

// Deck represents one playback channel.
// Audio is decoded fully upfront into a PCM buffer.
// The audio callback only reads from this buffer — zero allocations, zero I/O.
type Deck struct {
	id         int
	sampleRate int
	bus        *event.Bus

	// PCM buffer — set on LoadTrack, read from audio thread
	pcm atomic.Pointer[pcmBuffer]

	// Playback position — ONLY modified by audio thread in Stream()
	pos  int
	fpos float64 // fractional position accumulator for tempo (drift-free)

	// EQ — lock-free (uses atomic coefficient swap internally)
	eq *ThreeBandEQ

	// Beat FX — lock-free effect processor (echo/flanger/reverb)
	beatFX *BeatFX

	// Smoothed volume & gain — Set() from any goroutine, Tick() from audio thread
	volume SmoothParam
	gain   SmoothParam // Trim/gain knob: 0.0-2.0 multiplier (1.0 = unity)

	// Fade envelope for click-free play/stop — ONLY accessed from audio thread
	fade FadeEnvelope

	// Atomic control values
	playing   atomic.Bool
	tempoBits atomic.Uint64 // float64 as bits, ratio

	// Cue point state — written by UI/MIDI threads via the engine.
	// cuePoint holds the manual cue (persisted to DB); negative = unset.
	// fallbackCue is the auto-cue / track-start position computed on load
	// and is always a valid value (0..1).
	cuePoint    atomic.Uint64 // float64 bits, normalized 0..1; negative = unset
	fallbackCue atomic.Uint64 // float64 bits, 0..1
	cueHeld     atomic.Bool   // CUE button currently held down
	cuePreview  atomic.Bool   // playing because of CUE-hold preview (release → snap back)

	// Loop state. Positions are normalized 0..1 stored as float64 bits.
	// A NaN in loopStart/loopEnd means "unset". loopActive controls whether
	// Stream() wraps playback between the points. loopBeats is the beat
	// length used for halve/double math (0 = manual loop).
	loopStart  atomic.Uint64
	loopEnd    atomic.Uint64
	loopBeats  atomic.Uint64
	loopActive atomic.Bool

	// Jog wheel state — written by engine, consumed by Stream().
	// vinylMode true = top touch enables scratch; false = all rotation is pitch bend.
	// jogScratchVel is samples-per-output-sample (signed, decays each block).
	// jogPitchOffset is an additive tempo delta (signed, decays each block).
	// prevPlayingJog snapshots playing state on touch-down so release can restore it.
	vinylMode          atomic.Bool
	jogTouched         atomic.Bool
	jogScratchVel      atomic.Uint64
	jogPitchOffset     atomic.Uint64
	jogScratchGainBits atomic.Uint64 // float64 bits, samples-per-sample per tick
	jogPitchGainBits   atomic.Uint64 // float64 bits, tempo delta per tick
	prevPlayingJog     atomic.Bool

	// Pending commands from non-audio threads (written by UI/MIDI, consumed by audio thread)
	pendingSeek  atomic.Uint64 // float64 bits; NaN = no pending seek
	pendingFade  atomic.Int32  // 0=none, 1=fade-in, 2=fade-out
	pendingReset atomic.Bool   // set by LoadTrack, audio thread resets all state

	// Position snapshot for UI (written by audio thread, read by UI/position loop)
	posSnapshot atomic.Uint64 // float64 bits, fractional position 0.0-1.0

	track    atomic.Pointer[model.Track]
	waveform atomic.Pointer[WaveformData]

	// peakBits is the latest envelope-followed output peak for this deck,
	// 0..1 normalized, written by the audio thread in Stream() and read by
	// the UI / LED feedback loops. Smoothed with a per-block exponential
	// decay so meters don't strobe between blocks.
	peakBits atomic.Uint64

	// decodeDone is closed by the streaming decode goroutine once the full
	// PCM buffer is available. A new channel is installed on every LoadTrack
	// call. Stored as a pointer so callers snapshot the channel for *their*
	// specific load and aren't racing with a fresh channel installed by a
	// subsequent load.
	decodeDone atomic.Pointer[chan struct{}]

	// Optional cache for pre-computed waveform peaks. If non-nil, LoadTrack
	// will consult this before running the (relatively expensive) full
	// waveform generation pass.
	wfCache WaveformCache
}

func NewDeck(id int, sampleRate int, bus *event.Bus, wfCache WaveformCache) *Deck {
	d := &Deck{
		id:         id,
		sampleRate: sampleRate,
		bus:        bus,
		eq:         NewThreeBandEQ(sampleRate),
		beatFX:     NewBeatFX(sampleRate),
		fade:       NewFadeEnvelope(sampleRate, 0.01),
		wfCache:    wfCache,
	}
	d.volume.Init(0.8, sampleRate, 0.005)
	d.gain.Init(1.0, sampleRate, 0.005)
	d.storeFloat(&d.tempoBits, 1.0)
	d.storeFloat(&d.pendingSeek, math.NaN()) // sentinel: no pending seek
	d.storeFloat(&d.cuePoint, -1)            // sentinel: no cue set
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
	d.storeFloat(&d.jogScratchVel, 0)
	d.storeFloat(&d.jogPitchOffset, 0)
	d.storeFloat(&d.jogScratchGainBits, defaultJogScratchSensitivity)
	d.storeFloat(&d.jogPitchGainBits, defaultJogPitchSensitivity)
	d.vinylMode.Store(true) // default: classic scratch behavior
	return d
}

func (d *Deck) ID() int                 { return d.id }
func (d *Deck) SampleRate() int          { return d.sampleRate }
func (d *Deck) Track() *model.Track     { return d.track.Load() }
func (d *Deck) Waveform() *WaveformData { return d.waveform.Load() }

// PeakLevel returns the latest envelope-followed output peak (0..1).
// Written from the audio thread, safe to read from any goroutine.
func (d *Deck) PeakLevel() float64 {
	return math.Float64frombits(d.peakBits.Load())
}

// PCMSamples returns a read-only reference to the decoded PCM buffer.
// The returned slice must not be modified. Returns nil if no track is loaded.
// The slice reflects samples decoded so far — during streaming decode the
// caller may see a shorter buffer than the full track.
func (d *Deck) PCMSamples() [][2]float32 {
	p := d.pcm.Load()
	if p == nil {
		return nil
	}
	return p.samples[:p.Len()]
}

// DecodeDone returns a channel that is closed when the currently loading
// track has finished streaming its full PCM buffer. Each call to LoadTrack
// installs a fresh channel; call this *after* LoadTrack to obtain the
// channel tied to that specific load.
func (d *Deck) DecodeDone() <-chan struct{} {
	if ch := d.decodeDone.Load(); ch != nil {
		return *ch
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}

// decodeChunkSeconds is how often the streaming decoder publishes an updated
// waveform and flushes the latest sample count to the audio thread. 5 sec is
// a good balance: small enough to feel responsive, large enough that the
// re-normalization flicker on the waveform is infrequent.
const decodeChunkSeconds = 5

// LoadTrack opens the file, pre-allocates a PCM buffer sized for the full
// track, and spawns a goroutine that streams the decoded samples into it.
// LoadTrack itself only blocks long enough to open the file and allocate
// the buffer — playback and waveform updates begin as samples arrive.
//
// Callers that need to wait for the full track (e.g. to compute the
// auto-cue, or kick off whole-track analysis) should snapshot the
// DecodeDone() channel *immediately after* LoadTrack returns and read from
// it; the channel returned for this specific load is installed before
// LoadTrack publishes ActionTrackLoaded.
func (d *Deck) LoadTrack(track *model.Track) error {
	log.Printf("deck %d: opening '%s'...", d.id, track.Title)

	src, format, err := Decode(track.Path)
	if err != nil {
		return err
	}

	// Build offline decode chain: source → resample (if needed)
	var streamer beep.Streamer = src
	needsResample := format.SampleRate != beep.SampleRate(d.sampleRate)
	if needsResample {
		log.Printf("deck %d: resampling %d → %d Hz", d.id, format.SampleRate, d.sampleRate)
		streamer = beep.Resample(4, format.SampleRate, beep.SampleRate(d.sampleRate), src)
	}

	// Estimate total sample count at the target rate. For VBR formats this
	// may be slightly off — the decode loop grows the buffer if needed.
	estimatedSamples := src.Len()
	if needsResample {
		estimatedSamples = int(float64(estimatedSamples) * float64(d.sampleRate) / float64(format.SampleRate))
	}
	if estimatedSamples < 1 {
		estimatedSamples = d.sampleRate // 1 sec fallback
	}
	// Pre-allocate with generous headroom so VBR overruns don't trigger a
	// realloc + swap on every track.
	capacity := estimatedSamples + estimatedSamples/20 + 8192
	pcm := &pcmBuffer{
		samples:  make([][2]float32, capacity),
		totalLen: estimatedSamples,
	}

	// Stop playback BEFORE swapping the PCM buffer so the callback never
	// reads the new buffer at the old position (race window elimination).
	d.playing.Store(false)
	d.pendingFade.Store(0)                   // clear any pending fade
	d.storeFloat(&d.pendingSeek, math.NaN()) // clear any pending seek
	d.cueHeld.Store(false)
	d.cuePreview.Store(false)
	// Discard any loop from the previous track — loop points are per-session
	// and anchored to absolute sample indices of a specific PCM buffer.
	d.loopActive.Store(false)
	d.storeFloat(&d.loopStart, math.NaN())
	d.storeFloat(&d.loopEnd, math.NaN())
	d.storeFloat(&d.loopBeats, 0)
	// cuePoint is set by the engine after decode completes (from track.CuePoint).
	d.pcm.Store(pcm)
	d.track.Store(track)
	d.waveform.Store(nil)
	d.pendingReset.Store(true) // signal audio thread to reset pos/fade/EQ

	// Install a fresh decode-done channel for this load. The caller will
	// snapshot it via DecodeDone() right after we return.
	doneCh := make(chan struct{})
	d.decodeDone.Store(&doneCh)

	// Check cache first — if we have the waveform on disk, skip the live
	// builder entirely and just publish it.
	var cachedWF *WaveformData
	if d.wfCache != nil && track.ID != "" {
		if cached, ok := d.wfCache.GetWaveform(track.ID, d.sampleRate, track.FileMtime); ok && cached != nil {
			cachedWF = cached
			d.waveform.Store(cached)
		}
	}

	d.bus.PublishAsync(event.Event{
		Topic: event.TopicEngine, Action: event.ActionTrackLoaded,
		DeckID: d.id, Payload: track,
	})
	if cachedWF != nil {
		d.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionWaveformReady,
			DeckID: d.id, Payload: cachedWF,
		})
	}

	go d.streamDecode(src, streamer, pcm, track, estimatedSamples, cachedWF != nil, doneCh)
	return nil
}

// streamDecode runs on its own goroutine. It pulls blocks from the beep
// streamer, writes them into the pcmBuffer (growing it if the decoder
// overshoots the estimate), and — once per ~decodeChunkSeconds of audio —
// re-runs the waveform builder and publishes a waveform update. On
// completion it fires ActionTrackDecoded with the finished PCM so the
// analyzer can run whole-track analysis without re-decoding the file, and
// closes doneCh so the engine's post-load setup can proceed.
func (d *Deck) streamDecode(src beep.StreamSeekCloser, streamer beep.Streamer, initial *pcmBuffer, track *model.Track, estimatedSamples int, wfCached bool, doneCh chan struct{}) {
	defer src.Close()
	defer close(doneCh)

	pcm := initial

	var builder *WaveformBuilder
	if !wfCached {
		builder = NewWaveformBuilder(estimatedSamples, d.sampleRate)
	}

	chunkThreshold := d.sampleRate * decodeChunkSeconds
	tmp := make([][2]float64, 8192)

	writePos := 0
	sinceEvent := 0

	for {
		// Guard against another LoadTrack overwriting our buffer pointer —
		// if that happens we abandon this decode without publishing stale
		// data.
		if d.pcm.Load() != pcm {
			return
		}

		n, ok := streamer.Stream(tmp)
		if n > 0 {
			// Grow if the decoder produced more than the estimate.
			if writePos+n > len(pcm.samples) {
				newCap := (writePos + n) * 2
				bigger := &pcmBuffer{
					samples:  make([][2]float32, newCap),
					totalLen: writePos + n, // revise upward — estimate was low
				}
				copy(bigger.samples, pcm.samples[:writePos])
				bigger.decodedLen.Store(int64(writePos))
				if !d.pcm.CompareAndSwap(pcm, bigger) {
					// Someone else swapped the buffer (new LoadTrack). Bail.
					return
				}
				pcm = bigger
			}
			for i := 0; i < n; i++ {
				pcm.samples[writePos+i] = [2]float32{float32(tmp[i][0]), float32(tmp[i][1])}
			}
			writePos += n
			// Release-store the new length so the audio thread can see the
			// freshly-written samples in order.
			pcm.decodedLen.Store(int64(writePos))
			sinceEvent += n
		}

		if !ok {
			break
		}

		if sinceEvent >= chunkThreshold && builder != nil {
			builder.Feed(pcm.samples, writePos)
			data := builder.Snapshot()
			d.waveform.Store(data)
			d.bus.PublishAsync(event.Event{
				Topic: event.TopicEngine, Action: event.ActionWaveformReady,
				DeckID: d.id, Payload: data,
			})
			sinceEvent = 0
		}
	}

	// Real length may differ from the initial estimate. Correct totalLen so
	// downstream math (duration, loops, normalization) uses the truth.
	if writePos != pcm.totalLen {
		pcm.totalLen = writePos
	}

	log.Printf("deck %d: decoded %d samples (%.1f sec)", d.id, writePos, float64(writePos)/float64(d.sampleRate))

	if builder != nil {
		builder.Feed(pcm.samples, writePos)
		builder.Flush(pcm.samples, writePos)
		data := builder.Snapshot()
		d.waveform.Store(data)
		d.bus.PublishAsync(event.Event{
			Topic: event.TopicEngine, Action: event.ActionWaveformReady,
			DeckID: d.id, Payload: data,
		})
		if d.wfCache != nil && track.ID != "" {
			if err := d.wfCache.PutWaveform(track.ID, data, track.FileMtime); err != nil {
				log.Printf("deck %d: cache waveform: %v", d.id, err)
			}
		}
	}

	d.bus.PublishAsync(event.Event{
		Topic:  event.TopicEngine,
		Action: event.ActionTrackDecoded,
		DeckID: d.id,
		Payload: &event.TrackDecodedPayload{
			Track:      track,
			Samples:    pcm.samples[:writePos],
			SampleRate: d.sampleRate,
		},
	})
}

func (d *Deck) storeFloat(a *atomic.Uint64, v float64) {
	a.Store(math.Float64bits(v))
}

func (d *Deck) loadFloat(a *atomic.Uint64) float64 {
	return math.Float64frombits(a.Load())
}
