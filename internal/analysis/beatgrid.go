package analysis

import "math/cmplx"

// ComputeBeatGrid generates a fixed-interval beat grid from BPM.
// Uses a bass-focused onset envelope to find the optimal phase alignment
// so beat markers fall on kick drums. Returns beat positions in seconds.
func ComputeBeatGrid(samples [][2]float32, sampleRate int, bpm float64) []float64 {
	if bpm <= 0 || len(samples) == 0 || sampleRate <= 0 {
		return nil
	}

	totalDuration := float64(len(samples)) / float64(sampleRate)
	beatPeriod := 60.0 / bpm // seconds per beat

	mono := downmixAndDownsample(samples, sampleRate)
	if len(mono) < onsetFrameSize*2 {
		return simpleBeatGrid(0, beatPeriod, totalDuration)
	}

	// Use bass-only onset envelope for kick drum alignment.
	envelope := bassOnsetEnvelope(mono)
	if len(envelope) < 4 {
		return simpleBeatGrid(0, beatPeriod, totalDuration)
	}

	// Find optimal phase: search all possible offsets within one beat period.
	// For each offset, sum the squared onset strength at that offset + every
	// beat period. Squaring weights strong kick hits much higher than weak
	// background noise, producing more accurate phase alignment.
	onsetRate := float64(analysisSR) / float64(onsetHopSize)
	beatLag := beatPeriod * onsetRate // beat period in onset envelope samples

	maxOffset := int(beatLag)
	if maxOffset < 1 {
		maxOffset = 1
	}
	if maxOffset > len(envelope) {
		maxOffset = len(envelope)
	}

	bestOffset := 0
	bestScore := -1.0

	for offset := 0; offset < maxOffset; offset++ {
		var score float64
		count := 0
		for pos := float64(offset); int(pos) < len(envelope); pos += beatLag {
			idx := int(pos)
			if idx < len(envelope) {
				v := envelope[idx]
				score += v * v // squared weighting
				count++
			}
		}
		if count > 0 {
			score /= float64(count)
		}
		if score > bestScore {
			bestScore = score
			bestOffset = offset
		}
	}

	// Convert onset envelope offset to seconds
	firstBeat := float64(bestOffset) / onsetRate

	return simpleBeatGrid(firstBeat, beatPeriod, totalDuration)
}

// bassOnsetEnvelope computes spectral flux limited to 20-250 Hz.
// This isolates kick drums for accurate beat phase alignment,
// ignoring hi-hats, snares, and other high-frequency transients.
func bassOnsetEnvelope(mono []float64) []float64 {
	fftSize := nextPowerOf2(onsetFrameSize)
	numFrames := (len(mono) - onsetFrameSize) / onsetHopSize
	if numFrames <= 0 {
		return nil
	}

	// Bass frequency bin range: 20-250 Hz.
	// freq_resolution = analysisSR / fftSize = 8000/2048 ≈ 3.9 Hz/bin
	freqPerBin := float64(analysisSR) / float64(fftSize)
	binLow := int(20.0 / freqPerBin)   // ~bin 5
	binHigh := int(250.0 / freqPerBin)  // ~bin 64
	if binLow < 1 {
		binLow = 1
	}
	if binHigh > fftSize/2 {
		binHigh = fftSize / 2
	}

	envelope := make([]float64, numFrames)
	prevMag := make([]float64, binHigh+1)
	frame := make([]float64, fftSize)
	fftBuf := make([]complex128, fftSize)

	for f := 0; f < numFrames; f++ {
		offset := f * onsetHopSize

		copy(frame, mono[offset:offset+onsetFrameSize])
		for i := onsetFrameSize; i < fftSize; i++ {
			frame[i] = 0
		}

		hannWindow(frame[:onsetFrameSize])

		for i := range fftBuf {
			fftBuf[i] = complex(frame[i], 0)
		}
		fftInPlace(fftBuf)

		// Half-wave rectified spectral flux, bass bins only
		var flux float64
		for bin := binLow; bin <= binHigh; bin++ {
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

// simpleBeatGrid generates a fixed-interval grid from a start time.
func simpleBeatGrid(firstBeat, beatPeriod, totalDuration float64) []float64 {
	var grid []float64
	for t := firstBeat; t < totalDuration; t += beatPeriod {
		grid = append(grid, t)
	}
	return grid
}
