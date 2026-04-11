package analysis

import (
	"math"
	"math/cmplx"
)

const (
	// keyFFTSize is the frame size for chromagram computation.
	// 16384 samples at 48kHz = ~341ms. Gives binWidth ≈ 2.93 Hz,
	// providing ~2 bins per semitone at 130 Hz — sufficient resolution.
	keyFFTSize = 16384
	// keyHopSize is the hop between chromagram frames.
	keyHopSize = 8192
	// keyFrameSkip processes every Nth frame for speed.
	keyFrameSkip = 2
	// Reference frequency for A4.
	refFreqA4 = 440.0
	// Minimum frequency for chroma computation (C3 ≈ 130.81 Hz).
	// Below this, FFT resolution per semitone is insufficient.
	chromaMinFreq = 130.0
	// Maximum frequency for chroma computation.
	chromaMaxFreq = 5000.0
	// Spectral whitening window half-width (in bins).
	whitenWindow = 10
	// Minimum frame energy to include in analysis (skip silence).
	minFrameEnergy = 1e-8
)

// Krumhansl-Schmuckler key profiles.
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// Key names indexed by chroma index (0=C, 1=C#, ... 11=B).
var keyNames = [12]string{"C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}

// DetectKey analyzes stereo PCM audio and returns the detected musical key.
// Returns a string like "Am", "C", "F#m", "Eb", etc.
func DetectKey(samples [][2]float32, sampleRate int) string {
	if len(samples) == 0 || sampleRate <= 0 {
		return ""
	}

	chroma := computeChromagram(samples, sampleRate)

	// Check if chroma has any variance (skip flat/empty distributions)
	var variance float64
	var mean float64
	for _, v := range chroma {
		mean += v
	}
	mean /= 12
	for _, v := range chroma {
		d := v - mean
		variance += d * d
	}
	if variance < 1e-12 {
		return ""
	}

	return matchKeyProfile(chroma)
}

// computeChromagram accumulates pitch class energy across the track
// using spectral whitening and magnitude (not power) weighting.
func computeChromagram(samples [][2]float32, sampleRate int) [12]float64 {
	var chroma [12]float64

	fftSize := nextPowerOf2(keyFFTSize)
	numFrames := (len(samples) - keyFFTSize) / keyHopSize
	if numFrames <= 0 {
		return chroma
	}

	frame := make([]float64, fftSize)
	fftBuf := make([]complex128, fftSize)
	magBuf := make([]float64, fftSize/2+1)
	binWidth := float64(sampleRate) / float64(fftSize)

	// Pre-compute bin boundaries
	minBin := int(math.Ceil(chromaMinFreq / binWidth))
	if minBin < 1 {
		minBin = 1
	}
	maxBin := int(chromaMaxFreq / binWidth)
	if maxBin > fftSize/2 {
		maxBin = fftSize / 2
	}

	// Pre-compute chroma bin mapping for each FFT bin.
	chromaBinMap := make([]int, fftSize/2+1)
	for bin := minBin; bin <= maxBin; bin++ {
		freq := float64(bin) * binWidth
		if freq <= 0 {
			continue
		}
		// Map frequency to pitch class: 12 * log2(f/440) mod 12
		// Shift so A maps to index 9
		semitone := 12.0*math.Log2(freq/refFreqA4) + 9.0
		chromaIdx := int(math.Round(semitone)) % 12
		if chromaIdx < 0 {
			chromaIdx += 12
		}
		chromaBinMap[bin] = chromaIdx + 1 // +1 so 0 means "unmapped"
	}

	frameCount := 0
	for f := 0; f < numFrames; f += keyFrameSkip {
		offset := f * keyHopSize

		// Downmix to mono
		var frameEnergy float64
		for i := 0; i < keyFFTSize && offset+i < len(samples); i++ {
			s := samples[offset+i]
			v := float64(s[0]+s[1]) / 2.0
			frame[i] = v
			frameEnergy += v * v
		}
		for i := keyFFTSize; i < fftSize; i++ {
			frame[i] = 0
		}

		// Skip silent frames
		frameEnergy /= float64(keyFFTSize)
		if frameEnergy < minFrameEnergy {
			continue
		}

		// Apply Hann window
		hannWindow(frame[:keyFFTSize])

		// FFT
		for i := range fftBuf {
			fftBuf[i] = complex(frame[i], 0)
		}
		fftInPlace(fftBuf)

		// Compute magnitude spectrum (NOT squared — reduces bass dominance)
		for bin := 0; bin <= fftSize/2; bin++ {
			magBuf[bin] = cmplx.Abs(fftBuf[bin])
		}

		// Spectral whitening: normalize each bin by local average
		// This removes the natural 1/f spectral slope
		for bin := minBin; bin <= maxBin; bin++ {
			ci := chromaBinMap[bin]
			if ci == 0 {
				continue
			}

			// Compute local spectral average around this bin
			wStart := bin - whitenWindow
			if wStart < 0 {
				wStart = 0
			}
			wEnd := bin + whitenWindow
			if wEnd > fftSize/2 {
				wEnd = fftSize / 2
			}
			var localSum float64
			for b := wStart; b <= wEnd; b++ {
				localSum += magBuf[b]
			}
			localAvg := localSum / float64(wEnd-wStart+1)

			// Whitened magnitude
			whitened := magBuf[bin]
			if localAvg > 1e-10 {
				whitened = magBuf[bin] / localAvg
			}

			chroma[ci-1] += whitened
		}
		frameCount++
	}

	// Normalize chroma to sum to 1
	if frameCount > 0 {
		var total float64
		for _, v := range chroma {
			total += v
		}
		if total > 0 {
			for i := range chroma {
				chroma[i] /= total
			}
		}
	}

	return chroma
}

// matchKeyProfile correlates the observed chroma with all 24 key profiles.
func matchKeyProfile(chroma [12]float64) string {
	bestKey := ""
	bestCorr := -math.MaxFloat64

	for root := 0; root < 12; root++ {
		corrMaj := pearsonCorrelation(chroma, rotateProfile(majorProfile, root))
		if corrMaj > bestCorr {
			bestCorr = corrMaj
			bestKey = keyNames[root]
		}

		corrMin := pearsonCorrelation(chroma, rotateProfile(minorProfile, root))
		if corrMin > bestCorr {
			bestCorr = corrMin
			bestKey = keyNames[root] + "m"
		}
	}

	return bestKey
}

// rotateProfile rotates a key profile by the given number of semitones.
func rotateProfile(profile [12]float64, semitones int) [12]float64 {
	var rotated [12]float64
	for i := 0; i < 12; i++ {
		rotated[i] = profile[(i-semitones+12)%12]
	}
	return rotated
}

// pearsonCorrelation computes the Pearson correlation coefficient.
func pearsonCorrelation(observed [12]float64, profile [12]float64) float64 {
	var meanObs, meanProf float64
	for i := 0; i < 12; i++ {
		meanObs += observed[i]
		meanProf += profile[i]
	}
	meanObs /= 12
	meanProf /= 12

	var num, denomObs, denomProf float64
	for i := 0; i < 12; i++ {
		dObs := observed[i] - meanObs
		dProf := profile[i] - meanProf
		num += dObs * dProf
		denomObs += dObs * dObs
		denomProf += dProf * dProf
	}

	denom := math.Sqrt(denomObs * denomProf)
	if denom == 0 {
		return 0
	}
	return num / denom
}
