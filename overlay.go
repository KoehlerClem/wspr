package main

/*
#cgo LDFLAGS: -framework Cocoa
void overlayShow(void);
void overlayHide(void);
void overlaySetBands(const double *bands, int n);
*/
import "C"

import "unsafe"

// overlayShow displays the floating waveform pill near the bottom of the
// screen. Safe to call from any goroutine.
func overlayShow() { C.overlayShow() }

// overlayHide hides the waveform pill.
func overlayHide() { C.overlayHide() }

// overlaySetBands feeds a new frequency spectrum — one 0..1 value per bar — to
// the pill. Safe to call from any goroutine.
func overlaySetBands(bands []float64) {
	if len(bands) == 0 {
		return
	}
	C.overlaySetBands((*C.double)(unsafe.Pointer(&bands[0])), C.int(len(bands)))
}
