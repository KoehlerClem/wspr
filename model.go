package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// modelsDir holds whisper.cpp GGML models fetched by `wspr download`.
func modelsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wspr", "models")
}

// whisperModelPath resolves a whisper model setting to a .bin file path.
// A value containing "/" or ending in ".bin" is treated as a literal path;
// a bare name like "large-v3-turbo" resolves to <modelsDir>/ggml-<name>.bin.
func whisperModelPath(model string) string {
	if strings.Contains(model, "/") || strings.HasSuffix(model, ".bin") {
		return expandHome(model)
	}
	return filepath.Join(modelsDir(), "ggml-"+model+".bin")
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// downloadModel fetches a whisper.cpp GGML model from Hugging Face into
// modelsDir(). With no name it uses the configured whisper model.
func downloadModel(name string) {
	if name == "" {
		cfg, _, _ := loadConfig(configPath())
		name = cfg.WhisperModel
	}
	if strings.Contains(name, "/") || strings.HasSuffix(name, ".bin") {
		fatal(fmt.Errorf("%q looks like a path — pass a model name such as large-v3-turbo", name))
	}
	dest := whisperModelPath(name)
	if _, err := os.Stat(dest); err == nil {
		fmt.Println("already present:", dest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal(err)
	}
	url := whisperModelURL(name)
	fmt.Printf("downloading whisper model %q\n  %s\n  -> %s\n\n", name, url, dest)

	tmp := dest + ".part"
	cmd := exec.Command("curl", "-L", "--fail", "--progress-bar", "-o", tmp, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		fatal(fmt.Errorf("download failed (is %q a valid model name?): %w", name, err))
	}
	if err := os.Rename(tmp, dest); err != nil {
		fatal(err)
	}
	fmt.Println("\nsaved", dest)
}

func whisperModelURL(name string) string {
	return "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-" + name + ".bin"
}

// Background download of the configured model, driven by the setup guide
// (setup.go). The state feeds the guide's model row and makes
// startModelDownload idempotent.
type dlState int

const (
	dlIdle    dlState = iota
	dlRunning         // fetch in progress
	dlDone            // fetched (or already on disk) this session
	dlFailed          // last attempt failed — the row offers a retry
)

var (
	modelDlMu    sync.Mutex
	modelDlState = dlIdle
)

func modelDlIs(s dlState) bool {
	modelDlMu.Lock()
	defer modelDlMu.Unlock()
	return modelDlState == s
}

// modelReady reports whether the configured model is usable: fetched this
// session, or already on disk from an earlier run.
func modelReady(cfg Config) bool {
	modelDlMu.Lock()
	defer modelDlMu.Unlock()
	switch modelDlState {
	case dlRunning:
		return false
	case dlDone:
		return true
	}
	return modelOnDisk(cfg)
}

func modelOnDisk(cfg Config) bool {
	if cfg.Engine == "whisper" {
		_, err := os.Stat(whisperModelPath(cfg.WhisperModel))
		return err == nil
	}
	return parakeetModelCached(cfg.ParakeetModel)
}

// parakeetModelCached reports whether the Hugging Face cache already holds a
// snapshot of repo — parakeet-mlx downloads models through huggingface_hub.
func parakeetModelCached(repo string) bool {
	base := os.Getenv("HF_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache", "huggingface")
	}
	dir := filepath.Join(base, "hub",
		"models--"+strings.ReplaceAll(repo, "/", "--"), "snapshots")
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// startModelDownload fetches the configured model in the background. Safe to
// call repeatedly: a running or completed download is left alone, a failed one
// is retried. For parakeet the "download" is the worker's first model load, so
// a successful fetch also leaves the model warm and ready to transcribe.
func startModelDownload(cfg Config) {
	modelDlMu.Lock()
	if modelDlState == dlRunning || modelDlState == dlDone {
		modelDlMu.Unlock()
		return
	}
	modelDlState = dlRunning
	modelDlMu.Unlock()

	go func() {
		defer recoverLog("startModelDownload")
		err := fetchModel(cfg)
		modelDlMu.Lock()
		if err != nil {
			modelDlState = dlFailed
		} else {
			modelDlState = dlDone
		}
		modelDlMu.Unlock()
		if err != nil {
			logErr(fmt.Errorf("model download: %w", err))
		} else {
			logInfo("model ready: " + cfg.Engine + " / " + activeModel(cfg))
		}
	}()
}

func fetchModel(cfg Config) error {
	if cfg.Engine == "whisper" {
		return fetchWhisperModel(cfg.WhisperModel)
	}
	warmParakeet(cfg.ParakeetModel) // the first load downloads from Hugging Face
	if !parakeetModelCached(cfg.ParakeetModel) {
		return fmt.Errorf("parakeet model %q did not download (see log above)", cfg.ParakeetModel)
	}
	return nil
}

// fetchWhisperModel is downloadModel's quiet sibling for the setup guide: it
// returns errors instead of printing and exiting, since it runs behind a GUI.
func fetchWhisperModel(name string) error {
	dest := whisperModelPath(name)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	logInfo("downloading whisper model " + name)
	tmp := dest + ".part"
	cmd := exec.Command("curl", "-L", "--fail", "--silent", "--show-error", "-o", tmp, whisperModelURL(name))
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("whisper model %q: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmp, dest)
}

// modelInfo describes one curated speech-to-text model. Stats are taken from
// the official model cards and the HuggingFace Open ASR Leaderboard.
type modelInfo struct {
	engine    string
	name      string // value to pass to --model
	params    string
	size      string // download / disk size
	langs     string
	accuracy  string
	speed     string
	released  string
	isDefault bool
	summary   string
}

// catalog lists only the latest model of each family and the best performers,
// restricted to models this tool can actually run on-device.
var catalog = []modelInfo{
	{
		engine: "parakeet", name: "mlx-community/parakeet-tdt-0.6b-v3",
		params: "600M", size: "~600 MB", langs: "25 European languages",
		accuracy: "WER 6.34% multilingual avg, 4.85% English",
		speed:    "RTFx ~3300 (extremely fast)", released: "2025-08",
		isDefault: true,
		summary:   "Best all-rounder — multilingual, fast, near-top accuracy.",
	},
	{
		engine: "parakeet", name: "mlx-community/parakeet-tdt-0.6b-v2",
		params: "600M", size: "~600 MB", langs: "English only",
		accuracy: "WER 6.05% (Open ASR English avg)",
		speed:    "RTFx ~3400 (extremely fast)", released: "2025-05",
		summary: "English-only specialist — marginally sharper on English.",
	},
	{
		engine: "whisper", name: "large-v3-turbo",
		params: "809M", size: "1.5 GB", langs: "99 languages",
		accuracy: "WER ~8% (Open ASR avg)",
		speed:    "~8x faster than large-v3", released: "2024-10",
		isDefault: true,
		summary:   "Recommended Whisper model — fast with strong accuracy.",
	},
	{
		engine: "whisper", name: "large-v3",
		params: "1550M", size: "2.9 GB", langs: "99 languages",
		accuracy: "WER ~7.4% (Open ASR avg)",
		speed:    "baseline speed", released: "2023-11",
		summary: "Highest Whisper accuracy — larger and slower.",
	},
	{
		engine: "whisper", name: "large-v3-turbo-q5_0",
		params: "809M", size: "547 MB", langs: "99 languages",
		accuracy: "WER ~8% (quantized)",
		speed:    "~8x faster than large-v3", released: "2024-10",
		summary: "Quantized turbo — smallest download, near-turbo quality.",
	},
}

// listModels prints the curated catalog with per-model stats.
func listModels() {
	cfg, _, _ := loadConfig(configPath())
	current := activeModel(cfg)

	fmt.Println("wspr models — curated latest & best-performing speech-to-text models")
	fmt.Println("(all run fully on-device)")
	fmt.Println()

	section := func(engine, title, runner string) {
		fmt.Printf("%s   --engine %s\n", title, engine)
		fmt.Printf("  %s\n\n", runner)
		for _, m := range catalog {
			if m.engine != engine {
				continue
			}
			tag := ""
			if m.isDefault {
				tag += "  [default]"
			}
			if cfg.Engine == engine && m.name == current {
				tag += "  [configured]"
			}
			fmt.Printf("  %s%s\n", m.name, tag)
			fmt.Printf("    %s · %s · %s · released %s\n", m.params, m.size, m.langs, m.released)
			fmt.Printf("    %s · %s\n", m.accuracy, m.speed)
			fmt.Printf("    %s\n\n", m.summary)
		}
	}

	section("parakeet", "PARAKEET ENGINE (NVIDIA)", "on-device via parakeet-mlx, Apple Silicon")
	section("whisper", "WHISPER ENGINE (OpenAI)", "on-device via whisper.cpp")

	fmt.Println("Select a model (persists to the config file):")
	fmt.Println("  wspr --engine parakeet --model mlx-community/parakeet-tdt-0.6b-v2 --save")
	fmt.Println("  wspr --engine whisper  --model large-v3 --save")
	fmt.Println("Whisper models must be downloaded first:  wspr download large-v3")
	fmt.Println()
	fmt.Println("Not listed: NVIDIA's Canary models (e.g. canary-qwen-2.5b — WER ~5.6%, #1 on")
	fmt.Println("the Open ASR leaderboard) are more accurate, but require NVIDIA NeMo and")
	fmt.Println("cannot run through parakeet-mlx, so they are not selectable here.")
}
