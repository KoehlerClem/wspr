package main

/*
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework ApplicationServices
#include <stdlib.h>
void inputStart(void);
void inputSetRecordTrigger(int keyCode, int mods);
void inputSetToggleTrigger(int keyCode, int mods);
void pasteCmdV(void);
void *clipboardSave(void);
void clipboardRestore(void *handle);
void clipboardWriteString(const char *utf8);
int  cursorNeedsLeadingSpace(void);
void hotkeyCaptureShow(void);
void hotkeyCaptureClose(void);
void hotkeyCaptureSetText(const char *s, int locked);
*/
import "C"

import (
	"strings"
	"time"
	"unsafe"
)

// Channels fed by the global key monitor (see input_darwin.m).
var (
	recDownCh      = make(chan struct{}, 4)
	recUpCh        = make(chan struct{}, 4)
	toggleCh       = make(chan struct{}, 4)
	abortKeyCh     = make(chan struct{}, 4)
	hotkeyChangeCh = make(chan string, 1)
)

// inputStart installs the global keyboard monitor. Watching keystrokes this way
// needs the Accessibility permission; until it is granted the monitor is silent
// — calling inputStart again (e.g. after setup) re-installs it.
func inputStart() { C.inputStart() }

// inputSetRecordTrigger / inputSetToggleTrigger set the hotkey the monitor
// watches for. keyCode is a macOS virtual key code, or -1 for a modifier-only
// hotkey such as fn.
func inputSetRecordTrigger(keyCode, mods int) {
	C.inputSetRecordTrigger(C.int(keyCode), C.int(mods))
}

func inputSetToggleTrigger(keyCode, mods int) {
	C.inputSetToggleTrigger(C.int(keyCode), C.int(mods))
}

// pasteCmdV synthesizes a Cmd+V keystroke for the focused app.
func pasteCmdV() { C.pasteCmdV() }

// clipboardSnapshot captures the current clipboard so it can be restored after
// an auto-paste. The returned handle must be passed to clipboardRestore once.
func clipboardSnapshot() unsafe.Pointer { return C.clipboardSave() }

// clipboardRestore puts a clipboardSnapshot back and frees the handle.
func clipboardRestore(h unsafe.Pointer) { C.clipboardRestore(h) }

// cursorNeedsLeadingSpace reports whether the character before the focused text
// field's insertion point is non-whitespace, so a pasted transcription should
// be prefixed with a space. It is false when the answer can't be determined.
func cursorNeedsLeadingSpace() bool { return C.cursorNeedsLeadingSpace() != 0 }

// startHotkeyCapture shows the capture popup, which records the next combo.
func startHotkeyCapture() { C.hotkeyCaptureShow() }

// endHotkeyCapture hides the capture popup.
func endHotkeyCapture() { C.hotkeyCaptureClose() }

// hotkeyCaptureSetText updates the text shown in the capture popup — a live
// preview while keys are held, or the locked-in combo.
func hotkeyCaptureSetText(s string, locked bool) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	C.hotkeyCaptureSetText(cs, cbool(locked))
}

//export onHotkeyDown
func onHotkeyDown() { sendSignal(recDownCh) }

//export onHotkeyUp
func onHotkeyUp() { sendSignal(recUpCh) }

//export onToggleKey
func onToggleKey() { sendSignal(toggleCh) }

//export onAbortKey
func onAbortKey() { sendSignal(abortKeyCh) }

func sendSignal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// onHotkeyCaptureLive is called from the capture popup as keys are pressed, so
// it can show the in-progress combination.
//
//export onHotkeyCaptureLive
func onHotkeyCaptureLive(keyCode, mods C.int) {
	hotkeyCaptureSetText(captureDisplay(int(keyCode), int(mods)), false)
}

// onHotkeyCaptured is called from the capture popup once a combination is
// locked in: a macOS key code (or -1 for modifier-only) and a wspr modifier
// bitmask.
//
//export onHotkeyCaptured
func onHotkeyCaptured(keyCode, mods C.int) {
	combo := comboString(int(keyCode), int(mods))
	if combo == "" {
		logInfo("that key can't be used as a hotkey — try Change Hotkey again")
		C.hotkeyCaptureClose()
		return
	}
	if _, err := parseHotkey(combo); err != nil {
		logErr(err)
		C.hotkeyCaptureClose()
		return
	}
	hotkeyCaptureSetText(combo, true)
	select {
	case hotkeyChangeCh <- combo:
	default:
	}
	go func() { // leave the locked combo on screen briefly, then close
		time.Sleep(900 * time.Millisecond)
		C.hotkeyCaptureClose()
	}()
}

//export onHotkeyCaptureCancel
func onHotkeyCaptureCancel() {
	C.hotkeyCaptureClose()
	logInfo("hotkey change cancelled")
}

// comboString turns a key code + wspr modifier bitmask into a hotkey string
// such as "ctrl+option+space", "fn", or "f5".
func comboString(keyCode, mods int) string {
	var parts []string
	addMod := func(left, right, generic int, lname, rname, gname string) {
		switch {
		case mods&left != 0:
			parts = append(parts, lname)
		case mods&right != 0:
			parts = append(parts, rname)
		case mods&generic != 0:
			parts = append(parts, gname)
		}
	}
	addMod(modCtrlLeft, modCtrlRight, modCtrl, "lctrl", "rctrl", "ctrl")
	addMod(modOptionLeft, modOptionRight, modOption, "lopt", "ropt", "option")
	addMod(modShiftLeft, modShiftRight, modShift, "lshift", "rshift", "shift")
	if mods&modFn != 0 {
		parts = append(parts, "fn")
	}
	addMod(modCmdLeft, modCmdRight, modCmd, "lcmd", "rcmd", "cmd")
	if keyCode >= 0 {
		name := keyName(keyCode)
		if name == "" {
			return ""
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "+")
}

// captureDisplay renders an in-progress capture with key symbols, e.g.
// "⌃ ⌥ Space" or "🌐".
func captureDisplay(keyCode, mods int) string {
	var parts []string
	addSym := func(left, right, generic int, sym string) {
		switch {
		case mods&left != 0:
			parts = append(parts, sym+"L")
		case mods&right != 0:
			parts = append(parts, sym+"R")
		case mods&generic != 0:
			parts = append(parts, sym)
		}
	}
	addSym(modCtrlLeft, modCtrlRight, modCtrl, "⌃")
	addSym(modOptionLeft, modOptionRight, modOption, "⌥")
	addSym(modShiftLeft, modShiftRight, modShift, "⇧")
	addSym(modCmdLeft, modCmdRight, modCmd, "⌘")
	if mods&modFn != 0 {
		parts = append(parts, "🌐")
	}
	if keyCode >= 0 {
		if name := keyName(keyCode); name != "" {
			parts = append(parts, capitalize(name))
		}
	}
	if len(parts) == 0 {
		return "…"
	}
	return strings.Join(parts, " ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// keyName reverse-maps a macOS virtual key code to a wspr key name.
func keyName(code int) string {
	for name, k := range keyMap {
		if k == code {
			return name
		}
	}
	return ""
}
