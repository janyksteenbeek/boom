package audio

import (
	"math"
	"math/cmplx"
	"time"

	"github.com/gopxl/beep/v2"
)

const defaultPeakCount = 400

// WaveformData holds pre-computed peak data for waveform display.
type WaveformData struct {
	Peaks     []float64 // Overall amplitude (kept for compatibility)
	PeaksLow  []float64 // Low frequency band energy (20-250 Hz)
	PeaksMid  []float64 // Mid frequency band energy (250-4000 Hz)
	PeaksHigh []float64 // High frequency band energy (4000+ Hz)
	SampleRate int
	Duration   time.Duration
	NumSamples int
	Resolution int
}

// Frequency band boundaries in Hz.
const (
	freqLowMax  = 250.0
	freqMidMax  = 4000.0
)

// GenerateWaveformFromPCM generates frequency-colored waveform data from decoded PCM.
func GenerateWaveformFromPCM(samples [][2]float32, sampleRate int) *WaveformData {
	totalSamples := len(samples)
	if totalSamples == 0 {
		return &WaveformData{SampleRate: sampleRate}
	}

	resolution := totalSamples / defaultPeakCount
	if resolution < 1 {
		resolution = 1
	}

	sr := float64(sampleRate)
	fftSize := nextPowerOf2(resolution)
	binWidth := sr / float64(fftSize)

	// Pre-compute bin boundaries
	lowMaxBin := int(freqLowMax / binWidth)
	midMaxBin := int(freqMidMax / binWidth)
	nyquistBin := fftSize / 2
	if lowMaxBin < 1 {
		lowMaxBin = 1
	}
	if midMaxBin <= lowMaxBin {
		midMaxBin = lowMaxBin + 1
	}
	if nyquistBin <= midMaxBin {
		nyquistBin = midMaxBin + 1
	}

	numPeaks := totalSamples/resolution + 1
	peaks := make([]float64, 0, numPeaks)
	peaksLow := make([]float64, 0, numPeaks)
	peaksMid := make([]float64, 0, numPeaks)
	peaksHigh := make([]float64, 0, numPeaks)

	// Reusable buffers
	mono := make([]float64, fftSize)
	fftBuf := make([]complex128, fftSize)

	for offset := 0; offset < totalSamples; offset += resolution {
		end := offset + resolution
		if end > totalSamples {
			end = totalSamples
		}
		n := end - offset

		// Convert stereo to mono float64, zero-pad to fftSize
		for i := 0; i < n; i++ {
			s := samples[offset+i]
			mono[i] = float64(s[0]+s[1]) / 2.0
		}
		for i := n; i < fftSize; i++ {
			mono[i] = 0
		}

		// Overall RMS
		var sumSq float64
		for i := 0; i < n; i++ {
			sumSq += mono[i] * mono[i]
		}
		rms := math.Sqrt(sumSq / float64(n))
		peaks = append(peaks, rms)

		// Apply Hann window before FFT
		for i := 0; i < fftSize; i++ {
			w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(fftSize-1)))
			fftBuf[i] = complex(mono[i]*w, 0)
		}

		// In-place FFT
		fftInPlace(fftBuf)

		// Accumulate energy per band
		var energyLow, energyMid, energyHigh float64
		for bin := 1; bin < nyquistBin && bin < fftSize/2; bin++ {
			mag := cmplx.Abs(fftBuf[bin])
			e := mag * mag
			if bin < lowMaxBin {
				energyLow += e
			} else if bin < midMaxBin {
				energyMid += e
			} else {
				energyHigh += e
			}
		}

		// Convert energy to amplitude-like value (sqrt for perceptual scaling)
		peaksLow = append(peaksLow, math.Sqrt(energyLow))
		peaksMid = append(peaksMid, math.Sqrt(energyMid))
		peaksHigh = append(peaksHigh, math.Sqrt(energyHigh))
	}

	// Normalize and process each band
	normalizeAndProcess(peaks)
	normalizeAndProcess(peaksLow)
	normalizeAndProcess(peaksMid)
	normalizeAndProcess(peaksHigh)

	duration := time.Duration(float64(totalSamples) / float64(sampleRate) * float64(time.Second))

	return &WaveformData{
		Peaks:      peaks,
		PeaksLow:   peaksLow,
		PeaksMid:   peaksMid,
		PeaksHigh:  peaksHigh,
		SampleRate: sampleRate,
		Duration:   duration,
		NumSamples: totalSamples,
		Resolution: resolution,
	}
}

// normalizeAndProcess normalizes peaks to 0-1, applies gamma correction and smoothing.
func normalizeAndProcess(peaks []float64) {
	if len(peaks) == 0 {
		return
	}

	// Normalize to 0.0-1.0
	maxPeak := 0.0
	for _, p := range peaks {
		if p > maxPeak {
			maxPeak = p
		}
	}
	if maxPeak > 0 {
		for i := range peaks {
			peaks[i] /= maxPeak
		}
	}

	// Gamma correction — subtle boost for quiet parts, preserves dynamic range
	for i := range peaks {
		peaks[i] = math.Pow(peaks[i], 0.75)
	}

	// Smoothing — 3-sample moving average
	if len(peaks) > 2 {
		smoothed := make([]float64, len(peaks))
		smoothed[0] = (peaks[0] + peaks[1]) / 2
		for i := 1; i < len(peaks)-1; i++ {
			smoothed[i] = (peaks[i-1] + peaks[i] + peaks[i+1]) / 3
		}
		smoothed[len(peaks)-1] = (peaks[len(peaks)-2] + peaks[len(peaks)-1]) / 2
		copy(peaks, smoothed)
	}
}

// --- FFT ---

// nextPowerOf2 returns the smallest power of 2 >= n.
func nextPowerOf2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// fftInPlace performs an in-place radix-2 Cooley-Tukey FFT.
func fftInPlace(a []complex128) {
	n := len(a)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}

	// Butterfly stages
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		wn := -2.0 * math.Pi / float64(size)
		for k := 0; k < n; k += size {
			for m := 0; m < half; m++ {
				angle := wn * float64(m)
				w := complex(math.Cos(angle), math.Sin(angle))
				u := a[k+m]
				t := w * a[k+m+half]
				a[k+m] = u + t
				a[k+m+half] = u - t
			}
		}
	}
}

// GenerateWaveform reads an audio file and extracts waveform data.
// Kept as fallback — prefer GenerateWaveformFromPCM when PCM data is available.
func GenerateWaveform(path string) (*WaveformData, error) {
	src, format, err := Decode(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	buf := beep.NewBuffer(format)
	buf.Append(src)

	totalSamples := buf.Len()
	resolution := totalSamples / defaultPeakCount
	if resolution < 1 {
		resolution = 1
	}

	peaks := make([]float64, 0, totalSamples/resolution+1)
	streamer := buf.Streamer(0, totalSamples)
	chunk := make([][2]float64, resolution)

	for {
		n, ok := streamer.Stream(chunk)
		if n == 0 {
			break
		}

		var sumSq float64
		for _, s := range chunk[:n] {
			mono := (s[0] + s[1]) / 2.0
			sumSq += mono * mono
		}
		rms := math.Sqrt(sumSq / float64(n))
		peaks = append(peaks, rms)

		if !ok {
			break
		}
	}

	normalizeAndProcess(peaks)

	return &WaveformData{
		Peaks:      peaks,
		SampleRate: int(format.SampleRate),
		Duration:   format.SampleRate.D(totalSamples),
		NumSamples: totalSamples,
		Resolution: resolution,
	}, nil
}
