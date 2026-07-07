package main

import (
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// TestLiveCapture records one second from the default device and checks that a
// valid, plausibly-sized WAV comes out. It needs microphone access, so it is
// skipped unless explicitly requested:
//
//	WSPR_LIVE=1 go test -run TestLiveCapture -v
func TestLiveCapture(t *testing.T) {
	if os.Getenv("WSPR_LIVE") == "" {
		t.Skip("set WSPR_LIVE=1 to run the live microphone test")
	}
	var spectra int
	rec, err := startRecording(nil, func([]float64) { spectra++ })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1 * time.Second)
	path, dur, err := rec.Stop()
	defer os.Remove(path)
	if err != nil {
		t.Fatal(err)
	}
	if dur < 900*time.Millisecond {
		t.Fatalf("duration = %v, want ~1s", dur)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < wavHeaderSize {
		t.Fatalf("file too small: %d bytes", len(data))
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || string(data[36:40]) != "data" {
		t.Fatalf("bad WAV magic: % x", data[:44])
	}
	if r := binary.LittleEndian.Uint32(data[24:28]); r != captureRate {
		t.Fatalf("sample rate = %d, want %d", r, captureRate)
	}
	if n := binary.LittleEndian.Uint32(data[40:44]); int(n) != len(data)-wavHeaderSize {
		t.Fatalf("data size = %d, file has %d payload bytes", n, len(data)-wavHeaderSize)
	}
	// ~1 s of 16 kHz s16 mono is ~32000 bytes; accept a generous window.
	payload := len(data) - wavHeaderSize
	if payload < 20000 || payload > 60000 {
		t.Fatalf("payload = %d bytes, want roughly 32000 for 1s", payload)
	}
	t.Logf("captured %d bytes, %d spectrum windows", payload, spectra)
	if spectra == 0 {
		t.Fatal("no spectrum windows were emitted")
	}
}
