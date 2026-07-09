package main

import (
	"os"
	"strings"
	"testing"
)

func TestParakeetWithoutFFmpeg(t *testing.T) {
	fakebin := os.Getenv("WSPR_FAKEBIN")
	if fakebin == "" {
		t.Skip("set WSPR_FAKEBIN to a dir with uv but no ffmpeg")
	}
	t.Setenv("PATH", fakebin+":/usr/bin:/bin")
	w, err := startParakeetWorker("mlx-community/parakeet-tdt-0.6b-v3")
	if err != nil {
		t.Fatal(err)
	}
	defer w.stop()
	text, err := w.transcribe(os.Getenv("WSPR_TESTWAV"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("transcript: %q", text)
	if !strings.Contains(strings.ToLower(text), "one") {
		t.Fatalf("unexpected transcript: %q", text)
	}
}
