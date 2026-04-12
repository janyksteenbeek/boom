package output

// Backend implementation built on github.com/gordonklaus/portaudio.
//
// We use portaudio in **blocking mode** (no callback). The audio thread
// is owned entirely by the portaudio C library; on the Go side we just
// fill a pre-allocated output buffer and call Stream.Write, which blocks
// until portaudio has consumed it. This means there is no Go code on the
// audio thread, no cgo→Go callback overhead, and no Go GC interaction
// with the real-time render loop — the same architectural constraint we
// hit with malgo, just satisfied differently here.
//
// libportaudio is a runtime dependency. On macOS install it via
// `brew install portaudio`; the packaged Boom.app ships its own copy of
// libportaudio.dylib alongside the binary so end users don't need brew.

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gordonklaus/portaudio"
)

var (
	paInitOnce sync.Once
	paInitErr  error
)

func paEnsureInitialized() error {
	paInitOnce.Do(func() {
		paInitErr = portaudio.Initialize()
	})
	return paInitErr
}

type paBackend struct{}

func newBackend() (Backend, error) {
	if err := paEnsureInitialized(); err != nil {
		return nil, fmt.Errorf("output: portaudio init: %w", err)
	}
	return &paBackend{}, nil
}

// ListDevices returns every output-capable device portaudio sees, with
// the system default first.
func (b *paBackend) ListDevices() ([]Device, error) {
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("output: list devices: %w", err)
	}
	defaultDev, _ := portaudio.DefaultOutputDevice()

	out := make([]Device, 0, len(devs))
	for _, d := range devs {
		if d == nil || d.MaxOutputChannels == 0 {
			continue
		}
		out = append(out, Device{
			ID:          deviceIDFor(d),
			Name:        d.Name,
			IsDefault:   defaultDev != nil && d == defaultDev,
			NumChannels: d.MaxOutputChannels,
		})
	}

	for i, d := range out {
		if d.IsDefault && i != 0 {
			out[0], out[i] = out[i], out[0]
			break
		}
	}
	return out, nil
}

// deviceIDFor builds a stable string identifier from the device's host
// API and name. portaudio's own DeviceInfo pointer is process-local and
// not safe to persist; this combination is unique per device on the
// machine and survives across runs.
func deviceIDFor(d *portaudio.DeviceInfo) string {
	host := ""
	if d.HostApi != nil {
		host = d.HostApi.Name
	}
	return host + "::" + d.Name
}

func (b *paBackend) findDevice(id string) (*portaudio.DeviceInfo, error) {
	if id == "" {
		return portaudio.DefaultOutputDevice()
	}
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}
	for _, d := range devs {
		if d == nil || d.MaxOutputChannels == 0 {
			continue
		}
		if deviceIDFor(d) == id {
			return d, nil
		}
	}
	return nil, ErrDeviceNotFound
}

// OpenStream opens a portaudio output stream in blocking mode. The
// returned Stream owns its output buffer; each call to Stream.Write
// copies the caller's samples into that buffer and then blocks inside
// portaudio.Stream.Write until the audio thread has consumed it.
func (b *paBackend) OpenStream(cfg StreamConfig) (Stream, error) {
	if cfg.NumChannels <= 0 {
		cfg.NumChannels = 2
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48000
	}
	if cfg.BufferFrames <= 0 {
		cfg.BufferFrames = 512
	}

	devInfo, err := b.findDevice(cfg.DeviceID)
	if err != nil {
		return nil, err
	}
	if devInfo == nil {
		return nil, ErrDeviceNotFound
	}

	params := portaudio.LowLatencyParameters(nil, devInfo)
	params.Output.Channels = cfg.NumChannels
	params.SampleRate = float64(cfg.SampleRate)
	params.FramesPerBuffer = cfg.BufferFrames

	outBuf := make([]float32, cfg.BufferFrames*cfg.NumChannels)
	stream, err := portaudio.OpenStream(params, outBuf)
	if err != nil {
		return nil, fmt.Errorf("output: open stream on %q: %w", devInfo.Name, err)
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("output: start stream on %q: %w", devInfo.Name, err)
	}

	return &paStream{
		stream:      stream,
		outBuf:      outBuf,
		channels:    cfg.NumChannels,
		sr:          int(params.SampleRate),
		bufferFrms:  cfg.BufferFrames,
		blockOnFull: cfg.BlockOnFull,
	}, nil
}

// Close is a no-op. portaudio.Initialize is process-global; calling
// Terminate while another stream is open is undefined behavior, and
// re-initialising it later confused us with malgo. Letting the OS
// reclaim it at process exit is fine.
func (b *paBackend) Close() error { return nil }

// paStream wraps a single portaudio.Stream in blocking mode.
type paStream struct {
	mu sync.Mutex // serializes Write/Close

	stream *portaudio.Stream
	outBuf []float32

	channels    int
	sr          int
	bufferFrms  int
	blockOnFull bool
	closed      atomic.Bool

	underruns atomic.Uint64
}

// Write copies the caller's interleaved samples into the stream's
// output buffer in chunks of bufferFrms and submits each chunk to
// portaudio. With BlockOnFull=true the underlying portaudio Write
// blocks until the audio thread accepts the chunk; with BlockOnFull
// =false we check AvailableToWrite first and drop the rest of the
// caller's samples if there isn't space, so a slower-clocked secondary
// device can never starve the master.
func (s *paStream) Write(samples []float32) (int, error) {
	if s.closed.Load() {
		return 0, ErrStreamClosed
	}
	if len(samples) == 0 {
		return 0, nil
	}
	if len(samples)%s.channels != 0 {
		return 0, fmt.Errorf("output: write length %d not a multiple of channels %d",
			len(samples), s.channels)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.stream == nil {
		return 0, ErrStreamClosed
	}

	frames := len(samples) / s.channels
	written := 0
	for written < frames {
		chunk := frames - written
		if chunk > s.bufferFrms {
			chunk = s.bufferFrms
		}

		if !s.blockOnFull {
			avail, err := s.stream.AvailableToWrite()
			if err != nil {
				return written, err
			}
			if avail < chunk {
				// Drop the remainder of this Write call rather than
				// stalling. The cue path uses this so it cannot back-
				// pressure into the master feeder.
				return written, nil
			}
		}

		// Copy the next chunk into the stream's persistent output
		// buffer. portaudio.Stream.Write reads exactly bufferFrms*
		// channels samples from this buffer; if our chunk is short we
		// pad with silence so the trailing frames of a partial write
		// don't replay leftover audio from the previous call.
		need := chunk * s.channels
		copy(s.outBuf[:need], samples[written*s.channels:written*s.channels+need])
		if need < len(s.outBuf) {
			tail := s.outBuf[need:]
			for i := range tail {
				tail[i] = 0
			}
		}

		if err := s.stream.Write(); err != nil {
			// portaudio reports underrun via paOutputUnderflowed; bump
			// the counter and keep going so a transient hiccup doesn't
			// kill the stream.
			if isUnderflow(err) {
				s.underruns.Add(1)
			} else {
				return written, err
			}
		}
		written += chunk
	}
	return written, nil
}

func isUnderflow(err error) bool {
	return err != nil && err.Error() == portaudio.OutputUnderflowed.Error()
}

func (s *paStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		_ = s.stream.Stop()
		_ = s.stream.Close()
		s.stream = nil
	}
	return nil
}

func (s *paStream) SampleRate() int    { return s.sr }
func (s *paStream) NumChannels() int   { return s.channels }
func (s *paStream) Underruns() uint64  { return s.underruns.Load() }

// Compile-time assertion that the backend interface stays implemented.
var _ Backend = (*paBackend)(nil)
var _ Stream = (*paStream)(nil)
