// hotkey_mods.h — the modifier bitmask shared by the global hotkey monitor
// (input_darwin.m) and the "Change Hotkey…" capture popup
// (hotkey_capture_darwin.m). The MOD_* bits must match the mod* constants in
// config.go.
#ifndef WSPR_HOTKEY_MODS_H
#define WSPR_HOTKEY_MODS_H

#import <Cocoa/Cocoa.h>

#define MOD_CTRL   (1 << 0)
#define MOD_SHIFT  (1 << 1)
#define MOD_OPTION (1 << 2)
#define MOD_CMD    (1 << 3)
#define MOD_FN     (1 << 4)
// Side bits: a left/right variant of each side-aware modifier (fn has none).
#define MOD_CTRL_L   (1 << 5)
#define MOD_CTRL_R   (1 << 6)
#define MOD_SHIFT_L  (1 << 7)
#define MOD_SHIFT_R  (1 << 8)
#define MOD_OPTION_L (1 << 9)
#define MOD_OPTION_R (1 << 10)
#define MOD_CMD_L    (1 << 11)
#define MOD_CMD_R    (1 << 12)

// Generic bits decide which modifier *types* are held; side bits are an extra
// constraint layered on top (see triggerHeld in input_darwin.m).
#define MOD_GENERIC_MASK (MOD_CTRL | MOD_SHIFT | MOD_OPTION | MOD_CMD | MOD_FN)
#define MOD_SIDE_MASK                                       \
    (MOD_CTRL_L | MOD_CTRL_R | MOD_SHIFT_L | MOD_SHIFT_R |   \
     MOD_OPTION_L | MOD_OPTION_R | MOD_CMD_L | MOD_CMD_R)

// Virtual key codes for the left/right halves of each side-aware modifier.
#define KEY_CTRL_L   59
#define KEY_CTRL_R   62
#define KEY_SHIFT_L  56
#define KEY_SHIFT_R  60
#define KEY_OPTION_L 58
#define KEY_OPTION_R 61
#define KEY_CMD_L    55
#define KEY_CMD_R    54

// wsprNsMods converts an NSEvent modifier mask to wspr's *generic* bitmask. The
// left/right side is deliberately not read here: a global NSEvent monitor does
// not reliably carry the device-dependent flag bits, so sides are tracked from
// keyCode (see wsprSideBitForKeyCode) instead.
static inline int wsprNsMods(NSEventModifierFlags f) {
    int m = 0;
    if (f & NSEventModifierFlagControl)  m |= MOD_CTRL;
    if (f & NSEventModifierFlagShift)    m |= MOD_SHIFT;
    if (f & NSEventModifierFlagOption)   m |= MOD_OPTION;
    if (f & NSEventModifierFlagCommand)  m |= MOD_CMD;
    if (f & NSEventModifierFlagFunction) m |= MOD_FN;
    return m;
}

// wsprSideBitForKeyCode maps a modifier key's virtual key code to its side bit,
// or 0 for any non-side-aware key. Each press and release of a modifier key
// emits a flagsChanged carrying that key's code, so the caller toggles this bit
// to track which physical key is down.
static inline int wsprSideBitForKeyCode(int kc) {
    switch (kc) {
        case KEY_CTRL_L:   return MOD_CTRL_L;
        case KEY_CTRL_R:   return MOD_CTRL_R;
        case KEY_SHIFT_L:  return MOD_SHIFT_L;
        case KEY_SHIFT_R:  return MOD_SHIFT_R;
        case KEY_OPTION_L: return MOD_OPTION_L;
        case KEY_OPTION_R: return MOD_OPTION_R;
        case KEY_CMD_L:    return MOD_CMD_L;
        case KEY_CMD_R:    return MOD_CMD_R;
    }
    return 0;
}

// wsprReconcileSides clears the side bits of any modifier whose generic flag is
// no longer held, so a missed keyCode toggle can never leave a side stuck down.
static inline int wsprReconcileSides(int sideMods, NSEventModifierFlags f) {
    if (!(f & NSEventModifierFlagControl)) sideMods &= ~(MOD_CTRL_L | MOD_CTRL_R);
    if (!(f & NSEventModifierFlagShift))   sideMods &= ~(MOD_SHIFT_L | MOD_SHIFT_R);
    if (!(f & NSEventModifierFlagOption))  sideMods &= ~(MOD_OPTION_L | MOD_OPTION_R);
    if (!(f & NSEventModifierFlagCommand)) sideMods &= ~(MOD_CMD_L | MOD_CMD_R);
    return sideMods;
}

#endif // WSPR_HOTKEY_MODS_H
