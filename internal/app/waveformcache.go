package app

import (
	"time"

	"github.com/janyksteenbeek/boom/internal/audio"
	"github.com/janyksteenbeek/boom/internal/library"
)

// storeWaveformCache adapts *library.Store to audio.WaveformCache. It lives
// in the app layer so library doesn't need to import audio (which would
// create an import cycle).
type storeWaveformCache struct {
	store *library.Store
}

func newStoreWaveformCache(store *library.Store) *storeWaveformCache {
	return &storeWaveformCache{store: store}
}

func (c *storeWaveformCache) GetWaveform(trackID string, sampleRate int, mtime int64) (*audio.WaveformData, bool) {
	b, ok, err := c.store.GetWaveform(trackID, sampleRate, mtime)
	if err != nil || !ok || b == nil {
		return nil, false
	}
	return &audio.WaveformData{
		Peaks:      library.DecodeFloat64s(b.Peaks),
		PeaksLow:   library.DecodeFloat64s(b.PeaksLow),
		PeaksMid:   library.DecodeFloat64s(b.PeaksMid),
		PeaksHigh:  library.DecodeFloat64s(b.PeaksHigh),
		SampleRate: b.SampleRate,
		Duration:   time.Duration(b.DurationMs) * time.Millisecond,
		NumSamples: b.NumSamples,
		Resolution: b.Resolution,
	}, true
}

func (c *storeWaveformCache) PutWaveform(trackID string, data *audio.WaveformData, mtime int64) error {
	if data == nil {
		return nil
	}
	return c.store.PutWaveform(trackID, &library.WaveformBlob{
		SampleRate: data.SampleRate,
		DurationMs: int(data.Duration / time.Millisecond),
		Resolution: data.Resolution,
		NumSamples: data.NumSamples,
		Peaks:      library.EncodeFloat64s(data.Peaks),
		PeaksLow:   library.EncodeFloat64s(data.PeaksLow),
		PeaksMid:   library.EncodeFloat64s(data.PeaksMid),
		PeaksHigh:  library.EncodeFloat64s(data.PeaksHigh),
	}, mtime)
}
