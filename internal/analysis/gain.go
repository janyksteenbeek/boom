package analysis

import "math"

// targetRMSdBFS is the loudness target the track-gain calculation aims for.
// -18 dBFS RMS is a conservative full-scale target that sits comfortably
// below clipping for a typical stereo mix. Tracks louder than this receive
// a negative gain offset, quieter tracks a positive one, so that loading a
// track to a deck produces a predictable starting volume.
const targetRMSdBFS = -18.0

// ComputeTrackGain returns a dB offset that, when applied to the track,
// brings its integrated RMS to targetRMSdBFS. This is intentionally a
// simple RMS-based measure rather than full ITU-R BS.1770 loudness — it's
// cheap, stable, and good enough to balance decks within ~1-2 dB.
//
// Silence or a zero-length buffer returns 0 (neutral).
func ComputeTrackGain(samples [][2]float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		m := float64(s[0]+s[1]) * 0.5
		sumSq += m * m
	}
	meanSq := sumSq / float64(len(samples))
	if meanSq <= 0 {
		return 0
	}
	rmsDB := 10.0 * math.Log10(meanSq) // 10*log10(RMS²) = 20*log10(RMS)
	gain := targetRMSdBFS - rmsDB
	// Clamp to a sane range so pathological inputs (e.g. near-silence stems)
	// can't produce extreme offsets.
	if gain > 12 {
		gain = 12
	} else if gain < -12 {
		gain = -12
	}
	return gain
}
