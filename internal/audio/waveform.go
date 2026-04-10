package audio

import (
	"math"
	"time"

	"github.com/gopxl/beep/v2"
)

const defaultPeakCount = 400

// WaveformData holds pre-computed peak data for waveform display.
type WaveformData struct {
	Peaks      []float64
	SampleRate int
	Duration   time.Duration
	NumSamples int
	Resolution int
}

// GenerateWaveformFromPCM generates waveform data directly from an already-decoded PCM buffer.
// This avoids decoding the file a second time.
func GenerateWaveformFromPCM(samples [][2]float32, sampleRate int) *WaveformData {
	totalSamples := len(samples)
	if totalSamples == 0 {
		return &WaveformData{
			SampleRate: sampleRate,
		}
	}

	resolution := totalSamples / defaultPeakCount
	if resolution < 1 {
		resolution = 1
	}

	peaks := make([]float64, 0, totalSamples/resolution+1)

	for offset := 0; offset < totalSamples; offset += resolution {
		end := offset + resolution
		if end > totalSamples {
			end = totalSamples
		}
		n := end - offset

		// RMS (root mean square) for a smoother waveform
		var sumSq float64
		for _, s := range samples[offset:end] {
			mono := float64(s[0]+s[1]) / 2.0
			sumSq += mono * mono
		}
		rms := math.Sqrt(sumSq / float64(n))
		peaks = append(peaks, rms)
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

	// Gamma correction — makes quiet parts more visible
	for i := range peaks {
		peaks[i] = math.Pow(peaks[i], 0.6)
	}

	// Smoothing — 3-sample moving average
	if len(peaks) > 2 {
		smoothed := make([]float64, len(peaks))
		smoothed[0] = (peaks[0] + peaks[1]) / 2
		for i := 1; i < len(peaks)-1; i++ {
			smoothed[i] = (peaks[i-1] + peaks[i] + peaks[i+1]) / 3
		}
		smoothed[len(peaks)-1] = (peaks[len(peaks)-2] + peaks[len(peaks)-1]) / 2
		peaks = smoothed
	}

	duration := time.Duration(float64(totalSamples) / float64(sampleRate) * float64(time.Second))

	return &WaveformData{
		Peaks:      peaks,
		SampleRate: sampleRate,
		Duration:   duration,
		NumSamples: totalSamples,
		Resolution: resolution,
	}
}

// GenerateWaveform reads an audio file and extracts RMS-based waveform data.
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

	for i := range peaks {
		peaks[i] = math.Pow(peaks[i], 0.6)
	}

	if len(peaks) > 2 {
		smoothed := make([]float64, len(peaks))
		smoothed[0] = (peaks[0] + peaks[1]) / 2
		for i := 1; i < len(peaks)-1; i++ {
			smoothed[i] = (peaks[i-1] + peaks[i] + peaks[i+1]) / 3
		}
		smoothed[len(peaks)-1] = (peaks[len(peaks)-2] + peaks[len(peaks)-1]) / 2
		peaks = smoothed
	}

	return &WaveformData{
		Peaks:      peaks,
		SampleRate: int(format.SampleRate),
		Duration:   format.SampleRate.D(totalSamples),
		NumSamples: totalSamples,
		Resolution: resolution,
	}, nil
}
