package audio

import (
	"log"
	"sync"

	"github.com/janyksteenbeek/boom/internal/audio/output"
)

// devicesBackend is a process-wide backend used solely for enumeration.
// We hold one reference for the life of the app: miniaudio's context
// is heavyweight to spin up, and the settings dialog opens it on every
// invocation otherwise.
var (
	devicesBackend     output.Backend
	devicesBackendOnce sync.Once
	devicesBackendErr  error
)

func ensureDevicesBackend() (output.Backend, error) {
	devicesBackendOnce.Do(func() {
		devicesBackend, devicesBackendErr = output.New()
		if devicesBackendErr != nil {
			log.Printf("audio: device backend init failed: %v", devicesBackendErr)
		}
	})
	return devicesBackend, devicesBackendErr
}

// ListOutputDevices returns the available playback devices, with the
// system default first. The returned slice is always non-nil; on error
// it contains a single sentinel "System Default" entry with empty ID so
// the UI can still render something useful.
func ListOutputDevices() []output.Device {
	defaultEntry := output.Device{ID: "", Name: "System Default", IsDefault: true, NumChannels: 2}
	b, err := ensureDevicesBackend()
	if err != nil {
		return []output.Device{defaultEntry}
	}
	devs, err := b.ListDevices()
	if err != nil {
		log.Printf("audio: list devices failed: %v", err)
		return []output.Device{defaultEntry}
	}
	if len(devs) == 0 {
		return []output.Device{defaultEntry}
	}
	// Prepend a "System Default" entry so the UI can offer the no-op
	// choice without the user having to know which device the OS thinks
	// is currently default.
	out := make([]output.Device, 0, len(devs)+1)
	out = append(out, defaultEntry)
	out = append(out, devs...)
	return out
}
