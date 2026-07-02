package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// iconIdle returns a black "soundwave bars" glyph for the menu bar. It is used
// as a template icon, so macOS recolors it for light/dark menu bars.
func iconIdle() []byte { return wavePNG(color.NRGBA{R: 0, G: 0, B: 0, A: 255}) }

// iconRecording returns a red soundwave glyph shown while recording.
func iconRecording() []byte { return wavePNG(color.NRGBA{R: 255, G: 59, B: 48, A: 255}) }

// iconSetIdle / iconSetRecording swap the menu-bar icon. They are safe to call
// from any goroutine.
func iconSetIdle()      { menuSetIcon(iconIdle(), true) }
func iconSetRecording() { menuSetIcon(iconRecording(), false) }

// wavePNG draws five vertical bars resembling a sound waveform.
func wavePNG(col color.NRGBA) []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size)) // transparent background
	heights := []float64{0.40, 0.66, 1.00, 0.58, 0.46}
	const barW, gap = 5, 4
	total := len(heights)*barW + (len(heights)-1)*gap
	x := (size - total) / 2
	for _, h := range heights {
		bh := int(h * float64(size-14))
		if bh < barW {
			bh = barW
		}
		y := (size - bh) / 2
		fillRect(img, x, y, barW, bh, col)
		x += barW + gap
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func fillRect(img *image.NRGBA, x, y, w, h int, col color.NRGBA) {
	b := img.Bounds()
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if xx >= 0 && yy >= 0 && xx < b.Dx() && yy < b.Dy() {
				img.SetNRGBA(xx, yy, col)
			}
		}
	}
}
