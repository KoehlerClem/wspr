// wspr is a push-to-talk dictation tool for macOS. It lives in the menu bar:
// hold a global hotkey to record, release to transcribe with a local model and
// paste the text. Everything runs on-device.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

const version = "0.7.0"

// startCfg is the resolved configuration handed to the menu-bar callbacks.
var startCfg Config

func main() {
	// macOS GUI work must happen on the main OS thread.
	runtime.LockOSThread()
	ensurePATH() // a .app launched from Finder/login inherits only a minimal PATH
	initLog()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "devices":
			listDevices()
			return
		case "config":
			showConfig()
			return
		case "models":
			listModels()
			return
		case "history":
			showHistory(os.Args[2:])
			return
		case "download":
			name := ""
			if len(os.Args) > 2 {
				name = os.Args[2]
			}
			downloadModel(name)
			return
		case "help", "-h", "--help":
			usage()
			return
		case "version", "--version":
			fmt.Println("wspr", version)
			return
		}
	}

	guiMode = true // past the subcommand switch: this is the menu-bar app
	cfg, proceed := resolveConfig()
	if !proceed {
		return
	}
	startCfg = cfg
	banner(cfg)
	preflight(cfg)
	// runMenu owns the main thread and the macOS run loop; the dictation
	// engine starts from onMenuReady (see menu.go).
	runMenu()
}

// resolveConfig loads the config file, applies command-line flags, and handles
// --save. The bool is false when the program should exit immediately.
func resolveConfig() (Config, bool) {
	cfgPath := configPath()
	cfg, existed, err := loadConfig(cfgPath)
	if err != nil {
		fatal(err)
	}

	fs := flag.NewFlagSet("wspr", flag.ExitOnError)
	fs.Usage = usage
	hk := fs.String("hotkey", cfg.Hotkey, "push-to-talk hotkey (hold to record)")
	thk := fs.String("toggle-hotkey", cfg.ToggleHotkey, "hotkey to enable/disable dictation")
	mode := fs.String("mode", cfg.Mode, "recording mode: hold | toggle")
	engine := fs.String("engine", cfg.Engine, "transcription engine: parakeet | whisper")
	model := fs.String("model", "", "model for the active engine (overrides config)")
	lang := fs.String("language", cfg.Language, "language hint for whisper, e.g. en (empty = auto)")
	mic := fs.String("mic", cfg.Mic, "input device name (see: wspr devices)")
	noPaste := fs.Bool("no-paste", !cfg.Paste, "copy to clipboard only, don't auto-paste")
	noSounds := fs.Bool("no-sounds", !cfg.Sounds, "disable sound feedback")
	save := fs.Bool("save", false, "write the resolved settings to the config file and exit")
	_ = fs.Parse(os.Args[1:])

	cfg.Hotkey = *hk
	cfg.ToggleHotkey = *thk
	cfg.Mode = strings.ToLower(*mode)
	cfg.Engine = strings.ToLower(*engine)
	cfg.Language = *lang
	cfg.Mic = *mic
	cfg.Paste = !*noPaste
	cfg.Sounds = !*noSounds
	if *model != "" { // --model targets whichever engine is active
		if cfg.Engine == "whisper" {
			cfg.WhisperModel = *model
		} else {
			cfg.ParakeetModel = *model
		}
	}

	if *save {
		if err := saveConfig(cfgPath, cfg); err != nil {
			fatal(err)
		}
		fmt.Println("saved", cfgPath)
		return cfg, false
	}
	if !existed {
		_ = saveConfig(cfgPath, cfg)
		fmt.Println("created config:", cfgPath)
	}
	return cfg, true
}

// preflight verifies that the external tools the chosen engine needs are
// available, exiting with a helpful message if not.
func preflight(cfg Config) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		fatal(fmt.Errorf("ffmpeg not found on PATH — install it with:  brew install ffmpeg"))
	}
	switch cfg.Engine {
	case "parakeet":
		if _, err := exec.LookPath("parakeet-mlx"); err != nil {
			fatal(fmt.Errorf("parakeet-mlx not found on PATH — install it with:\n" +
				"  uv tool install parakeet-mlx      (or:  pip install parakeet-mlx)"))
		}
	case "whisper":
		if _, err := exec.LookPath("whisper-cpp"); err != nil {
			fatal(fmt.Errorf("whisper-cpp not found on PATH — install it with:  brew install whisper-cpp"))
		}
		mp := whisperModelPath(cfg.WhisperModel)
		if _, err := os.Stat(mp); err != nil {
			fatal(fmt.Errorf("whisper model not found: %s\n  download it with:  wspr download %s", mp, cfg.WhisperModel))
		}
	default:
		fatal(fmt.Errorf("unknown engine %q — use \"parakeet\" or \"whisper\"", cfg.Engine))
	}
}

func banner(cfg Config) {
	mode := "auto-paste"
	if !cfg.Paste {
		mode = "clipboard only"
	}
	fmt.Printf("wspr %s — starting (local, on-device)\n", version)
	fmt.Printf("  menu bar  look for the wspr waveform icon up top\n")
	if cfg.Mode == "toggle" {
		fmt.Printf("  tap       %s   to start / stop dictation\n", cfg.Hotkey)
	} else {
		fmt.Printf("  hold      %s   to dictate\n", cfg.Hotkey)
	}
	fmt.Printf("  esc       abort the recording in progress\n")
	fmt.Printf("  press     %s   to enable/disable\n", cfg.ToggleHotkey)
	fmt.Printf("  engine    %s · %s  (%s)\n", cfg.Engine, activeModel(cfg), mode)
	fmt.Printf("  quit      ctrl+c, or Quit from the menu\n\n")
	fmt.Println("Flags (restart wspr with any of these; add --save to persist):")
	fmt.Println(flagsHelp)
	fmt.Println()
}

func listDevices() {
	devs := audioDevices()
	if len(devs) == 0 {
		fmt.Println("No audio input devices found.")
		return
	}
	fmt.Println("Audio input devices (use the name with --mic):")
	fmt.Println()
	for _, d := range devs {
		fmt.Printf("  %s\n", d.name)
	}
}

func showConfig() {
	p := configPath()
	fmt.Println("config file:", p)
	data, err := os.ReadFile(p)
	if err != nil {
		fmt.Println("(not created yet — it is written on first run)")
		return
	}
	fmt.Println()
	fmt.Println(string(data))
}

// flagsHelp is the formatted flag list, shared by `wspr help` and the
// startup banner so they never drift apart.
const flagsHelp = `  --hotkey <combo>         push-to-talk hotkey        (default ctrl+option+space)
  --toggle-hotkey <combo>  enable/disable hotkey      (default ctrl+option+t)
  --mode <hold|toggle>     recording mode             (default hold)
  --engine <name>          parakeet | whisper         (default parakeet)
  --model <name>           model for the active engine
  --language <code>        whisper language hint, e.g. en  (default auto-detect)
  --mic <name>             input device name          (see: wspr devices)
  --no-paste               copy to clipboard only, don't auto-paste
  --no-sounds              disable sound feedback
  --save                   persist the given flags to the config file and exit`

func usage() {
	fmt.Print(`wspr ` + version + ` — local push-to-talk voice dictation for macOS

USAGE
  wspr [flags]        start the menu-bar dictation app
  wspr models         list curated models with their stats
  wspr history [n]    show recent transcriptions (default 20)
  wspr devices        list microphone devices
  wspr download [m]   download a whisper.cpp model (default: configured one)
  wspr config         show the config file path and contents
  wspr version        print version

FLAGS
` + flagsHelp + `

  Hotkeys are modifier(s)+key, e.g. ctrl+option+space, cmd+shift+d, ctrl+f5.
  Prefix a modifier with l/r to pin it to one side, e.g. rcmd, lshift, ropt
  (plain ctrl/shift/option/cmd match either side).
  While recording, press Esc to abort (discard without transcribing).

ENGINES (both run fully on-device)
  parakeet   NVIDIA Parakeet via parakeet-mlx (Apple Silicon).
             Best model: mlx-community/parakeet-tdt-0.6b-v3
             Install:    uv tool install parakeet-mlx
  whisper    OpenAI Whisper via whisper.cpp.
             Best model: large-v3-turbo  (or large-v3 for top accuracy)
             Install:    brew install whisper-cpp && wspr download

EXAMPLES
  wspr --hotkey cmd+shift+space --save
  wspr --engine whisper --model large-v3-turbo
  wspr --engine parakeet --model mlx-community/parakeet-tdt-0.6b-v2
`)
}

func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 70 {
		return s[:69] + "…"
	}
	return s
}

// guiMode is true once wspr is running as the menu-bar app (not a subcommand).
var guiMode bool

// logW receives all log output: stdout plus ~/.config/wspr/wspr.log, so the
// menu-bar app — which has no terminal — stays debuggable.
var logW io.Writer = os.Stdout

// ensurePATH prepends the usual tool directories to PATH. A .app launched from
// Finder or as a login item inherits only a minimal PATH, so ffmpeg, uv,
// whisper-cpp and parakeet-mlx would otherwise not be found.
func ensurePATH() {
	home, _ := os.UserHomeDir()
	path := os.Getenv("PATH")
	for _, d := range []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cargo", "bin"),
	} {
		if !strings.Contains(":"+path+":", ":"+d+":") {
			path = d + ":" + path
		}
	}
	_ = os.Setenv("PATH", path)
}

// initLog tees log output to ~/.config/wspr/wspr.log.
func initLog() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "wspr")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "wspr.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		logW = io.MultiWriter(os.Stdout, f)
	}
}

// tsLayout includes the date so sessions spanning days stay distinguishable in
// the cumulative log file.
const tsLayout = "2006-01-02 15:04:05"

func logInfo(msg string) {
	fmt.Fprintf(logW, "%s  %s\n", time.Now().Format(tsLayout), msg)
}

func logErr(err error) {
	fmt.Fprintf(logW, "%s  error: %v\n", time.Now().Format(tsLayout), err)
}

// recoverLog turns a panic in a goroutine into a logged crash report instead of
// a silent process death. Defer it at the top of any goroutine: the app keeps
// running (minus that one task) and the stack lands in wspr.log.
func recoverLog(where string) {
	if r := recover(); r != nil {
		fmt.Fprintf(logW, "%s  PANIC in %s: %v\n%s\n",
			time.Now().Format(tsLayout), where, r, debug.Stack())
	}
}

// logStream drains a subprocess pipe (typically stderr) line by line into the
// log, tagged with a prefix. Draining also prevents the child from blocking on
// a full pipe. Blank lines are skipped so the log stays readable.
func logStream(prefix string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // tolerate long lines (e.g. tracebacks)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			fmt.Fprintf(logW, "%s  [%s] %s\n", time.Now().Format(tsLayout), prefix, line)
		}
	}
}

func fatal(err error) {
	msg := "wspr: " + err.Error()
	fmt.Fprintln(os.Stderr, msg)
	fmt.Fprintln(logW, msg)
	if guiMode { // a menu-bar app has no terminal — surface the error on screen
		script := `display alert "wspr can't start" message ` + osaQuote(err.Error())
		_ = exec.Command("osascript", "-e", script).Run()
	}
	os.Exit(1)
}

// osaQuote renders s as a quoted AppleScript string literal.
func osaQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}
