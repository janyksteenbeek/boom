package audio

import "math"

const maxBufSize = 16384 // Pre-allocated max; handles any reasonable callback size

// Pre-computed equal-power crossfade lookup table — eliminates trig from audio hot path.
const crossfadeTableSize = 1024

var crossfadeTable [crossfadeTableSize][2]float32

func init() {
	for i := 0; i < crossfadeTableSize; i++ {
		pos := float64(i) / float64(crossfadeTableSize-1)
		crossfadeTable[i][0] = float32(math.Cos(pos * math.Pi / 2))
		crossfadeTable[i][1] = float32(math.Sin(pos * math.Pi / 2))
	}
}

// MasterMixer combines deck outputs with crossfade and master volume.
type MasterMixer struct {
	decks []*Deck

	// Smoothed parameters — Set() from any goroutine, Tick() from audio thread only
	crossfader SmoothParam
	masterVol  SmoothParam
	cueVol     SmoothParam // Headphone/cue volume (ready for cue output routing)

	// Beat FX on master output
	beatFX *BeatFX

	// Pre-allocated buffers — never reallocated
	buf1 [maxBufSize][2]float32
	buf2 [maxBufSize][2]float32
}

func NewMasterMixer(decks []*Deck, sampleRate int) *MasterMixer {
	m := &MasterMixer{
		decks:  decks,
		beatFX: NewBeatFX(sampleRate),
	}
	m.crossfader.Init(0.5, sampleRate, 0.005)
	m.masterVol.Init(0.8, sampleRate, 0.005)
	m.cueVol.Init(0.8, sampleRate, 0.005)
	return m
}

func (m *MasterMixer) SetCrossfader(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	m.crossfader.Set(float32(v))
}

func (m *MasterMixer) SetMasterVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	m.masterVol.Set(float32(v))
}

func (m *MasterMixer) SetCueVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	m.cueVol.Set(float32(v))
}

// Stream mixes all deck outputs with per-sample smoothed crossfade and master volume.
// Called from the master output's producer goroutine. Equivalent to
// StreamPair(samples, nil) — kept as a thin alias for callers that don't
// care about cue.
func (m *MasterMixer) Stream(samples [][2]float32) {
	m.StreamPair(samples, nil)
}

// StreamPair fills the master buffer and (optionally) a cue buffer in a
// single pass. The cue tap is pre-crossfade and pre-master-fader: every
// deck is summed straight in, then scaled by the smoothed cue volume.
// That matches how a hardware DJ mixer's headphone bus works when both
// deck PFL buttons are engaged — we don't have a per-deck PFL toggle yet,
// so for now everything is in the cue mix.
//
// Both buffers must be the same length when cue is non-nil. Pass nil for
// cue to skip the cue work entirely (and avoid touching the cue volume
// smoother).
func (m *MasterMixer) StreamPair(master, cue [][2]float32) {
	n := len(master)
	if cue != nil && len(cue) < n {
		n = len(cue)
	}

	for offset := 0; offset < n; offset += maxBufSize {
		end := offset + maxBufSize
		if end > n {
			end = n
		}
		var cueChunk [][2]float32
		if cue != nil {
			cueChunk = cue[offset:end]
		}
		m.streamChunk(master[offset:end], cueChunk)
	}
}

func (m *MasterMixer) streamChunk(master, cue [][2]float32) {
	n := len(master)

	clear(m.buf1[:n])
	clear(m.buf2[:n])

	if len(m.decks) > 0 {
		m.decks[0].Stream(m.buf1[:n])
	}
	if len(m.decks) > 1 {
		m.decks[1].Stream(m.buf2[:n])
	}

	// Smoothed params are ticked once per block — PrepareBlock returns the
	// start value and a per-sample linear step. This keeps zipper-noise
	// prevention intact while eliminating 3 atomic loads per sample.
	cfStart, cfStep := m.crossfader.PrepareBlock(n)
	mvStart, mvStep := m.masterVol.PrepareBlock(n)

	for i := 0; i < n; i++ {
		fi := float32(i)
		cf := cfStart + cfStep*fi
		mv := mvStart + mvStep*fi
		gainA, gainB := crossfadeGains(cf)

		master[i][0] = (m.buf1[i][0]*gainA + m.buf2[i][0]*gainB) * mv
		master[i][1] = (m.buf1[i][1]*gainA + m.buf2[i][1]*gainB) * mv
	}

	if cue != nil {
		cvStart, cvStep := m.cueVol.PrepareBlock(n)
		for i := 0; i < n; i++ {
			cv := cvStart + cvStep*float32(i)
			cue[i][0] = (m.buf1[i][0] + m.buf2[i][0]) * cv
			cue[i][1] = (m.buf1[i][1] + m.buf2[i][1]) * cv
		}
	}

	// Master beat FX is post-crossfade only — the cue bus stays dry so
	// the DJ hears the source signal, not whatever they're applying to
	// the room.
	m.beatFX.ProcessBuffer(master, n)
}

func (m *MasterMixer) SetBeatFXType(t FXType)    { m.beatFX.SetFXType(t) }
func (m *MasterMixer) SetBeatFXActive(on bool)   { m.beatFX.SetActive(on) }
func (m *MasterMixer) SetBeatFXWetDry(v float32) { m.beatFX.SetWetDry(v) }
func (m *MasterMixer) SetBeatFXTime(ms float32)  { m.beatFX.SetTime(ms) }

func crossfadeGains(pos float32) (gainA, gainB float32) {
	idx := int(pos * float32(crossfadeTableSize-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= crossfadeTableSize {
		idx = crossfadeTableSize - 1
	}
	return crossfadeTable[idx][0], crossfadeTable[idx][1]
}
