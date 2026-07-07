package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
)

// captureRate is the sample rate wspr records at — 16 kHz mono s16, the format
// the transcription engines expect. miniaudio resamples from whatever the
// hardware delivers.
const captureRate = 16000

// wavHeaderSize is the byte size of a canonical PCM WAV header.
const wavHeaderSize = 44

// audioCtx is the process-wide miniaudio (CoreAudio) context, shared by device
// enumeration and capture. It is created on first use and lives until exit.
var (
	audioCtxOnce sync.Once
	audioCtx     *malgo.AllocatedContext
	audioCtxErr  error
)

func audioContext() (*malgo.AllocatedContext, error) {
	audioCtxOnce.Do(func() {
		audioCtx, audioCtxErr = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	})
	if audioCtxErr != nil {
		return nil, fmt.Errorf("audio context: %w", audioCtxErr)
	}
	return audioCtx, nil
}

// recorder captures microphone audio to a temporary 16 kHz mono WAV file.
// The audio callback hands PCM chunks to a drain goroutine over pcm; the drain
// appends them to the WAV file and feeds the spectrum for the level meter.
type recorder struct {
	dev      *malgo.Device
	f        *os.File
	path     string
	start    time.Time
	pcm      chan []byte
	done     chan struct{}
	dropped  atomic.Int64
	writeErr error
}

// startRecording opens the given capture device (nil = system default) and
// records until Stop. Each fftSize window of samples is reduced to a spectrum
// and passed to onSpectrum, which may be nil.
func startRecording(devID *malgo.DeviceID, onSpectrum func([]float64)) (*recorder, error) {
	ctx, err := audioContext()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(os.TempDir(), fmt.Sprintf("wspr-%d.wav", time.Now().UnixNano()))
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	// Header placeholder — the sizes are only known at Stop, which rewrites it.
	if _, err := f.Write(make([]byte, wavHeaderSize)); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}

	r := &recorder{
		f:     f,
		path:  path,
		start: time.Now(),
		pcm:   make(chan []byte, 256),
		done:  make(chan struct{}),
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = captureRate
	if devID != nil {
		cfg.Capture.DeviceID = devID.Pointer()
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		// Runs on the audio thread: copy the chunk and hand it off. Dropping
		// beats blocking here — a full channel means the drain has stalled.
		Data: func(_, input []byte, _ uint32) {
			buf := make([]byte, len(input))
			copy(buf, input)
			select {
			case r.pcm <- buf:
			default:
				r.dropped.Add(1)
			}
		},
	})
	if err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("open microphone: %w", err)
	}
	r.dev = dev

	go r.drain(onSpectrum)

	if err := dev.Start(); err != nil {
		dev.Uninit()
		close(r.pcm)
		<-r.done
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("start microphone: %w", err)
	}
	return r, nil
}

// drain consumes PCM chunks off the channel, appending them to the WAV file
// and feeding the spectrum. It runs until the channel is closed by Stop.
func (r *recorder) drain(onSpectrum func([]float64)) {
	defer close(r.done)
	feed := spectrumFeed{onSpectrum: onSpectrum}
	for buf := range r.pcm {
		if r.writeErr == nil {
			if _, err := r.f.Write(buf); err != nil {
				r.writeErr = err
			}
		}
		feed.push(buf)
	}
}

// Stop ends the capture, finalizes the WAV file and returns its path and how
// long the recording lasted.
func (r *recorder) Stop() (string, time.Duration, error) {
	dur := time.Since(r.start)
	r.dev.Uninit() // stops the device; no data callbacks run past this point
	close(r.pcm)
	<-r.done
	if n := r.dropped.Load(); n > 0 {
		logErr(fmt.Errorf("recording overrun — dropped %d audio chunks", n))
	}
	err := r.finalizeWAV()
	if r.writeErr != nil && err == nil {
		err = r.writeErr
	}
	return r.path, dur, err
}

// finalizeWAV rewrites the header with the now-known data size and closes the
// file.
func (r *recorder) finalizeWAV() error {
	info, err := r.f.Stat()
	if err != nil {
		r.f.Close()
		return err
	}
	dataSize := uint32(info.Size() - wavHeaderSize)

	var h [wavHeaderSize]byte
	le := binary.LittleEndian
	copy(h[0:], "RIFF")
	le.PutUint32(h[4:], 36+dataSize)
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	le.PutUint32(h[16:], 16)                // fmt chunk size
	le.PutUint16(h[20:], 1)                 // PCM
	le.PutUint16(h[22:], 1)                 // mono
	le.PutUint32(h[24:], captureRate)       // sample rate
	le.PutUint32(h[28:], captureRate*2)     // byte rate (16-bit mono)
	le.PutUint16(h[32:], 2)                 // block align
	le.PutUint16(h[34:], 16)                // bits per sample
	copy(h[36:], "data")
	le.PutUint32(h[40:], dataSize)

	if _, err := r.f.WriteAt(h[:], 0); err != nil {
		r.f.Close()
		return err
	}
	return r.f.Close()
}

// audioDevice is one capture device as reported by miniaudio.
type audioDevice struct {
	id   malgo.DeviceID
	name string
}

// audioDevices lists the audio input devices. The name is what --mic expects.
func audioDevices() []audioDevice {
	ctx, err := audioContext()
	if err != nil {
		logErr(err)
		return nil
	}
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		logErr(fmt.Errorf("listing audio devices: %w", err))
		return nil
	}
	devs := make([]audioDevice, 0, len(infos))
	for i := range infos {
		devs = append(devs, audioDevice{id: infos[i].ID, name: infos[i].Name()})
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

// resolveMicID turns the configured microphone into the capture device ID to
// record from, or nil for the system default. Devices are matched by name so
// the choice stays stable when device IDs shift between sessions.
func resolveMicID(cfgMic string, devs []audioDevice) *malgo.DeviceID {
	name := activeMicName(cfgMic, devs)
	for i := range devs {
		if devs[i].name == name {
			id := devs[i].id
			return &id
		}
	}
	return nil
}
