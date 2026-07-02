// menu_darwin.m — wspr's menu-bar UI: an NSStatusItem and its NSMenu, built
// natively (replacing the fyne.io/systray dependency) so the History list can
// be a real scrollable NSTableView. This file also owns the NSApplication run
// loop.
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

#define HIST_VISIBLE 10     // rows shown before the History list scrolls
#define HIST_ROWH    20.0
#define HIST_WIDTH   360.0

static NSStatusItem *gStatusItem = nil;
static NSMenu       *gMenu = nil;
static NSMenuItem   *gRestart = nil, *gDictation = nil;
static NSMenuItem   *gModeHold = nil, *gModeToggle = nil;
static NSMenu       *gModelMenu = nil, *gMicMenu = nil;
static NSMenuItem   *gAutopaste = nil, *gSounds = nil;
static NSMenuItem   *gHistScrollItem = nil, *gHistEmptyItem = nil, *gHistClear = nil;
static NSTableView  *gHistTable = nil;
static id            gHistSource = nil;
static id            gTarget = nil;

static NSString *str(const char *s) {
    return s ? [NSString stringWithUTF8String:s] : @"";
}

// WSPRHistorySource backs the scrollable History table.
@interface WSPRHistorySource : NSObject <NSTableViewDataSource>
@property (nonatomic, retain) NSArray *rows;
@end
@implementation WSPRHistorySource
- (NSInteger)numberOfRowsInTableView:(NSTableView *)t {
    (void)t;
    return _rows ? (NSInteger)[_rows count] : 0;
}
- (id)tableView:(NSTableView *)t objectValueForTableColumn:(NSTableColumn *)c
            row:(NSInteger)r {
    (void)t; (void)c;
    if (!_rows || r < 0 || r >= (NSInteger)[_rows count]) return @"";
    return [_rows objectAtIndex:r];
}
@end

// WSPRMenuTarget receives every menu action and forwards it to Go.
@interface WSPRMenuTarget : NSObject
@end
@implementation WSPRMenuTarget
- (void)dictation:(id)s    { (void)s; onMenuDictation(); }
- (void)modeHold:(id)s     { (void)s; onMenuModeHold(); }
- (void)modeToggle:(id)s   { (void)s; onMenuModeToggle(); }
- (void)autopaste:(id)s    { (void)s; onMenuAutopaste(); }
- (void)sounds:(id)s       { (void)s; onMenuSounds(); }
- (void)hotkey:(id)s       { (void)s; onMenuHotkey(); }
- (void)restart:(id)s      { (void)s; onMenuRestart(); }
- (void)historyClear:(id)s { (void)s; onMenuHistoryClear(); }
- (void)quit:(id)s         { (void)s; onMenuQuit(); }
- (void)model:(id)s        { onMenuModel((int)[(NSMenuItem *)s tag]); }
- (void)mic:(id)s          { onMenuMic((int)[(NSMenuItem *)s tag]); }
- (void)historyClicked:(id)s {
    NSInteger row = [(NSTableView *)s clickedRow];
    if (row >= 0) {
        onMenuHistory((int)row);
        [gMenu cancelTracking];
    }
}
@end

// item makes a plain clickable menu item targeting gTarget.
static NSMenuItem *item(NSString *title, SEL action) {
    NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title
                                                action:action keyEquivalent:@""];
    [it setTarget:gTarget];
    return it;
}

// buildHistoryView builds the scrollable table shown in the History submenu.
static void buildHistoryView(void) {
    NSRect frame = NSMakeRect(0, 0, HIST_WIDTH, HIST_VISIBLE * HIST_ROWH);
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:frame];
    [scroll setHasVerticalScroller:YES];
    [scroll setHasHorizontalScroller:NO];
    [scroll setHorizontalScrollElasticity:NSScrollElasticityNone]; // no sideways scroll
    [scroll setAutohidesScrollers:YES];
    [scroll setDrawsBackground:NO];
    [scroll setBorderType:NSNoBorder];

    NSTableView *table = [[NSTableView alloc] initWithFrame:frame];
    NSTableColumn *col = [[NSTableColumn alloc] initWithIdentifier:@"h"];
    [col setEditable:NO];
    [col setWidth:HIST_WIDTH - 8.0];
    NSTextFieldCell *cell = [[NSTextFieldCell alloc] initTextCell:@""];
    [cell setFont:[NSFont menuFontOfSize:0]];
    [cell setLineBreakMode:NSLineBreakByTruncatingTail];
    [col setDataCell:cell];
    [table addTableColumn:col];
    [table setHeaderView:nil];
    [table setRowHeight:HIST_ROWH];
    [table setBackgroundColor:[NSColor clearColor]];
    [table setColumnAutoresizingStyle:NSTableViewUniformColumnAutoresizingStyle];
    [table setAutoresizingMask:NSViewWidthSizable]; // track the clip width
    [table setTarget:gTarget];
    [table setAction:@selector(historyClicked:)];

    gHistSource = [[WSPRHistorySource alloc] init];
    [table setDataSource:gHistSource];
    [scroll setDocumentView:table];
    gHistTable = table;

    gHistScrollItem = [[NSMenuItem alloc] init];
    [gHistScrollItem setView:scroll];
}

// buildMenuBar creates the status item and the whole menu (static structure;
// the Model, Microphone and History lists are filled in later by Go).
static void buildMenuBar(void) {
    gTarget = [[WSPRMenuTarget alloc] init];
    gMenu = [[NSMenu alloc] init];
    [gMenu setAutoenablesItems:NO];

    gRestart = item(@"Restart wspr", @selector(restart:));
    [gRestart setHidden:YES];
    [gMenu addItem:gRestart];

    gDictation = item(@"Dictation enabled", @selector(dictation:));
    [gMenu addItem:gDictation];

    NSMenuItem *modeRoot = [[NSMenuItem alloc] initWithTitle:@"Recording mode"
                                                      action:NULL keyEquivalent:@""];
    NSMenu *modeMenu = [[NSMenu alloc] init];
    [modeMenu setAutoenablesItems:NO];
    gModeHold = item(@"Hold to talk", @selector(modeHold:));
    gModeToggle = item(@"Toggle on/off", @selector(modeToggle:));
    [modeMenu addItem:gModeHold];
    [modeMenu addItem:gModeToggle];
    [modeRoot setSubmenu:modeMenu];
    [gMenu addItem:modeRoot];

    NSMenuItem *modelRoot = [[NSMenuItem alloc] initWithTitle:@"Model"
                                                       action:NULL keyEquivalent:@""];
    gModelMenu = [[NSMenu alloc] init];
    [gModelMenu setAutoenablesItems:NO];
    [modelRoot setSubmenu:gModelMenu];
    [gMenu addItem:modelRoot];

    NSMenuItem *micRoot = [[NSMenuItem alloc] initWithTitle:@"Microphone"
                                                     action:NULL keyEquivalent:@""];
    gMicMenu = [[NSMenu alloc] init];
    [gMicMenu setAutoenablesItems:NO];
    NSMenuItem *detecting = [[NSMenuItem alloc] initWithTitle:@"Detecting devices…"
                                                       action:NULL keyEquivalent:@""];
    [detecting setEnabled:NO];
    [gMicMenu addItem:detecting];
    [micRoot setSubmenu:gMicMenu];
    [gMenu addItem:micRoot];

    gAutopaste = item(@"Auto-paste", @selector(autopaste:));
    [gMenu addItem:gAutopaste];
    gSounds = item(@"Sound feedback", @selector(sounds:));
    [gMenu addItem:gSounds];
    [gMenu addItem:item(@"Change Hotkey…", @selector(hotkey:))];
    [gMenu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *histRoot = [[NSMenuItem alloc] initWithTitle:@"History"
                                                      action:NULL keyEquivalent:@""];
    NSMenu *histMenu = [[NSMenu alloc] init];
    [histMenu setAutoenablesItems:NO];
    buildHistoryView();
    [gHistScrollItem setHidden:YES];
    [histMenu addItem:gHistScrollItem];
    gHistEmptyItem = [[NSMenuItem alloc] initWithTitle:@"No transcriptions yet"
                                                action:NULL keyEquivalent:@""];
    [gHistEmptyItem setEnabled:NO];
    [histMenu addItem:gHistEmptyItem];
    [histMenu addItem:[NSMenuItem separatorItem]];
    gHistClear = item(@"Clear History", @selector(historyClear:));
    [gHistClear setEnabled:NO];
    [histMenu addItem:gHistClear];
    [histRoot setSubmenu:histMenu];
    [gMenu addItem:histRoot];

    [gMenu addItem:[NSMenuItem separatorItem]];
    [gMenu addItem:item(@"Quit wspr", @selector(quit:))];

    gStatusItem = [[NSStatusBar systemStatusBar]
        statusItemWithLength:NSVariableStatusItemLength];
    [[gStatusItem button] setToolTip:@"wspr — push-to-talk dictation"];
    [gStatusItem setMenu:gMenu];
}

// menuRun sets up the app, builds the menu, hands control to Go to start the
// engine, and then runs the Cocoa event loop. It does not return.
void menuRun(void) {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    buildMenuBar();
    onMenuReady();
    [NSApp run];
}

void menuQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}

void menuSetIcon(const void *png, int len, int isTemplate) {
    NSData *data = [NSData dataWithBytes:png length:len]; // copies the bytes
    dispatch_async(dispatch_get_main_queue(), ^{
        NSImage *img = [[NSImage alloc] initWithData:data];
        [img setTemplate:(isTemplate ? YES : NO)];
        [img setSize:NSMakeSize(18.0, 18.0)];
        [[gStatusItem button] setImage:img];
    });
}

void menuSetRestart(int visible, const char *title) {
    char *c = strdup(title ? title : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        if (strlen(c) > 0) [gRestart setTitle:str(c)];
        [gRestart setHidden:(visible ? NO : YES)];
        free(c);
    });
}

void menuSetDictation(int checked, const char *title) {
    char *c = strdup(title ? title : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        [gDictation setTitle:str(c)];
        [gDictation setState:(checked ? NSControlStateValueOn : NSControlStateValueOff)];
        free(c);
    });
}

void menuSetMode(int holdChecked) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [gModeHold setState:(holdChecked ? NSControlStateValueOn : NSControlStateValueOff)];
        [gModeToggle setState:(holdChecked ? NSControlStateValueOff : NSControlStateValueOn)];
    });
}

void menuAddModel(const char *label) {
    char *c = strdup(label ? label : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:str(c)
                            action:@selector(model:) keyEquivalent:@""];
        [it setTarget:gTarget];
        [it setTag:(NSInteger)[[gModelMenu itemArray] count]];
        [gModelMenu addItem:it];
        free(c);
    });
}

void menuCheckModel(int idx) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSArray *items = [gModelMenu itemArray];
        for (NSInteger i = 0; i < (NSInteger)[items count]; i++) {
            [[items objectAtIndex:i]
                setState:(i == idx ? NSControlStateValueOn : NSControlStateValueOff)];
        }
    });
}

void menuSetMics(const char *namesNL, int checkedIdx) {
    char *c = strdup(namesNL ? namesNL : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        [gMicMenu removeAllItems];
        NSString *joined = str(c);
        free(c);
        if ([joined length] == 0) {
            NSMenuItem *none = [[NSMenuItem alloc] initWithTitle:@"No input devices found"
                                    action:NULL keyEquivalent:@""];
            [none setEnabled:NO];
            [gMicMenu addItem:none];
            return;
        }
        NSArray *names = [joined componentsSeparatedByString:@"\n"];
        for (NSInteger i = 0; i < (NSInteger)[names count]; i++) {
            NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:[names objectAtIndex:i]
                                action:@selector(mic:) keyEquivalent:@""];
            [it setTarget:gTarget];
            [it setTag:i];
            [it setState:(i == checkedIdx ? NSControlStateValueOn : NSControlStateValueOff)];
            [gMicMenu addItem:it];
        }
    });
}

void menuSetAutopaste(int checked) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [gAutopaste setState:(checked ? NSControlStateValueOn : NSControlStateValueOff)];
    });
}

void menuSetSounds(int checked) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [gSounds setState:(checked ? NSControlStateValueOn : NSControlStateValueOff)];
    });
}

void menuSetHistory(const char *labelsNL, int count) {
    char *c = strdup(labelsNL ? labelsNL : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *joined = str(c);
        free(c);
        if (count <= 0) {
            [gHistScrollItem setHidden:YES];
            [gHistEmptyItem setHidden:NO];
            [gHistClear setEnabled:NO];
            return;
        }
        [gHistEmptyItem setHidden:YES];
        [gHistScrollItem setHidden:NO];
        [gHistClear setEnabled:YES];
        NSArray *rows = [joined componentsSeparatedByString:@"\n"];
        [(WSPRHistorySource *)gHistSource setRows:rows];
        [gHistTable reloadData];
        // Size the table to the rows, and to exactly the clip width so there
        // is no horizontal overflow to scroll into.
        NSScrollView *sv = [gHistTable enclosingScrollView];
        [gHistTable setFrameSize:NSMakeSize([gHistTable frame].size.width,
                                            count * HIST_ROWH)];
        [sv tile];
        [gHistTable setFrameSize:NSMakeSize([sv contentSize].width,
                                            count * HIST_ROWH)];
    });
}
