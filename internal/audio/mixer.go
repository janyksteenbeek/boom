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

	// Pre-allocated buffers — never reallocated
	buf1 [maxBufSize][2]float32
	buf2 [maxBufSize][2]float32
}

func NewMasterMixer(decks []*Deck, sampleRate int) *MasterMixer {
	m := &MasterMixer{
		decks: decks,
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
// Called from audio callback thread only.
func (m *MasterMixer) Stream(samples [][2]float32) {
	n := len(samples)

	// Process in chunks if request exceeds pre-allocated buffer size
	for offset := 0; offset < n; offset += maxBufSize {
		end := offset + maxBufSize
		if end > n {
			end = n
		}
		m.streamChunk(samples[offset:end])
	}
}

func (m *MasterMixer) streamChunk(samples [][2]float32) {
	n := len(samples)

	// Clear fixed buffers
	for i := 0; i < n; i++ {
		m.buf1[i] = [2]float32{}
		m.buf2[i] = [2]float32{}
	}

	// Read from decks into pre-allocated buffers
	if len(m.decks) > 0 {
		m.decks[0].Stream(m.buf1[:n])
	}
	if len(m.decks) > 1 {
		m.decks[1].Stream(m.buf2[:n])
	}

	// Per-sample smoothed mixing — eliminates clicks on crossfader/volume changes
	for i := 0; i < n; i++ {
		cf := m.crossfader.Tick()
		mv := m.masterVol.Tick()
		gainA, gainB := crossfadeGains(cf)

		samples[i][0] = (m.buf1[i][0]*gainA + m.buf2[i][0]*gainB) * mv
		samples[i][1] = (m.buf1[i][1]*gainA + m.buf2[i][1]*gainB) * mv
	}
}

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
