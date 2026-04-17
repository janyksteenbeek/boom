# Boom DJ — Raspberry Pi kiosk setup

This guide turns a Raspberry Pi 4/5 with a 5" touch screen into a
dedicated Boom DJ controller screen, CDJ/XDJ-XZ-style. The Pi drives
the screen; a simple MIDI controller handles physical control.

## Hardware

- Raspberry Pi 4 (4 GB) or Pi 5
- Official Pi 5" DSI or HDMI touch screen (800 x 480)
- USB MIDI controller with at least: play/cue/sync, loop controls,
  jog wheels, mixer (crossfader, line faders, EQ), browse encoder,
  LOAD A / LOAD B buttons
- USB audio interface (optional but recommended for latency)

## 1. Pi OS preparation

Flash **Raspberry Pi OS Bookworm (64-bit, desktop)** using the Pi
Imager. During imaging:

- enable SSH
- set username / wifi
- set locale

After first boot, run `raspi-config`:

- System Options → S6 Boot / Auto Login → Desktop Autologin
- Display Options → enable the GL (Fake KMS) driver
- Performance Options → GPU memory split 128 MB

Install kiosk support utilities:

```sh
sudo apt update
sudo apt install unclutter
```

## 2. Install Boom

Cross-compile on your dev machine:

```sh
make build-linux-arm64
scp build/boom-linux-arm64 pi@raspberrypi.local:/tmp/boom
```

On the Pi:

```sh
sudo install -m 755 /tmp/boom /usr/local/bin/boom
mkdir -p ~/.config/boom
cp configs/boom.yaml ~/.config/boom/
```

Point `music_dirs` in `~/.config/boom/boom.yaml` at your music
library (e.g. `/home/pi/Music` or a USB mount).

## 3. Kiosk service

Copy the systemd units from this repo to `~/.config/systemd/user/`:

```sh
scp configs/systemd/boom-kiosk.service pi@raspberrypi.local:~/.config/systemd/user/
scp configs/systemd/unclutter.service  pi@raspberrypi.local:~/.config/systemd/user/
```

Enable both:

```sh
systemctl --user daemon-reload
systemctl --user enable --now unclutter
systemctl --user enable --now boom-kiosk
```

Status check:

```sh
systemctl --user status boom-kiosk
journalctl --user -u boom-kiosk -f
```

## 4. Controller mapping

Drop your controller's MIDI mapping YAML in
`~/.config/boom/controllers/`. The browse encoder must map to
`library.browse_select` (push) and `library.browse_scroll` (rotate) —
these drive the library overlay. `deck.load_track` on your LOAD A /
LOAD B buttons closes the overlay and loads the selected track.

## 5. First boot

Power-cycle the Pi. You should see the Boom mini layout fullscreen
within ~5 seconds: two decks with waveforms, beat grids, and a thin
crossfader indicator at the bottom. Press the browse encoder on your
controller to open the library overlay; scroll to a track; press
LOAD A or LOAD B to send it to a deck and close the overlay.

## Maintenance

SSH into the Pi to update the binary or config — the kiosk has no
on-screen settings UI:

```sh
scp build/boom-linux-arm64 pi@raspberrypi.local:/tmp/boom
ssh pi@raspberrypi.local 'sudo install -m 755 /tmp/boom /usr/local/bin/boom && systemctl --user restart boom-kiosk'
```

## Troubleshooting

- **Black screen on boot**: the GL driver may not be enabled. Re-run
  `raspi-config` → Display Options.
- **Audio glitches**: use a USB audio interface. The Pi onboard jack
  is not reliable at low buffer sizes.
- **Browse overlay doesn't open**: verify your controller mapping
  publishes `library.browse_select`. Test with `cmd/midi-train` to
  see incoming MIDI messages.
- **Cursor visible**: `systemctl --user status unclutter` — service
  may not be active.
