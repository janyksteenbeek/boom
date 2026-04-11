package analysis

import "math"

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

// hannWindow applies an in-place Hann window to the given slice.
func hannWindow(buf []float64) {
	n := len(buf)
	if n <= 1 {
		return
	}
	inv := 1.0 / float64(n-1)
	for i := range buf {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)*inv))
		buf[i] *= w
	}
}
