package main

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void setupShow(void);
void setupClose(void);
void setupSetRow(int idx, const char *icon, const char *name, const char *desc,
                 const char *btnLabel, int btnEnabled, const char *statusText,
                 int progress);
void setupSetFooter(const char *label, int enabled, const char *caption);
*/
import "C"

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

// noProgress hides a row's download UI; -1 shows an indeterminate bar.
const noProgress = -2

// Clicks from the setup window, delivered by the C callbacks below.
var (
	setupRowCh    = make(chan int, 8)
	setupFooterCh = make(chan struct{}, 4)
	setupClosedCh = make(chan struct{}, 1)
)

//export onSetupRow
func onSetupRow(idx C.int) {
	select {
	case setupRowCh <- int(idx):
	default:
	}
}

//export onSetupFooter
func onSetupFooter() { sendSignal(setupFooterCh) }

//export onSetupClosed
func onSetupClosed() { sendSignal(setupClosedCh) }

// setupStep is one row of the setup guide — a permission, or the model
// download.
type setupStep struct {
	icon, name, desc string
	granted          func() bool   // is this step satisfied right now?
	busy             func() bool   // working in the background? (nil = never)
	request          func()        // open the system prompt / start the download
	actionLabel      func() string // label for the row's action button
	doneLabel        string        // status shown once granted ("Granted", "Downloaded")
}

// setupSteps lists the setup rows in display order: 0 = Microphone,
// 1 = Accessibility, 2 = the speech model download. Accessibility covers both
// the global key monitor (the hotkey) and the Cmd+V auto-paste — there is no
// Input Monitoring.
func setupSteps(cfg Config) []setupStep {
	return []setupStep{
		{
			icon:    "🎙️",
			name:    "Microphone",
			desc:    "Records your voice for on-device transcription.",
			granted: func() bool { return microphoneStatus() == micAuthorized },
			request: func() {
				if microphoneStatus() == micNotDetermined {
					requestMicrophone() // real Allow / Don't Allow prompt
				} else {
					openSecurityPane("Privacy_Microphone")
				}
			},
			actionLabel: func() string {
				if microphoneStatus() == micNotDetermined {
					return "Allow Access"
				}
				return "Open Settings"
			},
			doneLabel: "Granted",
		},
		{
			icon:    "📋",
			name:    "Accessibility",
			desc:    "Needed for the global hotkey and to paste into other apps.",
			granted: accessibilityTrusted,
			request: func() {
				requestAccessibility()
				openSecurityPane("Privacy_Accessibility")
			},
			actionLabel: func() string { return "Grant Access" },
			doneLabel:   "Granted",
		},
		modelSetupStep(cfg),
	}
}

// modelSetupStep is the setup row for the speech model: the configured one —
// by default NVIDIA's parakeet model, the same one wspr transcribes with.
func modelSetupStep(cfg Config) setupStep {
	short := activeModel(cfg)
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	desc := short
	for _, mi := range catalog {
		if mi.engine == cfg.Engine && mi.name == activeModel(cfg) {
			desc += " (" + mi.size + ")"
			break
		}
	}
	desc += " — downloads once, transcribes on-device."
	return setupStep{
		icon:    "🧠",
		name:    "Speech Model",
		desc:    desc,
		granted: func() bool { return modelReady(cfg) },
		busy:    func() bool { return modelDlIs(dlRunning) },
		request: func() { startModelDownload(cfg) },
		actionLabel: func() string {
			if modelDlIs(dlFailed) {
				return "Retry"
			}
			return "Download"
		},
		doneLabel: "Downloaded",
	}
}

// runSetup shows the setup guide — a single window listing the macOS
// permissions wspr needs plus the speech model download — and stays until the
// user finishes, then hands control back to the engine. It runs in its own
// goroutine and owns the menu status line until setup is done.
func runSetup(cfg Config, m *menu) {
	steps := setupSteps(cfg)
	if steps[0].granted() && steps[1].granted() {
		finishSetup(cfg, m)
		return
	}

	C.setupShow()

	poll := time.NewTicker(350 * time.Millisecond)
	defer poll.Stop()

	// draw repaints every row and the footer from the current grant state.
	// Microphone and Accessibility both flip to granted live — the only case
	// that needs a relaunch is a Microphone that was previously denied.
	autoStarted := false
	draw := func() {
		micOK := microphoneStatus() == micAuthorized
		axOK := accessibilityTrusted()
		// Once both permissions are settled, start the model download so it is
		// ready by the time the user dictates. Only once: a failed download is
		// retried by the row's button, not by every repaint.
		if micOK && axOK && !autoStarted {
			autoStarted = true
			startModelDownload(cfg)
		}
		for i, s := range steps {
			switch {
			case s.granted():
				setupRow(i, s.icon, s.name, s.desc, "", false, s.doneLabel, noProgress)
			case s.busy != nil && s.busy():
				setupRow(i, s.icon, s.name, s.desc, "", false, "", modelDlPercent())
			default:
				setupRow(i, s.icon, s.name, s.desc, s.actionLabel(), true, "", noProgress)
			}
		}
		switch {
		case micOK && axOK:
			caption := ""
			if modelDlIs(dlRunning) {
				caption = "The model keeps downloading in the background."
				if pct := modelDlPercent(); pct >= 0 {
					caption = fmt.Sprintf("The model keeps downloading in the background (%d%%).", pct)
				}
			}
			setupFooter("Done", true, caption)
		case microphoneStatus() == micDenied:
			setupFooter("Restart wspr", true,
				"Re-granting Microphone takes effect after a restart.")
		case !micOK:
			setupFooter("Done", false, "Allow Microphone access to continue.")
		default: // microphone ok, accessibility still missing
			setupFooter("Done", false, "Grant Accessibility to continue.")
		}
	}
	draw()

	for {
		select {
		case <-setupClosedCh:
			logInfo("setup guide closed before finishing")
			C.setupClose()
			finishSetup(cfg, m)
			return

		case idx := <-setupRowCh:
			if idx >= 0 && idx < len(steps) {
				steps[idx].request()
			}
			draw()

		case <-setupFooterCh:
			if microphoneStatus() == micDenied {
				logInfo("restarting wspr to apply Microphone access")
				relaunch() // does not return
			}
			C.setupClose()
			finishSetup(cfg, m)
			return

		case <-poll.C:
			draw() // pick up a grant the user made in System Settings
		}
	}
}

// finishSetup updates the menu to reflect whatever permissions are still
// missing once the guide is done. The hotkey itself needs no permission.
func finishSetup(cfg Config, m *menu) {
	startModelDownload(cfg) // make sure the model fetch / warm-up is underway
	inputStart()            // (re)install the key monitor now Accessibility is settled
	micOK := microphoneStatus() == micAuthorized
	axOK := accessibilityTrusted()
	switch {
	case micOK && axOK:
		logInfo("ready — Microphone and Accessibility granted")
	case !axOK:
		m.setRestart(true, "↻ Restart wspr  (after granting Accessibility)")
		logInfo("Accessibility not granted — the hotkey and auto-paste need it")
	default:
		logInfo("Microphone not granted — recording will not work until it is")
	}
}

// setupRow hands one permission row's content to the Cocoa window. progress
// (noProgress, -1 indeterminate, or 0..100) shows a download bar in place of
// the button; otherwise an empty btnLabel means the permission is granted and
// the row shows statusText instead.
func setupRow(idx int, icon, name, desc, btnLabel string, btnOn bool, status string, progress int) {
	ci, cn, cd := C.CString(icon), C.CString(name), C.CString(desc)
	cb, cs := C.CString(btnLabel), C.CString(status)
	C.setupSetRow(C.int(idx), ci, cn, cd, cb, cbool(btnOn), cs, C.int(progress))
	for _, p := range []*C.char{ci, cn, cd, cb, cs} {
		C.free(unsafe.Pointer(p))
	}
}

// setupFooter updates the window's footer button and the caption beside it.
func setupFooter(label string, enabled bool, caption string) {
	cl, cc := C.CString(label), C.CString(caption)
	C.setupSetFooter(cl, cbool(enabled), cc)
	C.free(unsafe.Pointer(cl))
	C.free(unsafe.Pointer(cc))
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
