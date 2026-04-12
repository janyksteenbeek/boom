// Package output is a thin, real-time-safe audio output layer.
//
// It hides the platform audio API behind a small interface that supports
// device enumeration and opening multiple output streams to different
// physical devices simultaneously. That second capability is what makes a
// DJ-style multi-out setup (master + headphone cue on different interfaces)
// possible — something neither oto nor beep can do on their own.
//
// On darwin the backend talks to CoreAudio directly via an AUHAL output
// unit, with a lock-free SPSC ring buffer feeding the render callback.
// On linux/windows the package falls back to a single-stream oto backend
// (no enumeration, no cue routing) so the rest of the app keeps working.
package output

import "errors"

// Device is one playback device exposed by the backend.
type Device struct {
	// ID is an opaque, stable, platform-specific identifier (CoreAudio
	// device UID on darwin). Persist this — not Name — when saving a user
	// selection: names are not unique and can change across reboots.
	ID string

	// Name is the human-readable label as shown by the OS.
	Name string

	// IsDefault marks the device the OS reports as the current default
	// output. At most one device has this set.
	IsDefault bool

	// NumChannels is the number of output channels the device offers.
	NumChannels int
}

// StreamConfig describes a requested output stream.
type StreamConfig struct {
	// DeviceID picks the device by Device.ID. Empty opens the system default.
	DeviceID string

	// SampleRate is the requested sample rate in Hz. The backend may
	// negotiate a different rate; check Stream.SampleRate() afterwards.
	SampleRate int

	// BufferFrames is the requested hardware buffer size in frames. Smaller
	// values reduce latency at the cost of CPU and underrun risk. The
	// backend may round to the nearest supported value.
	BufferFrames int

	// NumChannels is interleaved channels per frame. DJ stereo = 2.
	NumChannels int

	// BlockOnFull controls Write semantics when the internal ring buffer
	// has no space left.
	//
	//   true  — Write blocks until all samples are enqueued. This makes the
	//           stream's consumption rate the natural pace for the producer
	//           goroutine. Use this for the master/primary output.
	//   false — Write returns however many frames fit and the rest are
	//           dropped on the floor by the caller. Use this for secondary
	//           outputs (cue/headphone) so a slower-clocked secondary device
	//           can never starve the primary.
	BlockOnFull bool
}

// Stream is one open output to a device. All methods are safe to call
// from any non-audio goroutine; never call Write from the audio thread.
type Stream interface {
	// Write enqueues interleaved samples for playback. Length must be a
	// multiple of NumChannels. Returns the number of frames written. With
	// BlockOnFull=true the call blocks until everything is in; with
	// BlockOnFull=false it may return a short count.
	Write(samples []float32) (int, error)

	// Close stops playback and releases the device. Idempotent. After
	// Close, Write returns ErrStreamClosed.
	Close() error

	// SampleRate is the actual sample rate the device is running at.
	SampleRate() int

	// NumChannels is the channel count of the open stream.
	NumChannels() int

	// Underruns is the cumulative count of audio-thread underruns since
	// the stream was opened. An underrun means the audio thread asked for
	// samples and the producer hadn't supplied them in time — silence is
	// substituted and audible as a click or dropout.
	Underruns() uint64
}

// Backend manages device enumeration and stream creation. A process
// should hold a single Backend instance and reuse it.
type Backend interface {
	// ListDevices returns all currently visible playback devices, with
	// the system default first.
	ListDevices() ([]Device, error)

	// OpenStream opens a new output stream. Multiple streams may be open
	// at once on different devices.
	OpenStream(cfg StreamConfig) (Stream, error)

	// Close releases backend resources. Open streams should be closed
	// first.
	Close() error
}

// Common errors.
var (
	ErrStreamClosed   = errors.New("output: stream closed")
	ErrDeviceNotFound = errors.New("output: device not found")
	ErrUnsupported    = errors.New("output: backend unsupported on this platform")
)

// New returns the default backend for the current platform.
func New() (Backend, error) { return newBackend() }
