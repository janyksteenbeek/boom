package output

import (
	"os"
	"testing"
	"time"
)

// These tests touch real audio devices and the macOS AudioAnalytics
// framework. AudioAnalytics is known to crash with a Swift cast error
// during process teardown when run from a non-bundled binary like a
// `go test` runner — the same shutdown path works fine inside the
// packaged Boom.app where there's a proper NSApplication / Info.plist.
// We gate the tests behind BOOM_AUDIO_INTEGRATION=1 so a normal
// `go test ./...` doesn't crash on a developer's machine, while still
// letting us run them on demand.
func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("BOOM_AUDIO_INTEGRATION") == "" {
		t.Skip("set BOOM_AUDIO_INTEGRATION=1 to run audio integration tests")
	}
}

// TestEnumerateDevicesSmoke is a sanity check that the portaudio backend
// initialises and returns at least the system default playback device on
// the host running the test.
func TestEnumerateDevicesSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	b, err := New()
	if err != nil {
		t.Skipf("backend init failed (no audio device?): %v", err)
	}
	defer b.Close()

	devs, err := b.ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devs) == 0 {
		t.Skip("no playback devices on host")
	}
	t.Logf("found %d devices via portaudio", len(devs))
	for _, d := range devs {
		t.Logf("  - %-30s default=%-5v ch=%d id=%s",
			d.Name, d.IsDefault, d.NumChannels, truncate(d.ID, 32))
	}
}

// TestMultiStreamSmoke opens two output streams in parallel — the
// motivating use case for this whole package. If portaudio cannot
// open both, this fails loudly.
func TestMultiStreamSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	b, err := New()
	if err != nil {
		t.Skipf("backend init failed: %v", err)
	}
	defer b.Close()

	devs, err := b.ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devs) < 2 {
		t.Skip("need at least 2 playback devices for the multi-stream test")
	}

	master, err := b.OpenStream(StreamConfig{
		DeviceID: devs[0].ID, SampleRate: 48000, BufferFrames: 512,
		NumChannels: 2, BlockOnFull: true,
	})
	if err != nil {
		t.Fatalf("open master on %q: %v", devs[0].Name, err)
	}
	defer master.Close()

	cue, err := b.OpenStream(StreamConfig{
		DeviceID: devs[1].ID, SampleRate: 48000, BufferFrames: 512,
		NumChannels: 2, BlockOnFull: false,
	})
	if err != nil {
		t.Fatalf("open cue on %q: %v", devs[1].Name, err)
	}
	defer cue.Close()

	silence := make([]float32, 4800*2) // 50 ms
	if _, err := master.Write(silence); err != nil {
		t.Fatalf("master write: %v", err)
	}
	if _, err := cue.Write(silence); err != nil {
		t.Fatalf("cue write: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	t.Logf("master=%q (underruns=%d) cue=%q (underruns=%d)",
		devs[0].Name, master.Underruns(),
		devs[1].Name, cue.Underruns())
}

// TestOpenDefaultStreamSmoke opens the system default device, writes a
// short silence buffer, and confirms a clean close. If this works the
// portaudio blocking-write path is wired up correctly end-to-end.
func TestOpenDefaultStreamSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	b, err := New()
	if err != nil {
		t.Skipf("backend init failed: %v", err)
	}
	defer b.Close()

	st, err := b.OpenStream(StreamConfig{
		SampleRate:   48000,
		BufferFrames: 512,
		NumChannels:  2,
		BlockOnFull:  true,
	})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer st.Close()

	// 100 ms of silence at 48 kHz stereo = 9600 frames = 19200 floats.
	silence := make([]float32, 9600*2)
	written, err := st.Write(silence)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written != 9600 {
		t.Fatalf("Write returned %d frames, want 9600", written)
	}

	// Give the audio thread time to drain everything we just wrote so an
	// early Close doesn't cause a spurious underrun count.
	time.Sleep(150 * time.Millisecond)

	if u := st.Underruns(); u > 0 {
		t.Logf("note: %d underruns observed (often expected on first open)", u)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
