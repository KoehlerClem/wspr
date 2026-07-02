package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// historyEntry is one transcription record, stored as a line of JSON.
type historyEntry struct {
	Time          string  `json:"time"`           // RFC3339
	Engine        string  `json:"engine"`         // parakeet | whisper
	Model         string  `json:"model"`          // model used
	AudioSec      float64 `json:"audio_sec"`      // length of the recording
	TranscribeSec float64 `json:"transcribe_sec"` // time spent transcribing
	Text          string  `json:"text"`           // the transcription
}

// historyRefreshCh asks the engine loop to rebuild the History submenu.
var historyRefreshCh = make(chan struct{}, 1)

// historyPath is the JSON-lines file holding all past transcriptions.
func historyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wspr", "history.jsonl")
}

// appendHistory adds one entry to the history file. Failures are non-fatal.
func appendHistory(e historyEntry) {
	p := historyPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if data, err := json.Marshal(e); err == nil {
		_, _ = f.Write(append(data, '\n'))
	}
}

// clearHistory deletes the transcription history file.
func clearHistory() error {
	if err := os.Remove(historyPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// confirmClearHistory shows a confirmation dialog and reports whether the user
// chose to go ahead with clearing the history.
func confirmClearHistory() bool {
	const script = `display dialog "Clear all wspr transcription history? ` +
		`This cannot be undone." buttons {"Cancel", "Clear"} ` +
		`default button "Cancel" cancel button "Cancel" with icon caution`
	return exec.Command("osascript", "-e", script).Run() == nil
}

// loadHistory reads all history entries, oldest first.
func loadHistory() []historyEntry {
	f, err := os.Open(historyPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []historyEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e historyEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// showHistory prints the most recent transcriptions. An optional numeric
// argument overrides the default count.
func showHistory(args []string) {
	limit := 20
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}

	entries := loadHistory()
	if len(entries) == 0 {
		fmt.Println("no history yet — transcriptions are recorded once you start dictating")
		fmt.Println("file:", historyPath())
		return
	}

	start := 0
	if len(entries) > limit {
		start = len(entries) - limit
	}
	fmt.Printf("wspr history — showing %d of %d transcriptions\n\n", len(entries)-start, len(entries))
	for _, e := range entries[start:] {
		ts := e.Time
		if t, err := time.Parse(time.RFC3339, e.Time); err == nil {
			ts = t.Local().Format("2006-01-02 15:04:05")
		}
		fmt.Printf("  %s · %s · %.1fs audio\n", ts, e.Model, e.AudioSec)
		fmt.Printf("    %s\n\n", strings.ReplaceAll(e.Text, "\n", "\n    "))
	}
	fmt.Println("file:", historyPath())
}
