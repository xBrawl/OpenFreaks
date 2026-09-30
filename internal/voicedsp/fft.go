package voicedsp

import "math"

// fftSize — БПФ по 512 точкам: окно анализа 480 отсчётов (30 мс) с нулями.
const fftSize = 512

var (
	fftRev [fftSize]int
	fftCos [fftSize / 2]float64
	fftSin [fftSize / 2]float64
)

func init() {
	bits := 0
	for 1<<bits < fftSize {
		bits++
	}
	for i := range fftRev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		fftRev[i] = r
	}
	for i := range fftCos {
		a := -2 * math.Pi * float64(i) / fftSize
		fftCos[i], fftSin[i] = math.Cos(a), math.Sin(a)
	}
}

// fft — комплексное БПФ на месте; inverse включает деление на N.
func fft(re, im *[fftSize]float64, inverse bool) {
	for i, r := range fftRev {
		if i < r {
			re[i], re[r] = re[r], re[i]
			im[i], im[r] = im[r], im[i]
		}
	}
	sign := 1.0
	if inverse {
		sign = -1
	}
	for size := 2; size <= fftSize; size <<= 1 {
		half := size >> 1
		stride := fftSize / size
		for start := 0; start < fftSize; start += size {
			for k := 0; k < half; k++ {
				wr, wi := fftCos[k*stride], sign*fftSin[k*stride]
				a, b := start+k, start+k+half
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a] += tr
				im[a] += ti
			}
		}
	}
	if inverse {
		for i := range re {
			re[i] /= fftSize
			im[i] /= fftSize
		}
	}
}
