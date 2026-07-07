package main

import (
	"encoding/binary"
	"math"
)

// numBands is how many frequency bands the spectrum is reduced to — one per bar
// in the overlay pill. It must match BARS in overlay_darwin.m.
const numBands = 17

// fftSize is the analysis window in samples (a power of two). At 16 kHz this is
// a 16 ms window, giving ~62 Hz frequency resolution and ~60 updates/second.
const fftSize = 256

// spectrumFeed accumulates raw little-endian 16-bit mono PCM and reports, for
// each fftSize window, a frequency spectrum reduced to numBands values in 0..1.
type spectrumFeed struct {
	onSpectrum func([]float64) // may be nil — push then does nothing
	samples    []float64
}

// push consumes one chunk of PCM, of any length.
func (sf *spectrumFeed) push(pcm []byte) {
	if sf.onSpectrum == nil {
		return
	}
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[i:]))
		sf.samples = append(sf.samples, float64(s)/32768.0)
		if len(sf.samples) == fftSize {
			sf.onSpectrum(computeBands(sf.samples))
			sf.samples = sf.samples[:0]
		}
	}
}

// computeBands runs an FFT over one window and groups the magnitude spectrum
// into numBands log-spaced bands (bass first), each scaled to 0..1.
func computeBands(samples []float64) []float64 {
	re := make([]float64, fftSize)
	im := make([]float64, fftSize)
	for i := 0; i < fftSize; i++ {
		// A Hann window cuts spectral leakage between bands.
		w := 0.5 - 0.5*math.Cos(2.0*math.Pi*float64(i)/float64(fftSize-1))
		re[i] = samples[i] * w
	}
	fft(re, im)

	// Magnitudes, capped at 4 kHz: the recording is 16 kHz, so bin k is
	// k*16000/fftSize Hz, and 4 kHz lands on bin hiBin.
	const loBin = 1                      // skip bin 0 (DC); start near 0 Hz
	const hiBin = 4000 * fftSize / 16000 // 4 kHz
	mag := make([]float64, hiBin)
	for i := 0; i < hiBin; i++ {
		mag[i] = math.Hypot(re[i], im[i])
	}

	// Group bins into log-spaced bands, marching a cursor so every band gets
	// at least one bin: low bands are a single bin, high bands span many.
	bands := make([]float64, numBands)
	ratio := float64(hiBin) / float64(loBin)
	cursor := loBin
	for b := 0; b < numBands; b++ {
		hi := int(float64(loBin) * math.Pow(ratio, float64(b+1)/float64(numBands)))
		if hi <= cursor {
			hi = cursor + 1
		}
		if hi > hiBin {
			hi = hiBin
		}
		var energy float64
		for i := cursor; i < hi; i++ {
			energy += mag[i] * mag[i]
		}
		bands[b] = bandLevel(math.Sqrt(energy) / float64(fftSize))
		cursor = hi
	}
	return bands
}

// bandLevel maps a band's RMS-like amplitude onto a 0..1 display value using a
// plain dBFS scale — no tilt, no presence boost.
func bandLevel(amp float64) float64 {
	if amp <= 1e-9 {
		return 0.0
	}
	const sensitivity = 60.0 // dB offset — higher lifts every band's level
	lvl := (20.0*math.Log10(amp) + sensitivity) / 35.0
	if lvl < 0.0 {
		return 0.0
	}
	if lvl > 1.0 {
		return 1.0
	}
	return lvl
}

// fft computes the in-place radix-2 FFT of the complex signal held in re/im.
// The length must be a power of two.
func fft(re, im []float64) {
	n := len(re)
	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	// Cooley-Tukey butterflies, doubling the transform length each pass.
	for length := 2; length <= n; length <<= 1 {
		ang := -2.0 * math.Pi / float64(length)
		wlenRe, wlenIm := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			wRe, wIm := 1.0, 0.0
			for k := 0; k < length/2; k++ {
				aRe, aIm := re[i+k], im[i+k]
				bRe := re[i+k+length/2]*wRe - im[i+k+length/2]*wIm
				bIm := re[i+k+length/2]*wIm + im[i+k+length/2]*wRe
				re[i+k], im[i+k] = aRe+bRe, aIm+bIm
				re[i+k+length/2], im[i+k+length/2] = aRe-bRe, aIm-bIm
				wRe, wIm = wRe*wlenRe-wIm*wlenIm, wRe*wlenIm+wIm*wlenRe
			}
		}
	}
}
