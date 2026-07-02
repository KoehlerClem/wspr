package main

import (
	"math"
	"testing"
)

// TestFFTImpulse checks that the FFT of a unit impulse at index 0 is flat —
// every bin has magnitude 1.
func TestFFTImpulse(t *testing.T) {
	n := 256
	re := make([]float64, n)
	im := make([]float64, n)
	re[0] = 1.0
	fft(re, im)
	for i := 0; i < n; i++ {
		if mag := math.Hypot(re[i], im[i]); math.Abs(mag-1.0) > 1e-9 {
			t.Fatalf("bin %d magnitude = %v, want 1.0", i, mag)
		}
	}
}

// TestFFTSine checks that a pure sine at bin k produces a magnitude peak at
// bin k in the lower half of the spectrum.
func TestFFTSine(t *testing.T) {
	n, k := 256, 20
	re := make([]float64, n)
	im := make([]float64, n)
	for i := 0; i < n; i++ {
		re[i] = math.Sin(2 * math.Pi * float64(k) * float64(i) / float64(n))
	}
	fft(re, im)
	peak, peakBin := 0.0, -1
	for i := 0; i < n/2; i++ {
		if mag := math.Hypot(re[i], im[i]); mag > peak {
			peak, peakBin = mag, i
		}
	}
	if peakBin != k {
		t.Fatalf("peak at bin %d, want %d", peakBin, k)
	}
}

// TestComputeBandsOrder checks that a low-frequency tone lights a lower-indexed
// band than a high-frequency tone — i.e. bass really is on the left.
func TestComputeBandsOrder(t *testing.T) {
	low := loudestBand(computeBands(sineWindow(8)))   // ~500 Hz
	high := loudestBand(computeBands(sineWindow(30))) // ~1.9 kHz
	if low >= high {
		t.Fatalf("low-frequency band %d not below high-frequency band %d", low, high)
	}
}

// BenchmarkComputeBands measures one FFT + band-grouping pass — the work done
// once per ~16 ms audio window while recording.
func BenchmarkComputeBands(b *testing.B) {
	samples := sineWindow(20)
	for b.Loop() {
		computeBands(samples)
	}
}

func sineWindow(bin int) []float64 {
	s := make([]float64, fftSize)
	for i := range s {
		s[i] = 0.5 * math.Sin(2*math.Pi*float64(bin)*float64(i)/float64(fftSize))
	}
	return s
}

func loudestBand(bands []float64) int {
	best, idx := -1.0, 0
	for i, v := range bands {
		if v > best {
			best, idx = v, i
		}
	}
	return idx
}
