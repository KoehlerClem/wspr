package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the on-disk configuration, stored as JSON at configPath().
type Config struct {
	Hotkey        string `json:"hotkey"`         // push-to-talk combo, e.g. "ctrl+option+space" or "fn"
	ToggleHotkey  string `json:"toggle_hotkey"`  // enable/disable dictation
	Mode          string `json:"mode"`           // "hold" (push-to-talk) or "toggle"
	Engine        string `json:"engine"`         // "parakeet" (NVIDIA) or "whisper"
	ParakeetModel string `json:"parakeet_model"` // Hugging Face repo for parakeet-mlx
	WhisperModel  string `json:"whisper_model"`  // whisper.cpp model name or .bin path
	Language      string `json:"language"`       // optional ISO-639-1 hint (whisper only)
	Mic           string `json:"mic"`            // avfoundation audio device, e.g. ":0"
	Paste         bool   `json:"paste"`          // auto-paste after transcription
	Sounds        bool   `json:"sounds"`         // play sound feedback
}

func defaultConfig() Config {
	return Config{
		Hotkey:        "ctrl+option+space",
		ToggleHotkey:  "ctrl+option+t",
		Mode:          "hold",
		Engine:        "parakeet",
		ParakeetModel: "mlx-community/parakeet-tdt-0.6b-v3",
		WhisperModel:  "large-v3-turbo",
		Language:      "",
		Mic:           "",
		Paste:         true,
		Sounds:        false,
	}
}

// activeModel returns the model name in use for the configured engine.
func activeModel(cfg Config) string {
	if cfg.Engine == "whisper" {
		return cfg.WhisperModel
	}
	return cfg.ParakeetModel
}

// configPath returns ~/.config/wspr/config.json (override with $WSPR_CONFIG).
func configPath() string {
	if p := os.Getenv("WSPR_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wspr", "config.json")
}

// loadConfig reads the config file, falling back to defaults for any missing
// fields. The bool reports whether a file already existed.
func loadConfig(path string) (Config, bool, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, true, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, true, nil
}

func saveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Modifier bitmask, shared with the C hotkey code (see input_darwin.m).
const (
	modCtrl   = 1 << 0
	modShift  = 1 << 1
	modOption = 1 << 2
	modCmd    = 1 << 3
	modFn     = 1 << 4

	// Side-specific bits. A side bit rides alongside its generic bit: pressing
	// the left command sets modCmd|modCmdLeft, so a plain "cmd" binding still
	// matches either side, while "lcmd"/"rcmd" pin it to one. (fn has no sides.)
	modCtrlLeft    = 1 << 5
	modCtrlRight   = 1 << 6
	modShiftLeft   = 1 << 7
	modShiftRight  = 1 << 8
	modOptionLeft  = 1 << 9
	modOptionRight = 1 << 10
	modCmdLeft     = 1 << 11
	modCmdRight    = 1 << 12
)

// hotkeySpec is a parsed hotkey: a modifier bitmask plus an optional key.
type hotkeySpec struct {
	mods    int // OR of mod* bits
	keyCode int // macOS virtual key code, or -1 for a modifier-only hotkey
}

var modMap = map[string]int{
	"ctrl": modCtrl, "control": modCtrl,
	"lctrl": modCtrl | modCtrlLeft, "leftctrl": modCtrl | modCtrlLeft, "left_control": modCtrl | modCtrlLeft,
	"rctrl": modCtrl | modCtrlRight, "rightctrl": modCtrl | modCtrlRight, "right_control": modCtrl | modCtrlRight,

	"shift": modShift,
	"lshift": modShift | modShiftLeft, "leftshift": modShift | modShiftLeft, "left_shift": modShift | modShiftLeft,
	"rshift": modShift | modShiftRight, "rightshift": modShift | modShiftRight, "right_shift": modShift | modShiftRight,

	"option": modOption, "opt": modOption, "alt": modOption,
	"lopt": modOption | modOptionLeft, "lalt": modOption | modOptionLeft, "leftoption": modOption | modOptionLeft, "left_option": modOption | modOptionLeft,
	"ropt": modOption | modOptionRight, "ralt": modOption | modOptionRight, "rightoption": modOption | modOptionRight, "right_option": modOption | modOptionRight,

	"cmd": modCmd, "command": modCmd, "super": modCmd,
	"lcmd": modCmd | modCmdLeft, "leftcmd": modCmd | modCmdLeft, "left_command": modCmd | modCmdLeft,
	"rcmd": modCmd | modCmdRight, "rightcmd": modCmd | modCmdRight, "right_command": modCmd | modCmdRight,

	"fn": modFn, "function": modFn, "globe": modFn,
}

// keyMap maps key names to macOS virtual key codes.
var keyMap = map[string]int{
	"a": 0, "b": 11, "c": 8, "d": 2, "e": 14, "f": 3, "g": 5, "h": 4,
	"i": 34, "j": 38, "k": 40, "l": 37, "m": 46, "n": 45, "o": 31, "p": 35,
	"q": 12, "r": 15, "s": 1, "t": 17, "u": 32, "v": 9, "w": 13, "x": 7,
	"y": 16, "z": 6,
	"0": 29, "1": 18, "2": 19, "3": 20, "4": 21, "5": 23, "6": 22, "7": 26,
	"8": 28, "9": 25,
	"space": 49, "return": 36, "enter": 36, "tab": 48, "delete": 51,
	"escape": 53, "esc": 53,
	"left": 123, "right": 124, "down": 125, "up": 126,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	"minus": 27, "equal": 24, "leftbracket": 33, "rightbracket": 30,
	"semicolon": 41, "quote": 39, "comma": 43, "period": 47, "slash": 44,
	"backslash": 42, "grave": 50,
}

// parseHotkey turns a string like "ctrl+option+space", "fn", or "f5" into a
// hotkeySpec. Any combination is allowed: modifiers plus a key, a single key,
// or a single bare modifier.
func parseHotkey(s string) (hotkeySpec, error) {
	spec := hotkeySpec{keyCode: -1}
	for _, p := range strings.Split(strings.ToLower(strings.TrimSpace(s)), "+") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if m, ok := modMap[p]; ok {
			spec.mods |= m
			continue
		}
		if k, ok := keyMap[p]; ok {
			if spec.keyCode >= 0 {
				return spec, fmt.Errorf("hotkey %q has more than one key", s)
			}
			spec.keyCode = k
			continue
		}
		return spec, fmt.Errorf("unknown key or modifier %q in %q", p, s)
	}
	if spec.mods == 0 && spec.keyCode < 0 {
		return spec, fmt.Errorf("hotkey %q is empty", s)
	}
	return spec, nil
}
