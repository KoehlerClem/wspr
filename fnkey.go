package main

import (
	"os/exec"
	"strings"
)

// hotkeyUsesFn reports whether either configured hotkey involves the fn key.
func hotkeyUsesFn(cfg Config) bool {
	for _, s := range []string{cfg.Hotkey, cfg.ToggleHotkey} {
		if spec, err := parseHotkey(s); err == nil && spec.mods&modFn != 0 {
			return true
		}
	}
	return false
}

// freeFnKey disables the macOS standalone-fn action (emoji picker / input
// source / dictation) so the fn key can serve as a wspr hotkey without the
// emoji picker popping up. It is a no-op if the action is already off. The
// change is reversible under System Settings → Keyboard → "Press 🌐 key to".
func freeFnKey() {
	// The fn-key action is a per-host (ByHost) preference, hence -currentHost.
	const domain, key = "com.apple.HIToolbox", "AppleFnUsageType"
	out, _ := exec.Command("defaults", "-currentHost", "read", domain, key).Output()
	if strings.TrimSpace(string(out)) == "0" {
		return // already "Do Nothing"
	}
	if err := exec.Command("defaults", "-currentHost", "write", domain, key, "-int", "0").Run(); err != nil {
		logErr(err)
		return
	}
	_ = exec.Command("killall", "cfprefsd").Run() // flush the prefs cache
	logInfo("fn/🌐 key set to \"Do Nothing\" so it won't open the emoji picker")
	logInfo("  reverse it any time in System Settings → Keyboard → \"Press 🌐 key to\"")
}
