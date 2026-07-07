# wspr

A push-to-talk voice dictation **menu-bar app** for macOS, in the spirit of
Whispr Flow — but **100% local**. Audio never leaves your machine.

**Hold a global hotkey anywhere → speak → release → the transcription is pasted
into whatever app you're in.**

- 🔒 Fully on-device transcription — no API key, no network
- 🧠 Two engines: **NVIDIA Parakeet** and **OpenAI Whisper**
- 📊 Live **waveform pill** while recording
- 🎛️ **Menu-bar icon** — toggle dictation, switch model or mic, change the hotkey
- ⏯️ Toggle dictation on/off; **Esc** aborts a recording
- 🕘 Transcription **history**

## Engines

Both run entirely on your Mac. Pick with the menu, `--engine`, or `wspr models`.

| Engine | Runner | Best model | Notes |
|---|---|---|---|
| `parakeet` *(default)* | [`parakeet-mlx`](https://github.com/senstella/parakeet-mlx) | `mlx-community/parakeet-tdt-0.6b-v3` | NVIDIA Parakeet on Apple Silicon. Fast, multilingual. Auto-downloads on first use. |
| `whisper` | [`whisper.cpp`](https://github.com/ggerganov/whisper.cpp) | `large-v3-turbo` | OpenAI Whisper. Models downloaded via `wspr download`. |

Run `wspr models` for the full curated list with stats.

## Requirements

- macOS on Apple Silicon
- [Go](https://go.dev/dl/) 1.26+ (to build)
- The runner for your chosen engine (below)

Microphone capture is built in (CoreAudio via
[miniaudio](https://github.com/mackron/miniaudio)) — no ffmpeg needed.

## Install

```sh
uv tool install parakeet-mlx        # default engine (or: pip install parakeet-mlx)
make install                        # builds and installs to ~/.local/bin
```

For the Whisper engine instead:

```sh
brew install whisper-cpp
wspr download large-v3-turbo        # ~1.6 GB
wspr --engine whisper --save
```

## Using it

```sh
wspr
```

A waveform icon appears in your **menu bar**. Click it to toggle dictation,
switch model or microphone, change the hotkey, or open history/settings.

| Hotkey | Action |
|---|---|
| **hold** `ctrl+option+space` | record while held; transcribe + paste on release |
| `Esc` (while recording) | abort — discard without transcribing |
| `ctrl+option+t` | enable / disable dictation |

While recording, a small **waveform pill** appears near the bottom of the
screen and reacts to your voice. The menu-bar icon turns red.

### macOS permissions

On first run, a **setup window** opens listing the permissions wspr needs.
Each row has a button that triggers the system prompt (or opens the matching
System Settings pane); the row turns green once the permission is granted:

- **Microphone** — to record audio
- **Accessibility** — to watch for the global hotkey and to paste with Cmd+V

The global hotkey is detected with an `NSEvent` monitor, which needs
**Accessibility** — not Input Monitoring. Any key works as a hotkey: a combo,
a single key, or a bare modifier such as the `fn`/🌐 key.

## Commands

```
wspr [flags]        start the menu-bar dictation app
wspr models         list curated models with their stats
wspr history [n]    show recent transcriptions (default 20)
wspr devices        list microphone devices
wspr download [m]   download a whisper.cpp model
wspr config         show the config file path and contents
wspr version        print version
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--hotkey` | `ctrl+option+space` | push-to-talk hotkey |
| `--toggle-hotkey` | `ctrl+option+t` | enable/disable dictation |
| `--engine` | `parakeet` | `parakeet` or `whisper` |
| `--model` | — | model for the active engine |
| `--language` | auto | whisper language hint, e.g. `en` |
| `--mic` | auto | input device name (see `wspr devices`) |
| `--no-paste` | | copy to clipboard only |
| `--no-sounds` | | disable sound feedback |
| `--save` | | persist the given flags to the config file and exit |

A hotkey can be a combo (`cmd+shift+space`), a single key (`f5`), or a bare
modifier (`fn`). Prefix any modifier with `l`/`r` to pin it to one side —
`rcmd`, `lshift`, `ropt`, `lctrl` — while the plain name (`cmd`, `shift`, …)
matches either side. The menu's **Change Hotkey…** opens a popup that shows the
keys as you press them and locks the combo the moment you release a key.

## How it works

1. A global hotkey (an `NSEvent` monitor, gated by Accessibility) starts/stops
   recording.
2. The mic is captured in-process (CoreAudio via miniaudio) to a temp 16 kHz
   mono WAV; a live frequency spectrum of the audio drives the waveform pill.
3. On release, the selected local engine (`parakeet-mlx` or `whisper-cpp`)
   transcribes the WAV — entirely offline.
4. The text is pasted into the focused app with Cmd+V — wspr saves and
   restores your clipboard around the paste, so your copied content is left
   untouched — and appended to `~/.config/wspr/history.jsonl`.

The menu bar, waveform pill, setup guide, and hotkey-capture popup are native
Cocoa (via cgo); everything else is Go.

## Notes

- The first Parakeet run downloads the model (~600 MB) — slow once, then fast.
- Very short presses (<0.4s) are ignored.
- Config and history live in `~/.config/wspr/`.
