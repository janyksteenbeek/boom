package audio

import (
	"log"

	"github.com/gen2brain/malgo"
)

// AudioDevice represents an available audio output device.
type AudioDevice struct {
	Name      string
	ID        malgo.DeviceInfo
	IsDefault bool
}

// ListAudioDevices returns available audio output device names.
func ListAudioDevices() []string {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Printf("audio: failed to init malgo context for device listing: %v", err)
		return []string{"System Default"}
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Playback)
	if err != nil {
		log.Printf("audio: failed to list playback devices: %v", err)
		return []string{"System Default"}
	}

	names := []string{"System Default"}
	for _, d := range devices {
		name := d.Name()
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// FindDeviceByName returns the DeviceInfo for a named device.
// Returns nil if not found or name is empty (use system default).
func FindDeviceByName(name string) *malgo.DeviceInfo {
	if name == "" || name == "System Default" {
		return nil
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil
	}

	for _, d := range devices {
		if d.Name() == name {
			info := d
			return &info
		}
	}
	return nil
}
