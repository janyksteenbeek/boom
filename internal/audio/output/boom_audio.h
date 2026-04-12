// Boom audio C API — a thin, hand-written cgo surface over miniaudio.
//
// We deliberately do NOT use the malgo Go bindings: malgo's audio
// callback hops back into Go via cgo, which couples the audio thread to
// the Go scheduler and GC and was the root cause of the audible
// glitching we saw in earlier prototypes.
//
// Instead, this wrapper keeps the audio callback fully C-side. The Go
// producer goroutine writes interleaved float32 samples into a
// lock-free SPSC ring buffer; miniaudio's audio thread drains that ring
// from inside boom_data_callback and writes the result straight into
// the device buffer. There is no Go code on the audio thread, ever.
//
// Device IDs are exposed to Go as opaque byte blobs (the contents of an
// ma_device_id union). The Go side stores them as hex strings in
// config; OpenStream takes the same blob and passes it back to
// miniaudio. Names can collide and change across reboots; ma_device_id
// is the only thing that's actually stable.

#ifndef BOOM_AUDIO_H
#define BOOM_AUDIO_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct boom_stream boom_stream;

// ─── Backend lifecycle ───────────────────────────────────────────────

// boom_audio_init initialises the global miniaudio context. Idempotent.
// Returns 0 on success, non-zero (an ma_result code) on failure.
int boom_audio_init(void);

// boom_audio_shutdown tears down the global context. Safe to call when
// no streams are open. Idempotent.
void boom_audio_shutdown(void);

// boom_audio_backend_name returns the human-readable name of the active
// miniaudio backend (e.g. "Core Audio", "WASAPI", "ALSA"). The returned
// string is statically allocated by miniaudio and never null.
const char* boom_audio_backend_name(void);

// ─── Device enumeration ──────────────────────────────────────────────

// boom_device_id_size returns sizeof(ma_device_id), so the Go side can
// allocate a fixed-size buffer for the opaque ID blob.
size_t boom_device_id_size(void);

// boom_device_count refreshes the device list and returns the number of
// playback devices. Negative on error.
int boom_device_count(void);

// boom_device_at fills the supplied buffers with information about the
// playback device at index `idx`. `id_buf` must be at least
// boom_device_id_size() bytes; `name_buf` must be at least 256 bytes.
// Returns 0 on success.
int boom_device_at(int idx,
                   void*    id_buf,    size_t id_buf_size,
                   char*    name_buf,  int    name_buf_size,
                   int*     is_default,
                   uint32_t* num_channels);

// ─── Stream lifecycle ────────────────────────────────────────────────

// boom_stream_open creates and starts a playback stream on the
// requested device. Pass NULL for `id_bytes` to use the system default.
//
//   sample_rate    requested sample rate in Hz
//   channels       interleaved channel count (typically 2)
//   buffer_frames  hardware buffer size hint, in frames
//   ring_frames    SPSC ring buffer size in frames
//
// On success the function returns 0 and *out_stream points at a
// heap-allocated boom_stream owned by the caller. On failure the return
// value is an ma_result code and *out_stream is left untouched.
int boom_stream_open(const void* id_bytes, size_t id_bytes_size,
                     uint32_t sample_rate,
                     uint32_t channels,
                     uint32_t buffer_frames,
                     uint32_t ring_frames,
                     boom_stream** out_stream);

// boom_stream_close stops the device, releases miniaudio resources and
// frees the stream. After this call the boom_stream pointer is invalid.
void boom_stream_close(boom_stream* s);

// ─── Hot path ────────────────────────────────────────────────────────

// boom_stream_writable returns the number of frames currently free in
// the ring buffer.
uint32_t boom_stream_writable(boom_stream* s);

// boom_stream_write copies up to `frames` interleaved frames from
// `samples` into the ring buffer and returns the number of frames
// actually written (0 if the ring is full). The audio thread drains the
// ring from boom_data_callback, so the producer just keeps the ring
// topped up.
uint32_t boom_stream_write(boom_stream* s, const float* samples, uint32_t frames);

// ─── Stats ───────────────────────────────────────────────────────────

uint32_t boom_stream_buffer_frames(boom_stream* s);
uint32_t boom_stream_sample_rate(boom_stream* s);
uint32_t boom_stream_channels(boom_stream* s);
uint64_t boom_stream_underruns(boom_stream* s);

#ifdef __cplusplus
}
#endif

#endif // BOOM_AUDIO_H
