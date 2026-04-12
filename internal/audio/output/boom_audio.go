package output

/*
#cgo CFLAGS: -Wall -std=c11 -O2
#cgo darwin  LDFLAGS: -framework CoreFoundation -framework CoreAudio -framework AudioUnit -framework AudioToolbox
#cgo linux   LDFLAGS: -ldl -lpthread -lm
#cgo windows LDFLAGS: -lole32

#include <stdlib.h>
#include "boom_audio.h"
*/
import "C"

import (
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// miniaudioBackend is the cross-platform Backend implementation. It
// uses miniaudio internally for device discovery and audio thread
// management, but never lets the audio thread cross back into Go — see
// boom_audio.h for the rationale and the lock-free SPSC ring design.
type miniaudioBackend struct {
	mu          sync.Mutex
	initialised bool
	idSize      int // sizeof(ma_device_id) — fixed for the lifetime of the process
}

func newBackend() (Backend, error) {
	b := &miniaudioBackend{}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *miniaudioBackend) ensureInit() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initialised {
		return nil
	}
	if rc := C.boom_audio_init(); rc != 0 {
		return fmt.Errorf("output: miniaudio init failed (rc=%d)", int(rc))
	}
	b.idSize = int(C.boom_device_id_size())
	b.initialised = true
	return nil
}

// ListDevices returns the current playback device list. The Device.ID
// is a hex-encoded ma_device_id blob — opaque to callers but stable
// across runs because it's the same bytes miniaudio reports.
func (b *miniaudioBackend) ListDevices() ([]Device, error) {
	if err := b.ensureInit(); err != nil {
		return nil, err
	}

	count := int(C.boom_device_count())
	if count < 0 {
		return nil, fmt.Errorf("output: enumerate devices failed (rc=%d)", count)
	}
	if count == 0 {
		return nil, nil
	}

	idBuf := make([]byte, b.idSize)
	nameBuf := make([]byte, 256)

	devices := make([]Device, 0, count)
	for i := 0; i < count; i++ {
		var (
			isDefault   C.int
			numChannels C.uint32_t
		)
		// Reset buffers so leftover bytes from a previous device can't leak
		// into this one's ID/name.
		for j := range idBuf {
			idBuf[j] = 0
		}
		for j := range nameBuf {
			nameBuf[j] = 0
		}
		rc := C.boom_device_at(
			C.int(i),
			unsafe.Pointer(&idBuf[0]), C.size_t(len(idBuf)),
			(*C.char)(unsafe.Pointer(&nameBuf[0])), C.int(len(nameBuf)),
			&isDefault,
			&numChannels,
		)
		if rc != 0 {
			continue
		}

		name := cString(nameBuf)
		if name == "" {
			name = "Audio device"
		}
		devices = append(devices, Device{
			ID:          hex.EncodeToString(idBuf),
			Name:        name,
			IsDefault:   isDefault != 0,
			NumChannels: int(numChannels),
		})
	}

	// Stable order: default first, then in enumeration order.
	for i, d := range devices {
		if d.IsDefault && i != 0 {
			devices[0], devices[i] = devices[i], devices[0]
			break
		}
	}
	return devices, nil
}

// OpenStream opens a playback stream on the requested device. An empty
// DeviceID picks the system default. If the hex blob can't be decoded
// or doesn't match boom_device_id_size, an error is returned rather
// than silently falling back to the default — that would be confusing
// when a saved device disappears.
func (b *miniaudioBackend) OpenStream(cfg StreamConfig) (Stream, error) {
	if err := b.ensureInit(); err != nil {
		return nil, err
	}

	if cfg.NumChannels <= 0 {
		cfg.NumChannels = 2
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 48000
	}
	if cfg.BufferFrames <= 0 {
		cfg.BufferFrames = 512
	}

	var idPtr unsafe.Pointer
	var idSize C.size_t
	if cfg.DeviceID != "" {
		raw, err := hex.DecodeString(cfg.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("output: invalid device id: %w", err)
		}
		if len(raw) != b.idSize {
			return nil, ErrDeviceNotFound
		}
		idPtr = C.CBytes(raw) // freed at the end of OpenStream below
		defer C.free(idPtr)
		idSize = C.size_t(len(raw))
	}

	// Ring sized at 2× the hardware buffer with a small absolute floor.
	// The ring directly determines play-press-to-sound latency: with
	// BlockOnFull=true the producer keeps the ring nearly full, so a
	// 16× ring would mean ~340 ms of pre-buffered audio sitting in front
	// of any new sample. 2× is the minimum that still lets one hardware
	// buffer play while the next is queued, and 1024 frames keeps small
	// hardware buffers (128/256) above ~20 ms of headroom for Go
	// scheduler jitter.
	ring := uint32(cfg.BufferFrames) * 2
	if ring < 1024 {
		ring = 1024
	}
	if ring > 8192 {
		ring = 8192
	}

	var s *C.boom_stream
	rc := C.boom_stream_open(
		idPtr, idSize,
		C.uint32_t(cfg.SampleRate),
		C.uint32_t(cfg.NumChannels),
		C.uint32_t(cfg.BufferFrames),
		C.uint32_t(ring),
		&s,
	)
	if rc != 0 {
		return nil, fmt.Errorf("output: open stream failed (rc=%d)", int(rc))
	}

	st := &miniaudioStream{
		s:           s,
		channels:    int(C.boom_stream_channels(s)),
		sr:          int(C.boom_stream_sample_rate(s)),
		bufferFrms:  int(C.boom_stream_buffer_frames(s)),
		blockOnFull: cfg.BlockOnFull,
	}
	st.bufferPeriod = time.Duration(float64(time.Second) * float64(st.bufferFrms) / float64(st.sr))

	runtime.SetFinalizer(st, (*miniaudioStream).finalize)
	return st, nil
}

// Close is a no-op. The miniaudio context lives for the life of the
// process: tearing it down and re-initialising it confuses CoreAudio
// (we've seen "no object with given ID" errors after a re-init), and
// leaking it costs nothing because the OS reclaims everything at exit.
// If you really need a fresh context (e.g. tests), call ForceShutdown.
func (b *miniaudioBackend) Close() error { return nil }

// ForceShutdown tears down the global miniaudio context. Only intended
// for tests; production code should let the OS clean up at exit.
func ForceShutdown() {
	C.boom_audio_shutdown()
}

// BackendName reports which miniaudio backend is active (Core Audio,
// WASAPI, ALSA, etc.). Useful for diagnostic logs.
func (b *miniaudioBackend) BackendName() string {
	if !b.initialised {
		return "uninitialised"
	}
	return C.GoString(C.boom_audio_backend_name())
}

// miniaudioStream is one open device.
type miniaudioStream struct {
	mu sync.Mutex

	s            *C.boom_stream
	channels     int
	sr           int
	bufferFrms   int
	bufferPeriod time.Duration
	blockOnFull  bool
	closed       atomic.Bool
}

func (s *miniaudioStream) Write(samples []float32) (int, error) {
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

	frames := len(samples) / s.channels
	written := 0
	for written < frames {
		if s.closed.Load() {
			return written, ErrStreamClosed
		}
		base := unsafe.Pointer(&samples[written*s.channels])
		n := uint32(C.boom_stream_write(
			s.s,
			(*C.float)(base),
			C.uint32_t(frames-written),
		))
		written += int(n)

		if int(n) > 0 || written >= frames {
			continue
		}
		if !s.blockOnFull {
			return written, nil
		}
		s.sleepBackpressure()
	}
	return written, nil
}

func (s *miniaudioStream) sleepBackpressure() {
	d := s.bufferPeriod / 4
	if d < 250*time.Microsecond {
		d = 250 * time.Microsecond
	}
	if d > 5*time.Millisecond {
		d = 5 * time.Millisecond
	}
	time.Sleep(d)
}

func (s *miniaudioStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s != nil {
		C.boom_stream_close(s.s)
		s.s = nil
	}
	runtime.SetFinalizer(s, nil)
	return nil
}

func (s *miniaudioStream) SampleRate() int  { return s.sr }
func (s *miniaudioStream) NumChannels() int { return s.channels }

func (s *miniaudioStream) Underruns() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s == nil {
		return 0
	}
	return uint64(C.boom_stream_underruns(s.s))
}

func (s *miniaudioStream) finalize() { _ = s.Close() }

func cString(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// Compile-time assertion that the backend interface stays implemented.
var _ Backend = (*miniaudioBackend)(nil)
