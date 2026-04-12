package audio

import (
	"log"

	"github.com/gen2brain/malgo"
)

// ListAudioDevices returns available audio output device names.
// "System Default" is always the first entry.
func ListAudioDevices() []string {
	fallback := []string{"System Default"}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Printf("audio: failed to init malgo context for device listing: %v", err)
		return fallback
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Playback)
	if err != nil {
		log.Printf("audio: failed to list playback devices: %v", err)
		return fallback
	}

	names := fallback
	seen := map[string]struct{}{"System Default": {}}
	for _, d := range devices {
		name := d.Name()
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
