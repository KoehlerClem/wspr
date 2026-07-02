// input_darwin.m — the global push-to-talk hotkey, built on an NSEvent global
// monitor. Watching keystrokes this way needs the Accessibility permission
// (which wspr also uses for auto-paste) — but not Input Monitoring — and it
// detects anything: a combo, a single key, or a bare modifier such as fn.
//
// This file also drives auto-paste: it synthesizes the Cmd+V keystroke and
// snapshots/restores the clipboard around it, so the user's copied content is
// left untouched.
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#include "_cgo_export.h"

#include "hotkey_mods.h" // shared modifier bitmask + side tracking
#define KEY_ESCAPE 53

typedef struct {
    int keyCode; // -1 = modifier-only
    int mods;
    int active;  // currently held
} Trigger;

static Trigger gRecord = {-1, 0, 0};
static Trigger gToggle = {-1, 0, 0};
static int gCurMods    = 0; // modifiers held now (tracked from flagsChanged)
static int gSideMods   = 0; // which left/right modifier keys are physically down
static int gRecKeyDown = 0;
static int gTogKeyDown = 0;
static id  gMonitor    = nil;

// triggerSet reports whether a trigger has actually been configured.
static int triggerSet(Trigger *t) { return t->keyCode >= 0 || t->mods != 0; }

// triggerHeld reports whether a trigger is fully pressed right now. The generic
// modifier set must match exactly; any side bit the trigger asks for (left or
// right command) must also be among the modifiers held — so "cmd" matches
// either side while "lcmd"/"rcmd" pin it to one.
static int triggerHeld(Trigger *t, int keyDown) {
    if (!triggerSet(t)) return 0;
    if ((gCurMods & MOD_GENERIC_MASK) != (t->mods & MOD_GENERIC_MASK)) return 0;
    if (t->mods & MOD_SIDE_MASK & ~gCurMods) return 0;
    if (t->keyCode < 0) return 1;  // modifier-only
    return keyDown;                // key + optional modifiers
}

static void evalTriggers(void) {
    int rh = triggerHeld(&gRecord, gRecKeyDown);
    if (rh && !gRecord.active) {
        gRecord.active = 1;
        onHotkeyDown();
    } else if (!rh && gRecord.active) {
        gRecord.active = 0;
        onHotkeyUp();
    }
    int th = triggerHeld(&gToggle, gTogKeyDown);
    if (th && !gToggle.active) {
        gToggle.active = 1;
        onToggleKey();
    } else if (!th) {
        gToggle.active = 0;
    }
}

// processEvent feeds one keyboard event into the trigger state machine. The
// modifier set is tracked only from flagsChanged events: the fn flag rides
// along on arrow/function-key events too, so reading it from keyDown would
// make those keys look like an fn press.
static void processEvent(NSEvent *e) {
    NSEventType type = [e type];
    if (type == NSEventTypeFlagsChanged) {
        NSEventModifierFlags f = [e modifierFlags];
        // Each modifier-key press and release emits one flagsChanged carrying
        // that key's keyCode, so toggling its side bit tracks which physical
        // key is down; reconcile then drops any side whose modifier let go.
        int side = wsprSideBitForKeyCode((int)[e keyCode]);
        if (side) gSideMods ^= side;
        gSideMods = wsprReconcileSides(gSideMods, f);
        gCurMods = wsprNsMods(f) | gSideMods;
    } else if (type == NSEventTypeKeyDown) {
        int kc = (int)[e keyCode];
        if (gRecord.keyCode >= 0 && kc == gRecord.keyCode) gRecKeyDown = 1;
        if (gToggle.keyCode >= 0 && kc == gToggle.keyCode) gTogKeyDown = 1;
        if (kc == KEY_ESCAPE && gCurMods == 0) onAbortKey();
    } else if (type == NSEventTypeKeyUp) {
        int kc = (int)[e keyCode];
        if (gRecord.keyCode >= 0 && kc == gRecord.keyCode) gRecKeyDown = 0;
        if (gToggle.keyCode >= 0 && kc == gToggle.keyCode) gTogKeyDown = 0;
    }
    evalTriggers();
}

// inputStart installs the global keyboard monitor, replacing any earlier one.
// The monitor only delivers key events once wspr is trusted for Accessibility,
// so it is re-installed after setup once the permission has been granted.
void inputStart(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gMonitor != nil) {
            [NSEvent removeMonitor:gMonitor];
            [gMonitor release];
            gMonitor = nil;
        }
        NSEventMask mask = NSEventMaskKeyDown | NSEventMaskKeyUp |
                           NSEventMaskFlagsChanged;
        gMonitor = [[NSEvent addGlobalMonitorForEventsMatchingMask:mask
            handler:^(NSEvent *e) { processEvent(e); }] retain];
    });
}

void inputSetRecordTrigger(int keyCode, int mods) {
    dispatch_async(dispatch_get_main_queue(), ^{
        gRecord.keyCode = keyCode;
        gRecord.mods = mods;
        gRecord.active = 0;
        gRecKeyDown = 0;
    });
}

void inputSetToggleTrigger(int keyCode, int mods) {
    dispatch_async(dispatch_get_main_queue(), ^{
        gToggle.keyCode = keyCode;
        gToggle.mods = mods;
        gToggle.active = 0;
        gTogKeyDown = 0;
    });
}

// pasteCmdV synthesizes a Cmd+V keystroke for the focused app. It needs the
// Accessibility permission, but — unlike scripting System Events — it does not
// need the Automation permission.
void pasteCmdV(void) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
    CGEventRef down = CGEventCreateKeyboardEvent(src, (CGKeyCode)9, true);  // 'v'
    CGEventRef up   = CGEventCreateKeyboardEvent(src, (CGKeyCode)9, false);
    CGEventSetFlags(down, kCGEventFlagMaskCommand);
    CGEventSetFlags(up, kCGEventFlagMaskCommand);
    CGEventPost(kCGHIDEventTap, down);
    CGEventPost(kCGHIDEventTap, up);
    if (down) CFRelease(down);
    if (up) CFRelease(up);
    if (src) CFRelease(src);
}

// clipboardSave captures the current contents of the general pasteboard — every
// item with all of its data representations (text, images, files, …) — and
// returns it as an opaque handle. Pass the handle to clipboardRestore exactly
// once; that call frees it. wspr uses this to put the user's clipboard back
// after an auto-paste, since Cmd+V can only ever read this one shared
// pasteboard. Promised (lazily-provided) data cannot be captured and is the one
// thing a restore will not bring back.
void *clipboardSave(void) {
    NSMutableArray *saved = [[NSMutableArray alloc] init];
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        for (NSPasteboardItem *item in [pb pasteboardItems]) {
            NSPasteboardItem *copy = [[NSPasteboardItem alloc] init];
            for (NSString *type in [item types]) {
                NSData *d = [item dataForType:type];
                if (d) [copy setData:d forType:type];
            }
            [saved addObject:copy];
            [copy release];
        }
    }
    return (void *)saved;
}

// clipboardWriteString puts a UTF-8 string on the general pasteboard as
// proper Unicode. Going through NSPasteboard avoids the pbcopy/LANG trap:
// a .app launched from Finder inherits no LANG, and pbcopy then treats its
// input as Latin-1 — turning the two UTF-8 bytes for "ü" into "√º" on the
// clipboard.
void clipboardWriteString(const char *utf8) {
    if (utf8 == NULL) return;
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:utf8];
        if (s == nil) return;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        [pb setString:s forType:NSPasteboardTypeString];
    }
}

// clipboardRestore writes a clipboardSave snapshot back to the general
// pasteboard and frees the handle.
void clipboardRestore(void *handle) {
    if (handle == NULL) return;
    NSMutableArray *saved = (NSMutableArray *)handle;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        if ([saved count] > 0) [pb writeObjects:saved];
    }
    [saved release];
}

// cursorNeedsLeadingSpace inspects the focused text field through the
// Accessibility API and reports whether the character right before the
// insertion point is non-whitespace — i.e. a transcription pasted there should
// be given a leading space so it does not run into the previous word. It
// returns 0 whenever the answer cannot be determined (no focused field, caret
// at the very start, or an app that does not expose its text), so the caller
// safely falls back to pasting the text unchanged.
int cursorNeedsLeadingSpace(void) {
    int result = 0;
    @autoreleasepool {
        AXUIElementRef sys = AXUIElementCreateSystemWide();
        AXUIElementRef focused = NULL;
        AXError e = AXUIElementCopyAttributeValue(
            sys, kAXFocusedUIElementAttribute, (CFTypeRef *)&focused);
        CFRelease(sys);
        if (e != kAXErrorSuccess || focused == NULL) return 0;

        AXUIElementSetMessagingTimeout(focused, 0.5f); // don't stall the paste

        // Locate the insertion point — the start of any selection.
        CFTypeRef rangeVal = NULL;
        CFRange sel = {0, 0};
        if (AXUIElementCopyAttributeValue(focused, kAXSelectedTextRangeAttribute,
                &rangeVal) != kAXErrorSuccess || rangeVal == NULL) {
            CFRelease(focused);
            return 0;
        }
        Boolean gotRange = AXValueGetValue((AXValueRef)rangeVal,
                                           kAXValueTypeCFRange, &sel);
        CFRelease(rangeVal);
        if (!gotRange || sel.location <= 0) { // unknown, or at the start
            CFRelease(focused);
            return 0;
        }

        // Read the one character before the insertion point.
        CFRange before = CFRangeMake(sel.location - 1, 1);
        AXValueRef beforeVal = AXValueCreate(kAXValueTypeCFRange, &before);
        CFTypeRef strVal = NULL;
        if (AXUIElementCopyParameterizedAttributeValue(focused,
                kAXStringForRangeParameterizedAttribute, beforeVal,
                &strVal) == kAXErrorSuccess && strVal != NULL) {
            NSString *s = (NSString *)strVal;
            if ([s length] > 0) {
                NSCharacterSet *ws =
                    [NSCharacterSet whitespaceAndNewlineCharacterSet];
                result = [ws characterIsMember:[s characterAtIndex:0]] ? 0 : 1;
            }
            CFRelease(strVal);
        }
        if (beforeVal) CFRelease(beforeVal);
        CFRelease(focused);
    }
    return result;
}
