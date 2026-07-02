package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// transcribe runs the configured local engine on the WAV file and returns the
// recognized text. Nothing leaves the machine.
func transcribe(cfg Config, audioPath string) (string, error) {
	switch cfg.Engine {
	case "parakeet":
		return transcribeParakeet(cfg, audioPath)
	case "whisper":
		return transcribeWhisper(cfg, audioPath)
	default:
		return "", fmt.Errorf("unknown engine %q (use \"parakeet\" or \"whisper\")", cfg.Engine)
	}
}

// transcribeParakeet transcribes via the warm worker (model kept resident),
// falling back to the cold parakeet-mlx CLI if the worker is unavailable.
func transcribeParakeet(cfg Config, audioPath string) (string, error) {
	if text, ok := transcribeParakeetWarm(cfg.ParakeetModel, audioPath); ok {
		return text, nil
	}
	return transcribeParakeetCold(cfg, audioPath)
}

// transcribeParakeetCold runs the parakeet-mlx CLI, which reloads the model on
// every call. parakeet-mlx writes output files, so we point it at a fresh temp
// directory and read back whatever .txt file it produces.
func transcribeParakeetCold(cfg Config, audioPath string) (string, error) {
	outDir, err := os.MkdirTemp("", "wspr-parakeet-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(outDir)

	cmd := exec.Command("parakeet-mlx", audioPath,
		"--model", cfg.ParakeetModel,
		"--output-format", "txt",
		"--output-dir", outDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("parakeet-mlx failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}

	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			return readTranscript(filepath.Join(outDir, e.Name()))
		}
	}
	return "", fmt.Errorf("parakeet-mlx produced no transcript")
}

// transcribeWhisper runs Whisper locally via whisper.cpp (the `whisper-cpp`
// binary from Homebrew).
func transcribeWhisper(cfg Config, audioPath string) (string, error) {
	model := whisperModelPath(cfg.WhisperModel)
	if _, err := os.Stat(model); err != nil {
		return "", fmt.Errorf("whisper model not found: %s — run: wspr download %s", model, cfg.WhisperModel)
	}

	prefix := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	txtPath := prefix + ".txt"
	_ = os.Remove(txtPath)
	defer os.Remove(txtPath)

	args := []string{"-m", model, "-f", audioPath, "-nt", "-otxt", "-of", prefix}
	if cfg.Language != "" {
		args = append(args, "-l", cfg.Language)
	}
	cmd := exec.Command("whisper-cpp", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("whisper-cpp failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return readTranscript(txtPath)
}

func readTranscript(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading transcript: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
