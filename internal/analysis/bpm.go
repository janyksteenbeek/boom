package analysis

import (
	"math"
	"math/cmplx"
)

const (
	// Analysis sample rate after downsampling.
	analysisSR = 8000
	// FFT frame size for onset detection (2048 @ 8kHz = 256ms, good spectral resolution).
	onsetFrameSize = 2048
	// Hop size — small enough to give sufficient lag resolution.
	// onset_rate = 8000/128 = 62.5 Hz → ~47 lag values for 50-220 BPM.
	onsetHopSize = 128
	// BPM search range for autocorrelation (wide to capture all candidates).
	minBPM = 50.0
	maxBPM = 220.0
)

// DetectBPM analyzes stereo PCM audio and returns the detected BPM.
// rangeMin/rangeMax define the preferred DJ BPM range (e.g., 80-180).
// The result is doubled/halved to fit within this range.
// Returns 0 if detection fails.
func DetectBPM(samples [][2]float32, sampleRate int, rangeMin, rangeMax float64) float64 {
	if len(samples) == 0 || sampleRate <= 0 {
		return 0
	}

	// Step 1: Downmix to mono and downsample to analysisSR.
	mono := downmixAndDownsample(samples, sampleRate)
	if len(mono) < onsetFrameSize*2 {
		return 0
	}

	// Step 2: Compute onset strength envelope via spectral flux.
	envelope := spectralFluxEnvelope(mono)
	if len(envelope) < 16 {
		return 0
	}

	// Step 3: Autocorrelation of onset envelope.
	onsetRate := float64(analysisSR) / float64(onsetHopSize)
	bpm := autocorrelateBPM(envelope, onsetRate)

	// Step 4: Snap to DJ range — double or halve until within range.
	// A track at 63 BPM is almost certainly 126; one at 190 is likely 95.
	if rangeMin > 0 && rangeMax > rangeMin {
		bpm = snapToDJRange(bpm, rangeMin, rangeMax)
	}

	return math.Round(bpm*100) / 100
}

// downmixAndDownsample converts stereo float32 to mono float64 and
// decimates from sampleRate to analysisSR using simple averaging.
func downmixAndDownsample(samples [][2]float32, sampleRate int) []float64 {
	ratio := sampleRate / analysisSR
	if ratio < 1 {
		ratio = 1
	}

	outLen := len(samples) / ratio
	mono := make([]float64, outLen)

	for i := 0; i < outLen; i++ {
		var sum float64
		base := i * ratio
		end := base + ratio
		if end > len(samples) {
			end = len(samples)
		}
		for j := base; j < end; j++ {
			sum += float64(samples[j][0]+samples[j][1]) / 2.0
		}
		mono[i] = sum / float64(end-base)
	}
	return mono
}

// spectralFluxEnvelope computes the half-wave rectified spectral flux
// onset strength signal from mono audio at analysisSR.
func spectralFluxEnvelope(mono []float64) []float64 {
	fftSize := nextPowerOf2(onsetFrameSize)
	numFrames := (len(mono) - onsetFrameSize) / onsetHopSize
	if numFrames <= 0 {
		return nil
	}

	envelope := make([]float64, numFrames)
	prevMag := make([]float64, fftSize/2+1)
	frame := make([]float64, fftSize)
	fftBuf := make([]complex128, fftSize)

	for f := 0; f < numFrames; f++ {
		offset := f * onsetHopSize

		// Copy frame and zero-pad
		copy(frame, mono[offset:offset+onsetFrameSize])
		for i := onsetFrameSize; i < fftSize; i++ {
			frame[i] = 0
		}

		// Apply Hann window
		hannWindow(frame[:onsetFrameSize])

		// FFT
		for i := range fftBuf {
			fftBuf[i] = complex(frame[i], 0)
		}
		fftInPlace(fftBuf)

		// Compute magnitude and half-wave rectified spectral flux
		var flux float64
		for bin := 0; bin <= fftSize/2; bin++ {
			mag := cmplx.Abs(fftBuf[bin])
			diff := mag - prevMag[bin]
			if diff > 0 {
				flux += diff
			}
			prevMag[bin] = mag
		}
		envelope[f] = flux
	}

	return envelope
}

// autocorrelateBPM performs autocorrelation on the onset envelope
// to find the best BPM candidate. No Gaussian weighting is applied
// during the search — the raw ACF peak determines tempo.
// Parabolic interpolation refines the peak to sub-lag precision.
func autocorrelateBPM(envelope []float64, onsetRate float64) float64 {
	// Lag range corresponding to minBPM-maxBPM
	minLag := int(math.Ceil(onsetRate * 60.0 / maxBPM))
	maxLag := int(math.Floor(onsetRate * 60.0 / minBPM))

	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= len(envelope) {
		maxLag = len(envelope) - 1
	}
	if minLag >= maxLag {
		return 0
	}

	// Compute mean for zero-mean autocorrelation
	var mean float64
	for _, v := range envelope {
		mean += v
	}
	mean /= float64(len(envelope))

	// Compute zero-lag autocorrelation for normalization
	var zeroLagACF float64
	for _, v := range envelope {
		d := v - mean
		zeroLagACF += d * d
	}
	if zeroLagACF == 0 {
		return 0
	}
	zeroLagACF /= float64(len(envelope))

	// Compute normalized autocorrelation for each lag (no perceptual weighting)
	acfValues := make([]float64, maxLag+1)
	bestLag := minLag
	bestACF := -math.MaxFloat64

	for lag := minLag; lag <= maxLag; lag++ {
		var acf float64
		n := len(envelope) - lag
		for i := 0; i < n; i++ {
			acf += (envelope[i] - mean) * (envelope[i+lag] - mean)
		}
		acf /= float64(n)
		// Normalize by zero-lag value
		acf /= zeroLagACF

		acfValues[lag] = acf

		if acf > bestACF {
			bestACF = acf
			bestLag = lag
		}
	}

	// Parabolic interpolation around the peak for sub-lag precision
	refinedLag := float64(bestLag)
	if bestLag > minLag && bestLag < maxLag {
		alpha := acfValues[bestLag-1]
		beta := acfValues[bestLag]
		gamma := acfValues[bestLag+1]
		denom := alpha - 2*beta + gamma
		if denom != 0 {
			delta := 0.5 * (alpha - gamma) / denom
			// Sanity check: delta should be in [-0.5, 0.5]
			if delta > -0.5 && delta < 0.5 {
				refinedLag = float64(bestLag) + delta
			}
		}
	}

	bpm := onsetRate * 60.0 / refinedLag

	// Octave validation
	bpm = validateOctave(acfValues, onsetRate, bpm, minLag, maxLag)

	return bpm
}

// snapToDJRange doubles or halves the BPM until it falls within the
// preferred DJ range. Standard behavior in DJ software — a detected 63 BPM
// is reported as 126.
func snapToDJRange(bpm, rangeMin, rangeMax float64) float64 {
	if bpm <= 0 {
		return 0
	}
	for bpm < rangeMin {
		bpm *= 2
	}
	for bpm > rangeMax {
		bpm /= 2
	}
	return bpm
}

// validateOctave checks BPM/2 and BPM*2 to avoid octave errors.
// Uses raw (unweighted) ACF values for comparison.
func validateOctave(acfValues []float64, onsetRate float64, bpm float64, minLag, maxLag int) float64 {
	acfAt := func(targetBPM float64) float64 {
		lag := int(math.Round(onsetRate * 60.0 / targetBPM))
		if lag < minLag || lag > maxLag {
			return -1
		}
		return acfValues[lag]
	}

	origACF := acfAt(bpm)

	// Check half-time: prefer if nearly as strong (avoids double-time detection)
	halfBPM := bpm / 2
	if halfBPM >= minBPM {
		halfACF := acfAt(halfBPM)
		if halfACF > 0.8*origACF {
			return halfBPM
		}
	}

	// Check double-time: prefer if significantly stronger
	doubleBPM := bpm * 2
	if doubleBPM <= maxBPM {
		doubleACF := acfAt(doubleBPM)
		if doubleACF > 1.1*origACF {
			return doubleBPM
		}
	}

	return bpm
}
