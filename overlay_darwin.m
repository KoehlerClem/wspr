// Floating recording pill: a small, borderless, click-through capsule near the
// bottom of the screen. It shows a live frequency spectrum of the microphone —
// one bar per band, bass on the left, treble on the right — eased at 60fps.
#import <Cocoa/Cocoa.h>
#import <math.h>
#include <stdlib.h>

#define BARS 17 // must match numBands in spectrum.go

static void recenterPanel(void); // re-centres the pill on the active display

static void recenterPanel(void); // re-centres the pill on the active display

@interface WSPRWaveView : NSView {
    double _h[BARS];      // eased display height per bar, 0..1
    double _target[BARS]; // latest spectrum value per bar, 0..1
    NSTimer *_timer;
}
- (void)startTimer;
- (void)stopTimer;
- (void)feedBands:(const double *)bands count:(int)n;
@end

@implementation WSPRWaveView

- (void)startTimer {
    if (_timer != nil) return;
    for (int i = 0; i < BARS; i++) { _h[i] = 0.0; _target[i] = 0.0; }
    _timer = [NSTimer scheduledTimerWithTimeInterval:(1.0 / 60.0)
                                              target:self
                                            selector:@selector(tick)
                                            userInfo:nil
                                             repeats:YES];
}

- (void)stopTimer {
    [_timer invalidate];
    _timer = nil;
}

- (void)tick {
    for (int i = 0; i < BARS; i++) {
        double t = _target[i];
        // Fast attack, gentler release — lively but smooth.
        double k = (t > _h[i]) ? 0.5 : 0.22;
        _h[i] += (t - _h[i]) * k;
    }
    // Follow the active display: while recording the user may switch to an app
    // on another monitor (or into a fullscreen Space on one). A window can only
    // be visible on the display it is physically positioned on, so we re-centre
    // each tick. recenterPanel only moves the window when the target actually
    // changes, so this is a cheap no-op while the user stays on one display.
    recenterPanel();
    [self setNeedsDisplay:YES];
}

- (void)feedBands:(const double *)bands count:(int)n {
    if (n > BARS) n = BARS;
    for (int i = 0; i < n; i++) {
        double v = bands[i];
        if (v < 0.0) v = 0.0;
        if (v > 1.0) v = 1.0;
        _target[i] = v;
    }
}

- (void)drawRect:(NSRect)dirty {
    (void)dirty;
    NSRect b = [self bounds];
    CGFloat cy = b.size.height / 2.0;

    // The pill body — a fully rounded capsule.
    NSBezierPath *bg = [NSBezierPath bezierPathWithRoundedRect:b
                                                      xRadius:cy yRadius:cy];
    [[NSColor colorWithCalibratedWhite:0.11 alpha:0.96] setFill];
    [bg fill];

    // One bar per frequency band, mirrored about the centre line.
    CGFloat padX = 10.0, barW = 1.8;
    CGFloat gap = (b.size.width - 2.0 * padX - BARS * barW) / (BARS - 1);
    CGFloat maxH = b.size.height - 11.0;

    [[NSColor colorWithCalibratedRed:0.34 green:0.88 blue:0.52 alpha:1.0] setFill];
    for (int i = 0; i < BARS; i++) {
        double v = _h[i];
        if (v > 1.0) v = 1.0;
        // A gentle curve keeps quieter bands visible.
        CGFloat h = (CGFloat)(pow(v, 0.7) * maxH);
        if (h < 2.2) h = 2.2; // a faint baseline so the bars never vanish
        CGFloat x = padX + i * (barW + gap);
        NSBezierPath *p = [NSBezierPath bezierPathWithRoundedRect:
            NSMakeRect(x, cy - h / 2.0, barW, h)
            xRadius:barW / 2.0 yRadius:barW / 2.0];
        [p fill];
    }
}

@end

static NSPanel *gPanel = nil;
static WSPRWaveView *gView = nil;

// buildPanel creates a fresh pill window. We make a new one at the start of
// every recording (and tear it down at the end) rather than keeping a long-lived
// singleton. A window only ever joins the Spaces that exist at the moment it is
// created, and never the fullscreen Spaces created afterwards — a stale
// membership that used to leave the pill missing on, say, a Chrome window you
// fullscreened after launch. Building it fresh each time means it is always born
// into the current set of Spaces, so it appears everywhere with no restart and
// no state to go bad.
static void buildPanel(void) {
    NSRect frame = NSMakeRect(0, 0, 70, 30);
    gPanel = [[NSPanel alloc] initWithContentRect:frame
                                        styleMask:(NSWindowStyleMaskBorderless |
                                                   NSWindowStyleMaskNonactivatingPanel)
                                          backing:NSBackingStoreBuffered
                                            defer:NO];
    [gPanel setOpaque:NO];
    [gPanel setBackgroundColor:[NSColor clearColor]];
    // Screen-saver level keeps the pill above ordinary windows, the Dock, the
    // menu bar and fullscreen apps. FullScreenAuxiliary + CanJoinAllSpaces let
    // it ride along into every Space, including an app's fullscreen Space.
    [gPanel setLevel:NSScreenSaverWindowLevel];
    [gPanel setIgnoresMouseEvents:YES];
    [gPanel setHasShadow:NO];
    [gPanel setHidesOnDeactivate:NO];
    [gPanel setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces |
                                   NSWindowCollectionBehaviorStationary |
                                   NSWindowCollectionBehaviorFullScreenAuxiliary |
                                   NSWindowCollectionBehaviorIgnoresCycle)];
    gView = [[WSPRWaveView alloc] initWithFrame:frame];
    // Layer-back the view. On recent macOS a non-opaque borderless window will
    // not composite a plain drawRect: view to the screen — the window reports
    // itself visible (isVisible=1) yet renders nothing, which is exactly the
    // recurring "pill shows nowhere" symptom. Backing the view with a layer
    // forces AppKit to render drawRect: into a layer that does get composited.
    [gView setWantsLayer:YES];
    [gPanel setContentView:gView]; // panel retains gView
    [gView release];               // panel is now its sole owner
}

// destroyPanel tears the pill window down completely so nothing survives between
// recordings. Releasing the panel also releases its content view (gView).
static void destroyPanel(void) {
    if (gPanel == nil) return;
    [gView stopTimer];
    [gPanel orderOut:nil];
    [gPanel release];
    gPanel = nil;
    gView = nil;
}

// recenterPanel places the pill near the bottom-centre of the display the user
// is currently working on. [NSScreen mainScreen] is the display with keyboard
// focus (not necessarily the primary one), so re-running this as the user moves
// between displays keeps the pill with them — a window is only visible on the
// display it is physically positioned on. Called every animation tick; it only
// moves the window when the target origin actually changes, so staying on one
// display costs nothing.
static void recenterPanel(void) {
    if (gPanel == nil) return;
    NSScreen *screen = [NSScreen mainScreen];
    if (screen == nil) return;
    NSRect sf = [screen frame];
    NSRect pf = [gPanel frame];
    CGFloat x = sf.origin.x + (sf.size.width - pf.size.width) / 2.0;
    CGFloat y = sf.origin.y + 130.0;
    NSPoint origin = NSMakePoint(x, y);
    if (NSEqualPoints(pf.origin, origin)) return;
    [gPanel setFrameOrigin:origin];
}

void overlayShow(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        destroyPanel(); // clear any leftover from a previous recording
        buildPanel();
        recenterPanel();
        [gView startTimer];
        [gPanel orderFrontRegardless];
    });
}

void overlayHide(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        destroyPanel();
    });
}

// overlaySetBands copies the spectrum (Go owns the passed buffer only for the
// duration of this call) and hands it to the view on the main thread.
void overlaySetBands(const double *bands, int n) {
    int m = (n < BARS) ? n : BARS;
    double *copy = (double *)malloc(sizeof(double) * BARS);
    for (int i = 0; i < BARS; i++) copy[i] = (i < m) ? bands[i] : 0.0;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gView != nil) [gView feedBands:copy count:BARS];
        free(copy);
    });
}
