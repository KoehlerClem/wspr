package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// hotkeyHint describes how to trigger dictation, given the recording mode.
func hotkeyHint(cfg Config) string {
	if cfg.Mode == "toggle" {
		return "tap " + cfg.Hotkey + " to start/stop dictation"
	}
	return "hold " + cfg.Hotkey + " to dictate"
}

// applyHotkeys points the key monitor at the hotkeys from the config. A combo
// that fails to parse is logged and simply left unregistered.
func applyHotkeys(cfg Config) {
	if spec, err := parseHotkey(cfg.Hotkey); err == nil {
		inputSetRecordTrigger(spec.keyCode, spec.mods)
	} else {
		logErr(err)
	}
	if spec, err := parseHotkey(cfg.ToggleHotkey); err == nil {
		inputSetToggleTrigger(spec.keyCode, spec.mods)
	} else {
		logErr(err)
	}
}

// engineLoop starts the global keyboard listener and runs the dictation event
// loop. It is started as a goroutine by onMenuReady, and also listens to the
// menu items so the menu and the hotkeys stay in sync.
func engineLoop(cfg Config, m *menu) {
	inputStart() // install the global key monitor (silent until Accessibility)
	applyHotkeys(cfg)
	if hotkeyUsesFn(cfg) {
		freeFnKey()
	}
	// Walk the user through any missing macOS permissions with the setup guide
	// (setup.go). Setup also kicks off the model download / warm-up once the
	// permissions are settled, so the model is ready by the first dictation.
	go runSetup(cfg, m)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	enabled := true
	var rec *recorder

	save := func() {
		if err := saveConfig(configPath(), cfg); err != nil {
			logErr(err)
		}
	}
	// startRec begins a recording (no-op if one is already running).
	startRec := func() {
		if rec != nil {
			return
		}
		r, err := startRecording(resolveMicArg(cfg.Mic, m.micDevs), overlaySetBands)
		if err != nil {
			logErr(err)
			return
		}
		rec = r
		iconSetRecording()
		overlayShow()
		if cfg.Sounds {
			playSound("Pop")
		}
		if cfg.Mode == "toggle" {
			logInfo("recording… (tap the hotkey to stop, Esc to abort)")
		} else {
			logInfo("recording… (release to transcribe, Esc to abort)")
		}
	}
	// finishRec stops a recording and sends it off to be transcribed.
	finishRec := func() {
		if rec == nil {
			return
		}
		r := rec
		rec = nil
		iconSetIdle()
		overlayHide()
		path, dur, _ := r.Stop()
		if cfg.Sounds {
			playSound("Bottle")
		}
		if dur < 400*time.Millisecond {
			logInfo("too short — ignored")
			_ = os.Remove(path)
			return
		}
		go process(cfg, path, dur, m)
	}
	// discard ends an in-flight recording and throws the audio away.
	discard := func() {
		if rec == nil {
			return
		}
		r := rec
		rec = nil
		iconSetIdle()
		overlayHide()
		p, _, _ := r.Stop()
		_ = os.Remove(p)
	}
	setEnabled := func(v bool) {
		enabled = v
		m.setDictation(v)
		if v {
			logInfo("dictation ENABLED")
		} else {
			discard()
			logInfo("dictation DISABLED")
		}
		if cfg.Sounds {
			playSound("Glass")
		}
	}

	m.setMode(cfg.Mode)
	m.setModel(cfg)
	m.refreshHistory()
	m.setMics(audioDevices(), cfg) // detect input devices and resolve the mic
	logInfo("ready — " + hotkeyHint(cfg))

	for {
		select {
		case <-recDownCh:
			if !enabled {
				continue
			}
			if cfg.Mode == "toggle" {
				if rec == nil {
					startRec()
				} else {
					finishRec()
				}
			} else {
				startRec()
			}

		case <-recUpCh:
			if cfg.Mode == "toggle" {
				continue // releasing the key is meaningless in toggle mode
			}
			finishRec()

		case <-abortKeyCh:
			if rec == nil {
				continue // Esc only matters while recording
			}
			discard()
			if cfg.Sounds {
				playSound("Funk")
			}
			logInfo("recording aborted")

		case <-toggleCh:
			setEnabled(!enabled)

		case <-m.dictation:
			setEnabled(!enabled)

		case <-m.modeHold:
			cfg.Mode = "hold"
			m.setMode(cfg.Mode)
			save()
			logInfo("recording mode → hold to talk")

		case <-m.modeToggle:
			cfg.Mode = "toggle"
			m.setMode(cfg.Mode)
			save()
			logInfo("recording mode → toggle on/off")

		case idx := <-m.model:
			if idx >= 0 && idx < len(catalog) {
				applyModel(&cfg, catalog[idx].name)
				save()
				m.setModel(cfg)
				if cfg.Engine == "parakeet" {
					go warmParakeet(cfg.ParakeetModel)
				}
				logInfo("model → " + cfg.Engine + " / " + activeModel(cfg))
			}

		case <-m.autopaste:
			cfg.Paste = !cfg.Paste
			m.setAutopaste(cfg.Paste)
			save()
			logInfo(fmt.Sprintf("auto-paste → %v", cfg.Paste))

		case <-m.sounds:
			cfg.Sounds = !cfg.Sounds
			m.setSounds(cfg.Sounds)
			save()
			logInfo(fmt.Sprintf("sound feedback → %v", cfg.Sounds))

		case <-m.hotkey:
			logInfo("change-hotkey: press your new shortcut")
			startHotkeyCapture()

		case combo := <-hotkeyChangeCh:
			cfg.Hotkey = combo
			applyHotkeys(cfg)
			if hotkeyUsesFn(cfg) {
				freeFnKey()
			}
			save()
			logInfo("hotkey changed → " + combo)

		case idx := <-m.history:
			if idx >= 0 && idx < len(m.historyTexts) {
				if t := m.historyTexts[idx]; t != "" {
					if err := setClipboard(t); err != nil {
						logErr(err)
					} else {
						if cfg.Sounds {
							playSound("Pop")
						}
						logInfo("copied from history: " + preview(t))
					}
				}
			}

		case <-historyRefreshCh:
			m.refreshHistory()

		case <-m.historyClear:
			go func() { // confirm dialog blocks — keep it off the engine loop
				if !confirmClearHistory() {
					return
				}
				if err := clearHistory(); err != nil {
					logErr(err)
					return
				}
				logInfo("history cleared")
				select {
				case historyRefreshCh <- struct{}{}:
				default:
				}
			}()

		case idx := <-m.mic:
			if idx >= 0 && idx < len(m.micDevs) {
				cfg.Mic = m.micDevs[idx].name
				m.setMicChecks(cfg)
				save()
				logInfo("microphone → " + m.micDevs[idx].name)
			}

		case <-m.restart:
			logInfo("restarting wspr to apply the new permission")
			relaunch()

		case <-m.quit:
			logInfo("quitting")
			discard()
			stopParakeet()
			menuQuitApp()
			return

		case <-sigCh:
			fmt.Println()
			logInfo("quitting")
			discard()
			stopParakeet()
			menuQuitApp()
			return
		}
	}
}

// relaunch restarts wspr. A newly granted Input Monitoring permission is
// visible only to a fresh process, so wspr must relaunch itself once the user
// has enabled it. relaunch does not return on success.
func relaunch() {
	exe, err := os.Executable()
	if err != nil {
		logErr(fmt.Errorf("relaunch: %w", err))
		return
	}
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		// Inside a .app bundle — reopen it. -n starts a fresh instance, so
		// there is no race with this process shutting down.
		_ = exec.Command("open", "-n", exe[:i+len(".app")]).Start()
	} else {
		// Bare binary — spawn a fresh copy with the same arguments.
		_ = exec.Command(exe, os.Args[1:]...).Start()
	}
	os.Exit(0)
}

// openSecurityPane opens a pane of System Settings → Privacy & Security. The
// anchor is e.g. "Privacy_ListenEvent" (Input Monitoring) or
// "Privacy_Accessibility".
func openSecurityPane(anchor string) {
	_ = exec.Command("open",
		"x-apple.systempreferences:com.apple.preference.security?"+anchor).Start()
}

// applyModel points the config at the chosen catalog model, switching engine
// as needed.
func applyModel(cfg *Config, name string) {
	for _, mi := range catalog {
		if mi.name == name {
			cfg.Engine = mi.engine
			if mi.engine == "whisper" {
				cfg.WhisperModel = name
			} else {
				cfg.ParakeetModel = name
			}
			return
		}
	}
}

// process transcribes a finished recording and pastes the result. It runs in
// its own goroutine so the hotkey loop stays responsive.
func process(cfg Config, path string, dur time.Duration, m *menu) {
	defer recoverLog("process")
	defer os.Remove(path)

	if info, err := os.Stat(path); err != nil || info.Size() < 1024 {
		logErr(fmt.Errorf("no audio captured — check microphone permission for your terminal"))
		return
	}

	logInfo(fmt.Sprintf("transcribing %.1fs with %s/%s…", dur.Seconds(), cfg.Engine, activeModel(cfg)))
	t0 := time.Now()
	text, err := transcribe(cfg, path)
	if err != nil {
		logErr(err)
		return
	}
	if text == "" {
		logInfo("empty transcription — nothing to paste")
		return
	}
	appendHistory(historyEntry{
		Time:          time.Now().Format(time.RFC3339),
		Engine:        cfg.Engine,
		Model:         activeModel(cfg),
		AudioSec:      dur.Seconds(),
		TranscribeSec: time.Since(t0).Seconds(),
		Text:          text,
	})
	select { // ask the engine loop to refresh the History submenu
	case historyRefreshCh <- struct{}{}:
	default:
	}
	switch {
	case cfg.Paste && accessibilityTrusted():
		// Smart spacing, like Whisper Flow: if the caret sits right after a
		// non-whitespace character, prepend a space so the dictated text does
		// not run into the previous word.
		out := text
		if cursorNeedsLeadingSpace() {
			out = " " + text
		}
		// Cmd+V can only read the one shared system clipboard, so auto-paste
		// has to borrow it: snapshot the user's clipboard, paste the
		// transcription, then restore the snapshot so their copied content is
		// left exactly as it was.
		snap := clipboardSnapshot()
		if err := setClipboard(out); err != nil {
			clipboardRestore(snap)
			logErr(fmt.Errorf("clipboard: %w", err))
			return
		}
		time.Sleep(60 * time.Millisecond) // let the clipboard change register
		pasteToFrontApp()
		time.Sleep(150 * time.Millisecond) // let the app read the paste first
		clipboardRestore(snap)
		logInfo(fmt.Sprintf("pasted in %.1fs: %q", time.Since(t0).Seconds(), preview(text)))
	case cfg.Paste:
		// Auto-paste needs Accessibility to synthesize Cmd+V; without it, leave
		// the transcription on the clipboard so it isn't lost.
		if err := setClipboard(text); err != nil {
			logErr(fmt.Errorf("clipboard: %w", err))
			return
		}
		logInfo("transcription copied — grant Accessibility to auto-paste")
	default:
		// Clipboard-only mode (--no-paste): the user asked for the text on the
		// clipboard rather than pasted into the app.
		if err := setClipboard(text); err != nil {
			logErr(fmt.Errorf("clipboard: %w", err))
			return
		}
		logInfo(fmt.Sprintf("copied to clipboard in %.1fs: %q", time.Since(t0).Seconds(), preview(text)))
	}
}
