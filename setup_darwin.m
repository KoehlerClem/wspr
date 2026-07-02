// setup_darwin.m — the first-run setup guide.
//
// A single window that lists every macOS permission wspr needs, plus the
// speech model download. Each row has a button that triggers the system prompt
// (or opens the matching System Settings pane, or starts the download); once a
// row is satisfied it turns green. The Go side (setup.go) owns the logic and
// polls for progress — this file is the window.
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

#define ROWS 3

// WSPRButton accepts a click even when wspr is not the active app, so a single
// click on the guide — while System Settings still has focus — always counts.
@interface WSPRButton : NSButton
@end
@implementation WSPRButton
- (BOOL)acceptsFirstMouse:(NSEvent *)e { (void)e; return YES; }
@end

// WSPRSetupView draws the rounded card the permission rows sit on.
@interface WSPRSetupView : NSView
@end
@implementation WSPRSetupView
- (void)drawRect:(NSRect)dirty {
    [super drawRect:dirty];
    NSRect card = NSMakeRect(32, 96, 460, 270);
    NSBezierPath *p = [NSBezierPath bezierPathWithRoundedRect:card
                                                      xRadius:12.0 yRadius:12.0];
    [[NSColor controlBackgroundColor] setFill];
    [p fill];
    [[NSColor separatorColor] setFill];
    NSRectFill(NSMakeRect(52, 276, 424, 1)); // between rows 0 and 1
    NSRectFill(NSMakeRect(52, 186, 424, 1)); // between rows 1 and 2
}
@end

// WSPRSetupTarget receives the button clicks and the window-close request and
// forwards them to Go.
@interface WSPRSetupTarget : NSObject <NSWindowDelegate>
@end
@implementation WSPRSetupTarget
- (void)row:(id)s    { onSetupRow((int)[(NSButton *)s tag]); }
- (void)footer:(id)s { (void)s; onSetupFooter(); }
- (BOOL)windowShouldClose:(id)s { (void)s; onSetupClosed(); return YES; }
@end

static NSWindow        *gWin = nil;
static WSPRSetupTarget *gTarget = nil;
static NSTextField     *gIcon[ROWS], *gName[ROWS], *gDesc[ROWS], *gStatus[ROWS];
static NSButton        *gBtn[ROWS];
static NSButton        *gFooter = nil;
static NSTextField     *gCaption = nil;

// makeLabel builds a borderless, non-editable text field used as a label.
static NSTextField *makeLabel(NSRect frame, NSFont *font, NSColor *color,
                              BOOL wraps, NSTextAlignment align) {
    NSTextField *f = [[NSTextField alloc] initWithFrame:frame];
    [f setBezeled:NO];
    [f setDrawsBackground:NO];
    [f setEditable:NO];
    [f setSelectable:NO];
    [f setFont:font];
    [f setTextColor:color];
    [f setAlignment:align];
    if (wraps) {
        [f setLineBreakMode:NSLineBreakByWordWrapping];
        [[f cell] setWraps:YES];
    }
    return f;
}

// addRow builds the controls for one permission row whose bottom edge is ry.
static void addRow(NSView *c, int idx, CGFloat ry) {
    gIcon[idx] = makeLabel(NSMakeRect(52, ry + 26, 40, 40),
        [NSFont systemFontOfSize:28.0], [NSColor labelColor],
        NO, NSTextAlignmentLeft);
    gName[idx] = makeLabel(NSMakeRect(100, ry + 47, 250, 22),
        [NSFont systemFontOfSize:15.0 weight:NSFontWeightSemibold],
        [NSColor labelColor], NO, NSTextAlignmentLeft);
    gDesc[idx] = makeLabel(NSMakeRect(100, ry + 14, 248, 33),
        [NSFont systemFontOfSize:12.0], [NSColor secondaryLabelColor],
        YES, NSTextAlignmentLeft);
    gStatus[idx] = makeLabel(NSMakeRect(326, ry + 35, 150, 20),
        [NSFont systemFontOfSize:13.0 weight:NSFontWeightSemibold],
        [NSColor systemGreenColor], NO, NSTextAlignmentRight);

    gBtn[idx] = [[WSPRButton alloc] initWithFrame:NSMakeRect(352, ry + 29, 124, 32)];
    [gBtn[idx] setBezelStyle:NSBezelStyleRounded];
    [gBtn[idx] setTag:idx];
    [gBtn[idx] setTarget:gTarget];
    [gBtn[idx] setAction:@selector(row:)];

    [c addSubview:gIcon[idx]];
    [c addSubview:gName[idx]];
    [c addSubview:gDesc[idx]];
    [c addSubview:gStatus[idx]];
    [c addSubview:gBtn[idx]];
}

// buildSetupWindow lazily creates the guide window and all its controls.
static void buildSetupWindow(void) {
    if (gWin != nil) return;
    gTarget = [[WSPRSetupTarget alloc] init];

    NSRect content = NSMakeRect(0, 0, 524, 488);
    gWin = [[NSWindow alloc]
        initWithContentRect:content
                  styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable)
                    backing:NSBackingStoreBuffered
                      defer:NO];
    [gWin setTitle:@"wspr Setup"];
    [gWin setReleasedWhenClosed:NO];
    [gWin setLevel:NSFloatingWindowLevel]; // stay visible above System Settings
    [gWin setHidesOnDeactivate:NO];
    [gWin setDelegate:gTarget];

    WSPRSetupView *c = [[WSPRSetupView alloc] initWithFrame:content];
    [gWin setContentView:c];

    NSTextField *title = makeLabel(NSMakeRect(32, 442, 460, 28),
        [NSFont systemFontOfSize:21.0 weight:NSFontWeightBold],
        [NSColor labelColor], NO, NSTextAlignmentLeft);
    [title setStringValue:@"Set up wspr"];
    [c addSubview:title];

    NSTextField *sub = makeLabel(NSMakeRect(32, 388, 460, 48),
        [NSFont systemFontOfSize:13.0], [NSColor secondaryLabelColor],
        YES, NSTextAlignmentLeft);
    [sub setStringValue:@"wspr needs a couple of macOS permissions and a speech "
                         "model. It all runs on your Mac, nothing is uploaded."];
    [c addSubview:sub];

    addRow(c, 0, 276);
    addRow(c, 1, 186);
    addRow(c, 2, 96);

    gCaption = makeLabel(NSMakeRect(32, 44, 312, 16),
        [NSFont systemFontOfSize:11.0], [NSColor secondaryLabelColor],
        NO, NSTextAlignmentLeft);
    [c addSubview:gCaption];

    gFooter = [[WSPRButton alloc] initWithFrame:NSMakeRect(352, 32, 140, 32)];
    [gFooter setBezelStyle:NSBezelStyleRounded];
    [gFooter setKeyEquivalent:@"\r"]; // Return triggers the footer button
    [gFooter setTarget:gTarget];
    [gFooter setAction:@selector(footer:)];
    [c addSubview:gFooter];
}

// setupShow creates the guide window (if needed) and brings it to the front.
void setupShow(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        buildSetupWindow();
        [gWin center];
        [NSApp activateIgnoringOtherApps:YES];
        [gWin makeKeyAndOrderFront:nil];
    });
}

// setupClose hides the guide window once setup is finished.
void setupClose(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gWin != nil) [gWin orderOut:nil];
    });
}

// setupSetRow renders one permission row. An empty btnLabel means the
// permission is granted: the green status text is shown in place of the button.
void setupSetRow(int idx, const char *icon, const char *name, const char *desc,
                 const char *btnLabel, int btnEnabled, const char *statusText) {
    if (idx < 0 || idx >= ROWS) return;
    char *cIcon = strdup(icon);
    char *cName = strdup(name);
    char *cDesc = strdup(desc);
    char *cBtn  = strdup(btnLabel);
    char *cStat = strdup(statusText);
    dispatch_async(dispatch_get_main_queue(), ^{
        buildSetupWindow();
        [gIcon[idx] setStringValue:[NSString stringWithUTF8String:cIcon]];
        [gName[idx] setStringValue:[NSString stringWithUTF8String:cName]];
        [gDesc[idx] setStringValue:[NSString stringWithUTF8String:cDesc]];

        BOOL granted = (strlen(cBtn) == 0);
        [gBtn[idx] setHidden:granted];
        [gStatus[idx] setHidden:!granted];
        if (granted) {
            [gStatus[idx] setStringValue:[NSString stringWithFormat:@"✓  %@",
                [NSString stringWithUTF8String:cStat]]];
        } else {
            [gBtn[idx] setTitle:[NSString stringWithUTF8String:cBtn]];
            [gBtn[idx] setEnabled:(btnEnabled ? YES : NO)];
        }

        free(cIcon); free(cName); free(cDesc); free(cBtn); free(cStat);
    });
}

// setupSetFooter updates the footer button and the caption beside it.
void setupSetFooter(const char *label, int enabled, const char *caption) {
    char *cLabel   = strdup(label);
    char *cCaption = strdup(caption);
    dispatch_async(dispatch_get_main_queue(), ^{
        buildSetupWindow();
        [gFooter setTitle:[NSString stringWithUTF8String:cLabel]];
        [gFooter setEnabled:(enabled ? YES : NO)];
        [gCaption setStringValue:[NSString stringWithUTF8String:cCaption]];
        free(cLabel); free(cCaption);
    });
}
