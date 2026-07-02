package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// recorder wraps a running ffmpeg process capturing microphone audio to a
// temporary 16 kHz mono WAV file.
type recorder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	path  string
	start time.Time
}

// startRecording launches ffmpeg against the given avfoundation device
// (e.g. ":0"). While recording, ffmpeg also streams a raw-PCM copy over an
// extra pipe; readSpectrum turns it into a frequency spectrum and passes each
// to onSpectrum, which may be nil.
func startRecording(mic string, onSpectrum func([]float64)) (*recorder, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("wspr-%d.wav", time.Now().UnixNano()))

	// Pipe for a live copy of the raw audio: it becomes fd 3 in the child, and
	// ffmpeg writes a second, raw-PCM output to /dev/fd/3 that drives the meter.
	levelR, levelW, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "avfoundation",
		"-i", mic,
		// the WAV that gets transcribed
		"-ac", "1", "-ar", "16000", path,
		// a raw-PCM copy to fd 3, read live to drive the level meter
		"-ac", "1", "-ar", "16000", "-f", "s16le", "/dev/fd/3",
	)
	cmd.ExtraFiles = []*os.File{levelW} // becomes the child's fd 3

	stdin, err := cmd.StdinPipe()
	if err != nil {
		levelR.Close()
		levelW.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		levelR.Close()
		levelW.Close()
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}
	levelW.Close() // the child holds its own copy

	go readSpectrum(levelR, onSpectrum)

	return &recorder{cmd: cmd, stdin: stdin, path: path, start: time.Now()}, nil
}

// Stop asks ffmpeg to finalize the file gracefully and returns the WAV path
// and how long recording lasted.
func (r *recorder) Stop() (string, time.Duration, error) {
	dur := time.Since(r.start)
	// "q" tells ffmpeg to quit and write a valid WAV header.
	_, _ = io.WriteString(r.stdin, "q\n")
	_ = r.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		return r.path, dur, err
	case <-time.After(3 * time.Second):
		_ = r.cmd.Process.Kill()
		<-done
		return r.path, dur, nil
	}
}

// audioDevice is one avfoundation audio input device.
type audioDevice struct {
	index int
	name  string
}

// audioDevices lists the avfoundation audio input devices by running ffmpeg's
// device enumerator and parsing its output. The index is what --mic expects.
func audioDevices() []audioDevice {
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-f", "avfoundation",
		"-list_devices", "true", "-i", "").CombinedOutput()
	var devs []audioDevice
	inAudio := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "AVFoundation audio devices:") {
			inAudio = true
			continue
		}
		if strings.Contains(line, "AVFoundation video devices:") {
			inAudio = false
			continue
		}
		if !inAudio {
			continue
		}
		// A device line reads "[AVFoundation indev @ 0x..] [0] Name": drop the
		// "[AVFoundation indev @ ..] " prefix, then parse "[N] Name".
		rest := line
		if i := strings.Index(rest, "] "); i >= 0 {
			rest = rest[i+2:]
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "[") {
			continue
		}
		j := strings.Index(rest, "]")
		if j < 0 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(rest[1:j]))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(rest[j+1:]); name != "" {
			devs = append(devs, audioDevice{index: idx, name: name})
		}
	}
	return devs
}

// isContinuityMic reports whether a device name looks like an iPhone/iPad
// Continuity device. wspr never picks one automatically — selecting it makes
// macOS try to connect to the phone instead of just recording.
func isContinuityMic(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "iphone") || strings.Contains(l, "ipad")
}

// activeMicName resolves the configured microphone to a concrete device name.
// An explicit choice wins if that device is present; otherwise it falls back to
// the first local device, skipping Continuity (iPhone) devices.
func activeMicName(cfgMic string, devs []audioDevice) string {
	if cfgMic != "" {
		for _, d := range devs {
			if d.name == cfgMic {
				return d.name
			}
		}
	}
	for _, d := range devs {
		if !isContinuityMic(d.name) {
			return d.name
		}
	}
	if len(devs) > 0 {
		return devs[0].name
	}
	return ""
}

// resolveMicArg turns the configured microphone into an ffmpeg avfoundation
// input argument. Addressing the device by name keeps it stable even when the
// numeric device indices shift (e.g. when an iPhone appears or disappears).
func resolveMicArg(cfgMic string, devs []audioDevice) string {
	if name := activeMicName(cfgMic, devs); name != "" {
		return ":" + name
	}
	return ":0"
}
