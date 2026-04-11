package analysis

// ComputeBeatGrid generates a fixed-interval beat grid from BPM and the onset envelope.
// Returns beat positions in seconds.
func ComputeBeatGrid(samples [][2]float32, sampleRate int, bpm float64) []float64 {
	if bpm <= 0 || len(samples) == 0 || sampleRate <= 0 {
		return nil
	}

	totalDuration := float64(len(samples)) / float64(sampleRate)
	beatPeriod := 60.0 / bpm // seconds per beat

	// Find the downbeat: strongest onset in the first 4 beats.
	firstBeat := findDownbeat(samples, sampleRate, beatPeriod)

	// Generate fixed grid
	var grid []float64
	for t := firstBeat; t < totalDuration; t += beatPeriod {
		grid = append(grid, t)
	}

	return grid
}

// findDownbeat estimates the position of the first downbeat by looking for
// the strongest energy onset in the first few beats of the track.
func findDownbeat(samples [][2]float32, sampleRate int, beatPeriod float64) float64 {
	// Search window: first 4 beats
	searchSamples := int(beatPeriod * 4 * float64(sampleRate))
	if searchSamples > len(samples) {
		searchSamples = len(samples)
	}
	if searchSamples == 0 {
		return 0
	}

	// Compute short-time energy with small hop
	hopSamples := sampleRate / 100 // 10ms hops
	if hopSamples < 1 {
		hopSamples = 1
	}
	frameSamples := sampleRate / 20 // 50ms frames
	if frameSamples < 1 {
		frameSamples = 1
	}

	bestEnergy := 0.0
	bestPos := 0.0

	for offset := 0; offset+frameSamples <= searchSamples; offset += hopSamples {
		var energy float64
		end := offset + frameSamples
		if end > searchSamples {
			end = searchSamples
		}
		for i := offset; i < end; i++ {
			s := float64(samples[i][0]+samples[i][1]) / 2.0
			energy += s * s
		}

		// Weight earlier positions slightly to prefer the actual start
		timeSec := float64(offset) / float64(sampleRate)
		weight := 1.0 - 0.1*(timeSec/(beatPeriod*4))
		energy *= weight

		if energy > bestEnergy {
			bestEnergy = energy
			bestPos = timeSec
		}
	}

	return bestPos
}
