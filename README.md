<p align="center">
  <img src="assets/logo/SVG/logo-boom.svg" alt="Boom" width="300">
</p>

<p align="center">
  Cross-platform DJ performance tool built in Go. Lightweight alternative to Rekordbox/DJay Pro that runs on macOS, Linux (including Raspberry Pi), and Windows.
</p>

<p align="center">
  <img src="assets/screenshot.png" alt="Boom screenshot" width="800">
</p>

## Features

- **Dual deck playback** with real-time waveform display
- **MIDI controller support** — YAML-based mappings (ships with Pioneer DDJ-FLX4)
- **Music library** with automatic metadata scanning backed by SQLite
- **Crossfade mixer** with 3-band EQ and per-channel gain
- **Plugin architecture** for effects, analyzers, and remote library sources
- **Event-driven** — all subsystems communicate via a decoupled pub/sub bus

## Requirements

- Go 1.26+
- C compiler (CGo is required for OpenGL and audio backends)

Platform-specific dependencies:

| Platform | Dependencies |
|---|---|
| macOS | Xcode Command Line Tools |
| Linux | `libasound2-dev libgl1-mesa-dev xorg-dev` |
| Windows | MSYS2 or TDM-GCC |

## Quick start

```sh
make build
./build/boom
```

## Build targets

```
make build              Build for current platform
make run                Build and run
make test               Run tests
make lint               Run golangci-lint
make build-linux        Cross-compile for Linux amd64
make build-linux-arm64  Cross-compile for Linux arm64 (Raspberry Pi)
make build-windows      Cross-compile for Windows amd64
make build-all          Build all targets
make midi-train         Build MIDI mapping trainer utility
```

## MIDI controller mapping

Controller mappings live in `configs/controllers/` as YAML files. Use the included trainer to learn new controller mappings interactively:

```sh
make midi-train
./build/midi-train
```

## Project layout

```
cmd/boom/              Main application entry point
cmd/midi-train/        MIDI mapping utility
internal/
  app/                 Application lifecycle
  audio/               Audio engine, decks, mixer, EQ, waveform analysis
  config/              Configuration loading and defaults
  controller/          MIDI controller mapping, layers, LED feedback
  event/               Pub/sub event bus
  library/             Music library, metadata scanning, SQLite store
  midi/                MIDI device management
  plugin/              Plugin interfaces (effects, analyzers, sources)
  ui/                  Fyne UI — deck views, mixer, browser, settings
pkg/model/             Shared data types (Track, Deck)
configs/               Default config and controller mappings
```

## License

[MIT](LICENSE)
