package main

/*
#cgo LDFLAGS: -framework AVFoundation -framework ApplicationServices
void requestMicrophone(void);
int  microphoneStatus(void);
void requestAccessibility(void);
int  accessibilityTrusted(void);
*/
import "C"

// Microphone authorization states, mirroring AVAuthorizationStatus.
const (
	micNotDetermined = 0 // the user has not been asked yet
	micDenied        = 1 // denied or restricted — only System Settings can change it
	micAuthorized    = 2 // granted
)

// requestMicrophone shows the microphone permission prompt and blocks until the
// user has answered it. It returns immediately if the choice was already made.
func requestMicrophone() { C.requestMicrophone() }

// microphoneStatus reports the current microphone authorization without
// prompting — one of micNotDetermined, micDenied or micAuthorized.
func microphoneStatus() int { return int(C.microphoneStatus()) }

// requestAccessibility shows the Accessibility permission prompt if wspr is not
// already trusted.
func requestAccessibility() { C.requestAccessibility() }

// accessibilityTrusted reports whether wspr has Accessibility permission
// (no prompt — safe to call before every paste).
func accessibilityTrusted() bool { return C.accessibilityTrusted() != 0 }
