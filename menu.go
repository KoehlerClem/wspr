package main

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void menuRun(void);
void menuQuit(void);
void menuSetIcon(const void *png, int len, int isTemplate);
void menuSetRestart(int visible, const char *title);
void menuSetDictation(int checked, const char *title);
void menuSetMode(int holdChecked);
void menuAddModel(const char *label);
void menuCheckModel(int idx);
void menuSetMics(const char *namesNL, int checkedIdx);
void menuSetAutopaste(int checked);
void menuSetSounds(int checked);
void menuSetHistory(const char *labelsNL, int count);
*/
import "C"

import (
	"strings"
	"time"
	"unsafe"
)

// historyMax caps how many entries the History list loads (it scrolls).
const historyMax = 200

// theMenu is the live menu; set up in onMenuReady and used by the C callbacks.
var theMenu *menu

// menu holds the click channels the engine loop listens to, plus the data
// behind the dynamic Model / Microphone / History lists.
type menu struct {
	dictation    chan struct{}
	modeHold     chan struct{}
	modeToggle   chan struct{}
	autopaste    chan struct{}
	sounds       chan struct{}
	hotkey       chan struct{}
	restart      chan struct{}
	historyClear chan struct{}
	quit         chan struct{}
	model        chan int
	mic          chan int
	history      chan int

	micDevs      []audioDevice // device behind each Microphone row
	historyTexts []string      // full text behind each History row
}

func newMenu() *menu {
	return &menu{
		dictation:    make(chan struct{}, 4),
		modeHold:     make(chan struct{}, 4),
		modeToggle:   make(chan struct{}, 4),
		autopaste:    make(chan struct{}, 4),
		sounds:       make(chan struct{}, 4),
		hotkey:       make(chan struct{}, 4),
		restart:      make(chan struct{}, 4),
		historyClear: make(chan struct{}, 4),
		quit:         make(chan struct{}, 4),
		model:        make(chan int, 4),
		mic:          make(chan int, 4),
		history:      make(chan int, 4),
	}
}

// runMenu builds the menu bar and runs the Cocoa event loop. It does not return.
func runMenu() { C.menuRun() }

// menuQuitApp terminates the application.
func menuQuitApp() { C.menuQuit() }

// menuSetIcon updates the menu-bar icon from PNG bytes.
func menuSetIcon(png []byte, template bool) {
	if len(png) == 0 {
		return
	}
	C.menuSetIcon(unsafe.Pointer(&png[0]), C.int(len(png)), cbool(template))
}

//export onMenuReady
func onMenuReady() {
	m := newMenu()
	theMenu = m
	for _, mi := range catalog {
		cl := C.CString(modelLabel(mi))
		C.menuAddModel(cl)
		C.free(unsafe.Pointer(cl))
	}
	m.setDictation(true)
	m.setMode(startCfg.Mode)
	m.setModel(startCfg)
	m.setAutopaste(startCfg.Paste)
	m.setSounds(startCfg.Sounds)
	iconSetIdle()
	go engineLoop(startCfg, m)
}

//export onMenuDictation
func onMenuDictation() { menuClick(theMenu.dictation) }

//export onMenuModeHold
func onMenuModeHold() { menuClick(theMenu.modeHold) }

//export onMenuModeToggle
func onMenuModeToggle() { menuClick(theMenu.modeToggle) }

//export onMenuAutopaste
func onMenuAutopaste() { menuClick(theMenu.autopaste) }

//export onMenuSounds
func onMenuSounds() { menuClick(theMenu.sounds) }

//export onMenuHotkey
func onMenuHotkey() { menuClick(theMenu.hotkey) }

//export onMenuRestart
func onMenuRestart() { menuClick(theMenu.restart) }

//export onMenuHistoryClear
func onMenuHistoryClear() { menuClick(theMenu.historyClear) }

//export onMenuQuit
func onMenuQuit() { menuClick(theMenu.quit) }

//export onMenuModel
func onMenuModel(idx C.int) { menuClickInt(theMenu.model, int(idx)) }

//export onMenuMic
func onMenuMic(idx C.int) { menuClickInt(theMenu.mic, int(idx)) }

//export onMenuHistory
func onMenuHistory(idx C.int) { menuClickInt(theMenu.history, int(idx)) }

func menuClick(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func menuClickInt(ch chan int, v int) {
	select {
	case ch <- v:
	default:
	}
}

func (m *menu) setDictation(enabled bool) {
	title := "Dictation enabled"
	if !enabled {
		title = "Dictation disabled"
	}
	ct := C.CString(title)
	C.menuSetDictation(cbool(enabled), ct)
	C.free(unsafe.Pointer(ct))
}

func (m *menu) setMode(mode string) { C.menuSetMode(cbool(mode != "toggle")) }

// setModel ticks the catalog entry the config currently selects.
func (m *menu) setModel(cfg Config) {
	active := activeModel(cfg)
	idx := -1
	for i, mi := range catalog {
		if mi.name == active && mi.engine == cfg.Engine {
			idx = i
			break
		}
	}
	C.menuCheckModel(C.int(idx))
}

func (m *menu) setAutopaste(b bool) { C.menuSetAutopaste(cbool(b)) }
func (m *menu) setSounds(b bool)    { C.menuSetSounds(cbool(b)) }

func (m *menu) setRestart(visible bool, title string) {
	ct := C.CString(title)
	C.menuSetRestart(cbool(visible), ct)
	C.free(unsafe.Pointer(ct))
}

// setMics fills the Microphone submenu from the detected device list.
func (m *menu) setMics(devs []audioDevice, cfg Config) {
	m.micDevs = devs
	m.setMicChecks(cfg)
}

// setMicChecks rebuilds the Microphone submenu, ticking the configured device.
func (m *menu) setMicChecks(cfg Config) {
	active := activeMicName(cfg.Mic, m.micDevs)
	names := make([]string, len(m.micDevs))
	checked := -1
	for i, d := range m.micDevs {
		names[i] = d.name
		if d.name == active {
			checked = i
		}
	}
	cn := C.CString(strings.Join(names, "\n"))
	C.menuSetMics(cn, C.int(checked))
	C.free(unsafe.Pointer(cn))
}

// refreshHistory rebuilds the History list from the history file, newest first.
func (m *menu) refreshHistory() {
	entries := loadHistory()
	total := len(entries)
	n := total
	if n > historyMax {
		n = historyMax
	}
	m.historyTexts = make([]string, n)
	labels := make([]string, n)
	for i := 0; i < n; i++ {
		e := entries[total-1-i] // newest first
		m.historyTexts[i] = e.Text
		labels[i] = historyItemLabel(e)
	}
	cl := C.CString(strings.Join(labels, "\n"))
	C.menuSetHistory(cl, C.int(n))
	C.free(unsafe.Pointer(cl))
}

// modelLabel turns a catalog entry into a short, readable menu label.
func modelLabel(mi modelInfo) string {
	name := mi.name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if mi.engine == "parakeet" {
		return "NVIDIA " + name
	}
	return "Whisper " + name
}

// historyItemLabel renders one history entry as a compact list row.
func historyItemLabel(e historyEntry) string {
	ts := e.Time
	if t, err := time.Parse(time.RFC3339, e.Time); err == nil {
		ts = t.Local().Format("15:04")
	}
	text := strings.Join(strings.Fields(e.Text), " ")
	if r := []rune(text); len(r) > 60 {
		text = string(r[:59]) + "…"
	}
	return ts + "  " + text
}
