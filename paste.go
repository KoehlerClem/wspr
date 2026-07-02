package main

/*
#include <stdlib.h>
void clipboardWriteString(const char *utf8);
*/
import "C"

import (
	"os/exec"
	"unsafe"
)

// setClipboard copies text to the macOS clipboard via NSPasteboard.
//
// We deliberately do not shell out to pbcopy: when wspr is launched as a .app
// from Finder it inherits no LANG, and pbcopy then treats stdin as Latin-1
// rather than UTF-8 — the two UTF-8 bytes for "ü" (C3 BC) get reinterpreted
// as the chars "Ã"/"¼" and re-encoded, landing on the clipboard as the
// unrelated symbols "√º". Going straight through NSPasteboard avoids that.
func setClipboard(text string) error {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.clipboardWriteString(cs)
	return nil
}

// pasteToFrontApp sends Cmd+V to the focused app by synthesizing the keystroke
// directly (see pasteCmdV). Requires Accessibility permission.
//
// Cmd+V has no source other than the shared system clipboard, so auto-paste
// has to route the transcription through it; process() brackets this with
// clipboardSnapshot/clipboardRestore to leave the user's clipboard untouched.
func pasteToFrontApp() {
	pasteCmdV()
}

// playSound plays a built-in macOS system sound without blocking.
func playSound(name string) {
	_ = exec.Command("afplay", "/System/Library/Sounds/"+name+".aiff").Start()
}
