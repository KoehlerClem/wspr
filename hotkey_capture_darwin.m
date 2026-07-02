// hotkey_capture_darwin.m — the "Change Hotkey…" popup.
//
// A key window with a local event monitor: it shows the keys as they are
// pressed and locks in the combination the moment the first key is released —
// so any single key, bare modifier (fn), or combo can be captured.
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

#include "hotkey_mods.h" // shared modifier bitmask + side tracking
#define KEY_ESCAPE 53

// WSPRKeyPanel is a borderless panel that can still become the key window, so
// the local event monitor receives the keystrokes the user presses.
@interface WSPRKeyPanel : NSPanel
@end
@implementation WSPRKeyPanel
- (BOOL)canBecomeKeyWindow { return YES; }
@end

static NSString *gCapText = nil;  // live preview, or the locked-in combo
static BOOL      gCapLocked = NO; // YES once a combo has been locked in

@interface WSPRCaptureView : NSView
@end
@implementation WSPRCaptureView
- (void)drawRect:(NSRect)dirty {
    (void)dirty;
    NSRect b = [self bounds];
    NSBezierPath *bg = [NSBezierPath bezierPathWithRoundedRect:b xRadius:16.0 yRadius:16.0];
    [[NSColor colorWithCalibratedWhite:0.12 alpha:0.98] setFill];
    [bg fill];

    NSMutableParagraphStyle *ps = [[[NSMutableParagraphStyle alloc] init] autorelease];
    [ps setAlignment:NSTextAlignmentCenter];

    NSString *headline = gCapLocked ? @"Shortcut set" : @"Press your new shortcut";
    NSColor  *valColor = gCapLocked
        ? [NSColor colorWithCalibratedRed:0.40 green:0.85 blue:0.47 alpha:1.0]
        : [NSColor whiteColor];

    NSDictionary *capStyle = @{
        NSFontAttributeName : [NSFont systemFontOfSize:12.0],
        NSForegroundColorAttributeName : [NSColor colorWithCalibratedWhite:0.62 alpha:1.0],
        NSParagraphStyleAttributeName : ps
    };
    [headline drawInRect:NSMakeRect(0, b.size.height - 40, b.size.width, 18)
          withAttributes:capStyle];

    NSString *val = (gCapText != nil) ? gCapText : @"…";
    NSDictionary *valStyle = @{
        NSFontAttributeName : [NSFont systemFontOfSize:26.0 weight:NSFontWeightBold],
        NSForegroundColorAttributeName : valColor,
        NSParagraphStyleAttributeName : ps
    };
    [val drawInRect:NSMakeRect(0, b.size.height / 2.0 - 22.0, b.size.width, 40.0)
     withAttributes:valStyle];

    if (!gCapLocked) {
        NSDictionary *hint = @{
            NSFontAttributeName : [NSFont systemFontOfSize:11.0],
            NSForegroundColorAttributeName : [NSColor colorWithCalibratedWhite:0.5 alpha:1.0],
            NSParagraphStyleAttributeName : ps
        };
        [@"release a key to lock it in   ·   Esc to cancel"
               drawInRect:NSMakeRect(0, 16, b.size.width, 16)
           withAttributes:hint];
    }
}
@end

static NSPanel *gCapPanel = nil;
static id gCapMonitor = nil;

// Capture state: the keys held now, and the peak combination seen so far.
static int gCurMods = 0, gCurKey = -1;
static int gPeakMods = 0, gPeakKey = -1;
static int gSideMods = 0; // which left/right modifier keys are physically down

static void removeCaptureMonitor(void) {
    if (gCapMonitor != nil) {
        [NSEvent removeMonitor:gCapMonitor];
        [gCapMonitor release];
        gCapMonitor = nil;
    }
}

// lockCapture fires the moment the first key is released: the peak combination
// is what gets locked in.
static void lockCapture(void) {
    if (gCapMonitor == nil) return;             // already locked or closed
    if (gPeakMods == 0 && gPeakKey < 0) return; // nothing was pressed
    removeCaptureMonitor();
    onHotkeyCaptured(gPeakKey, gPeakMods); // Go validates, shows it, then closes
}

static void ensureCapturePanel(void) {
    if (gCapPanel != nil) return;
    NSRect frame = NSMakeRect(0, 0, 420, 132);
    gCapPanel = [[WSPRKeyPanel alloc] initWithContentRect:frame
                                               styleMask:NSWindowStyleMaskBorderless
                                                 backing:NSBackingStoreBuffered
                                                   defer:NO];
    [gCapPanel setOpaque:NO];
    [gCapPanel setBackgroundColor:[NSColor clearColor]];
    [gCapPanel setLevel:NSStatusWindowLevel];
    [gCapPanel setHasShadow:NO];
    [gCapPanel setHidesOnDeactivate:NO];
    [gCapPanel setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces |
                                      NSWindowCollectionBehaviorStationary |
                                      NSWindowCollectionBehaviorIgnoresCycle)];
    [gCapPanel setContentView:[[[WSPRCaptureView alloc] initWithFrame:frame] autorelease]];
}

void hotkeyCaptureShow(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        ensureCapturePanel();
        [gCapText release];
        gCapText = nil;
        gCapLocked = NO;
        gCurMods = gPeakMods = 0;
        gCurKey = gPeakKey = -1;
        gSideMods = 0;
        [[gCapPanel contentView] setNeedsDisplay:YES];
        [gCapPanel center];
        [NSApp activateIgnoringOtherApps:YES];
        [gCapPanel makeKeyAndOrderFront:nil];

        removeCaptureMonitor();
        gCapMonitor = [[NSEvent addLocalMonitorForEventsMatchingMask:
            (NSEventMaskKeyDown | NSEventMaskKeyUp | NSEventMaskFlagsChanged)
            handler:^NSEvent *(NSEvent *e) {
                NSEventType t = [e type];
                if (t == NSEventTypeFlagsChanged) {
                    NSEventModifierFlags f = [e modifierFlags];
                    int side = wsprSideBitForKeyCode((int)[e keyCode]);
                    if (side) gSideMods ^= side;
                    gSideMods = wsprReconcileSides(gSideMods, f);
                    int nm = wsprNsMods(f) | gSideMods;
                    if (nm & ~gCurMods) {        // a modifier went down
                        gCurMods = nm;
                        gPeakMods |= nm;
                        onHotkeyCaptureLive(gCurKey, gCurMods);
                    } else if (nm != gCurMods) { // a modifier went up — release
                        gCurMods = nm;
                        lockCapture();
                    }
                } else if (t == NSEventTypeKeyDown) {
                    int kc = (int)[e keyCode];
                    if (kc == KEY_ESCAPE && gCurMods == 0) {
                        onHotkeyCaptureCancel();
                    } else {
                        gCurKey = kc;
                        gPeakKey = kc;
                        gPeakMods |= gCurMods;
                        onHotkeyCaptureLive(gCurKey, gCurMods);
                    }
                } else if (t == NSEventTypeKeyUp) {
                    if ((int)[e keyCode] == gCurKey) lockCapture();
                }
                return nil; // swallow — the captured keys must not act
            }] retain];
    });
}

void hotkeyCaptureSetText(const char *s, int locked) {
    char *copy = strdup(s);
    dispatch_async(dispatch_get_main_queue(), ^{
        [gCapText release];
        gCapText = [[NSString alloc] initWithUTF8String:copy];
        free(copy);
        gCapLocked = locked ? YES : NO;
        if (gCapPanel != nil) [[gCapPanel contentView] setNeedsDisplay:YES];
    });
}

void hotkeyCaptureClose(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        removeCaptureMonitor();
        if (gCapPanel != nil) [gCapPanel orderOut:nil];
    });
}
