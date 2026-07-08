package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync/atomic"
	"time"
)

var (
	iconBlack = color.NRGBA{A: 255}
	iconRed   = color.NRGBA{R: 255, G: 59, B: 48, A: 255}  // recording
	iconGreen = color.NRGBA{R: 52, G: 199, B: 89, A: 255}  // download progress
	iconGray  = color.NRGBA{R: 128, G: 128, B: 128, A: 140} // unfilled track
)

// iconIdle returns a black "soundwave bars" glyph for the menu bar. It is used
// as a template icon, so macOS recolors it for light/dark menu bars.
func iconIdle() []byte { return wavePNG(func(float64) color.NRGBA { return iconBlack }) }

// iconRecording returns a red soundwave glyph shown while recording.
func iconRecording() []byte { return wavePNG(func(float64) color.NRGBA { return iconRed }) }

// iconProgress renders the bars filled green from the left up to frac (0..1),
// gray beyond — the menu-bar download meter.
func iconProgress(frac float64) []byte {
	return wavePNG(func(t float64) color.NRGBA {
		if t <= frac {
			return iconGreen
		}
		return iconGray
	})
}

// iconSweep renders gray bars with a green band sweeping left to right —
// working, but no percentage to show. phase is in seconds.
func iconSweep(phase float64) []byte {
	const bandW = 0.45
	const period = 1.4 // seconds per sweep
	p := math.Mod(phase/period, 1.0)
	return wavePNG(func(t float64) color.NRGBA {
		if d := math.Mod(p-t+1.0, 1.0); d < bandW {
			return iconGreen
		}
		return iconGray
	})
}

// iconRecordingOn tracks whether the recording icon is being shown, so the
// download animation never paints over it.
var iconRecordingOn atomic.Bool

// iconSetIdle / iconSetRecording swap the menu-bar icon. They are safe to call
// from any goroutine.
func iconSetIdle()      { iconRecordingOn.Store(false); menuSetIcon(iconIdle(), true) }
func iconSetRecording() { iconRecordingOn.Store(true); menuSetIcon(iconRecording(), false) }

// iconAnimOn guards against more than one icon animator running at a time.
var iconAnimOn atomic.Bool

// startIconProgress animates the menu-bar icon while the model download /
// warm-up runs: a left-to-right green fill when the percentage is known, a
// sweeping green band while it is indeterminate. It restores the idle icon
// when the download leaves the running state.
func startIconProgress() {
	if !iconAnimOn.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer recoverLog("startIconProgress")
		defer iconAnimOn.Store(false)
		t0 := time.Now()
		tick := time.NewTicker(120 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			if !modelDlIs(dlRunning) {
				if !iconRecordingOn.Load() {
					iconSetIdle()
				}
				return
			}
			if iconRecordingOn.Load() {
				continue // the red recording icon wins
			}
			if pct := modelDlPercent(); pct >= 0 {
				menuSetIcon(iconProgress(float64(pct)/100.0), false)
			} else {
				menuSetIcon(iconSweep(time.Since(t0).Seconds()), false)
			}
		}
	}()
}

// wavePNG draws five vertical bars resembling a sound waveform. colAt maps a
// pixel column's normalized position across the bars (0..1, left to right) to
// its color.
func wavePNG(colAt func(float64) color.NRGBA) []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size)) // transparent background
	heights := []float64{0.40, 0.66, 1.00, 0.58, 0.46}
	const barW, gap = 5, 4
	total := len(heights)*barW + (len(heights)-1)*gap
	x0 := (size - total) / 2
	x := x0
	for _, h := range heights {
		bh := int(h * float64(size-14))
		if bh < barW {
			bh = barW
		}
		y := (size - bh) / 2
		for xx := x; xx < x+barW; xx++ {
			col := colAt((float64(xx-x0) + 0.5) / float64(total))
			for yy := y; yy < y+bh; yy++ {
				img.SetNRGBA(xx, yy, col)
			}
		}
		x += barW + gap
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
