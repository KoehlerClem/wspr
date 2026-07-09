package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// parakeetWorkerScript is a tiny Python program that loads a Parakeet model
// once and then transcribes file paths fed on stdin, one JSON line per result.
// Keeping the model resident avoids the ~1.2s reload the CLI pays every call.
//
// parakeet-mlx's own load_audio shells out to ffmpeg, which wspr no longer
// requires. wspr always records 16 kHz mono s16 WAV — exactly the format the
// model wants — so the loader is replaced with a stdlib WAV reader; anything
// unexpected falls back to the original ffmpeg path. parakeet.py imports
// load_audio by name, so its binding is patched too.
const parakeetWorkerScript = `
import sys, json, wave
try:
    import numpy as np
    import mlx.core as mx
    from parakeet_mlx import from_pretrained
    from parakeet_mlx import audio as _pa
    from parakeet_mlx import parakeet as _pk

    _ffmpeg_load = _pa.load_audio

    def _load_audio(path, rate, dtype=mx.bfloat16):
        try:
            with wave.open(str(path), "rb") as w:
                if (w.getnchannels(), w.getsampwidth(), w.getframerate()) == (1, 2, rate):
                    pcm = w.readframes(w.getnframes())
                    return mx.array(np.frombuffer(pcm, np.int16)).astype(mx.float32) / 32768.0
        except Exception:
            pass
        return _ffmpeg_load(path, rate, dtype)

    _pa.load_audio = _load_audio
    _pk.load_audio = _load_audio

    model = from_pretrained(sys.argv[1])
except Exception as e:
    print(json.dumps({"error": "load: " + str(e)}), flush=True)
    sys.exit(1)
print(json.dumps({"ready": True}), flush=True)
while True:
    line = sys.stdin.readline()
    if not line:
        break
    path = line.strip()
    if not path:
        continue
    try:
        print(json.dumps({"text": model.transcribe(path).text}), flush=True)
    except Exception as e:
        print(json.dumps({"error": str(e)}), flush=True)
`

type workerMsg struct {
	Ready bool   `json:"ready"`
	Text  string `json:"text"`
	Error string `json:"error"`
}

// parakeetWorker is a persistent Python process with a Parakeet model loaded.
type parakeetWorker struct {
	model string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
}

var (
	pkMu     sync.Mutex // serializes access to pkWorker
	pkWorker *parakeetWorker
)

// startParakeetWorker launches a worker and blocks until the model is loaded.
func startParakeetWorker(model string) (*parakeetWorker, error) {
	cmd := exec.Command("uv", "run", "--with", "parakeet-mlx",
		"python", "-c", parakeetWorkerScript, model)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Capture the worker's stderr into wspr.log — a .app has no terminal, so a
	// Python traceback or uv/download progress would otherwise be lost. The
	// reader goroutine also drains the pipe so the worker can't block on it.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go logStream("parakeet", stderr)
	w := &parakeetWorker{model: model, cmd: cmd, stdin: stdin, out: bufio.NewReader(stdout)}

	msg, err := w.readMsg(90 * time.Second) // model load can be slow first time
	if err != nil {
		w.stop()
		return nil, fmt.Errorf("worker did not start: %w", err)
	}
	if msg.Error != "" {
		w.stop()
		return nil, fmt.Errorf("worker: %s", msg.Error)
	}
	if !msg.Ready {
		w.stop()
		return nil, fmt.Errorf("worker: unexpected reply")
	}
	return w, nil
}

// readMsg reads worker stdout until a JSON message arrives or d elapses,
// skipping any non-JSON chatter.
func (w *parakeetWorker) readMsg(d time.Duration) (workerMsg, error) {
	type res struct {
		m   workerMsg
		err error
	}
	ch := make(chan res, 1)
	go func() {
		for {
			line, err := w.out.ReadString('\n')
			if err != nil {
				ch <- res{err: err}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var m workerMsg
			if json.Unmarshal([]byte(line), &m) == nil {
				ch <- res{m: m}
				return
			}
		}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(d):
		return workerMsg{}, fmt.Errorf("timed out")
	}
}

func (w *parakeetWorker) transcribe(audioPath string) (string, error) {
	if _, err := io.WriteString(w.stdin, audioPath+"\n"); err != nil {
		return "", err
	}
	msg, err := w.readMsg(30 * time.Second)
	if err != nil {
		return "", err
	}
	if msg.Error != "" {
		return "", fmt.Errorf("parakeet: %s", msg.Error)
	}
	return strings.TrimSpace(msg.Text), nil
}

func (w *parakeetWorker) stop() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
}

// warmParakeet preloads the worker for model so the first dictation is fast.
func warmParakeet(model string) {
	defer recoverLog("warmParakeet")
	pkMu.Lock()
	defer pkMu.Unlock()
	if pkWorker != nil && pkWorker.model == model {
		return
	}
	if pkWorker != nil {
		pkWorker.stop()
		pkWorker = nil
	}
	t0 := time.Now()
	w, err := startParakeetWorker(model)
	if err != nil {
		logErr(fmt.Errorf("parakeet warm-up failed (falling back to cold mode): %w", err))
		return
	}
	pkWorker = w
	logInfo(fmt.Sprintf("parakeet model loaded and kept warm in %.1fs", time.Since(t0).Seconds()))
}

// stopParakeet shuts the worker down (called on quit).
func stopParakeet() {
	pkMu.Lock()
	defer pkMu.Unlock()
	if pkWorker != nil {
		pkWorker.stop()
		pkWorker = nil
	}
}

// transcribeParakeetWarm transcribes via the persistent worker, starting it if
// needed. The bool is false if the worker is unavailable, so the caller can
// fall back to the cold CLI path.
func transcribeParakeetWarm(model, audioPath string) (string, bool) {
	pkMu.Lock()
	defer pkMu.Unlock()
	if pkWorker == nil || pkWorker.model != model {
		if pkWorker != nil {
			pkWorker.stop()
			pkWorker = nil
		}
		w, err := startParakeetWorker(model)
		if err != nil {
			logErr(fmt.Errorf("parakeet worker unavailable: %w", err))
			return "", false
		}
		pkWorker = w
	}
	text, err := pkWorker.transcribe(audioPath)
	if err != nil {
		logErr(fmt.Errorf("parakeet worker error (restarting): %w", err))
		pkWorker.stop()
		pkWorker = nil
		return "", false
	}
	return text, true
}
