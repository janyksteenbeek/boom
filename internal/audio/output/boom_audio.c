// Boom audio C wrapper — wraps miniaudio with our own SPSC ring buffer
// and a fully-C audio callback. See boom_audio.h for the rationale.

#include "boom_audio.h"

// Trim a few features we'll never need to keep the binary smaller.
// Decoding/encoding/resampling are all handled by beep on the Go side;
// the resource manager / node graph are higher-level miniaudio APIs we
// don't touch. None of these affect device enumeration or playback.
#define MA_NO_DECODING
#define MA_NO_ENCODING
#define MA_NO_GENERATION
#define MA_NO_RESOURCE_MANAGER
#define MA_NO_NODE_GRAPH
#define MA_NO_THREADING /* not actually safe to define — leave threading on */
#undef MA_NO_THREADING

#define MINIAUDIO_IMPLEMENTATION
#include "miniaudio.h"

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

// ─── Global context ──────────────────────────────────────────────────

static ma_context        g_context;
static int               g_context_initialised = 0;
static ma_device_info*   g_playback_devices    = NULL;
static ma_uint32         g_playback_count      = 0;

int boom_audio_init(void) {
    if (g_context_initialised) return 0;
    ma_result rc = ma_context_init(NULL, 0, NULL, &g_context);
    if (rc != MA_SUCCESS) return (int)rc;
    g_context_initialised = 1;
    return 0;
}

void boom_audio_shutdown(void) {
    if (!g_context_initialised) return;
    ma_context_uninit(&g_context);
    g_context_initialised = 0;
    g_playback_devices    = NULL;
    g_playback_count      = 0;
}

const char* boom_audio_backend_name(void) {
    if (!g_context_initialised) return "uninitialised";
    return ma_get_backend_name(g_context.backend);
}

// ─── Device enumeration ──────────────────────────────────────────────

size_t boom_device_id_size(void) {
    return sizeof(ma_device_id);
}

int boom_device_count(void) {
    if (!g_context_initialised) {
        if (boom_audio_init() != 0) return -1;
    }
    ma_result rc = ma_context_get_devices(
        &g_context,
        &g_playback_devices, &g_playback_count,
        NULL, NULL);
    if (rc != MA_SUCCESS) return -(int)rc;
    return (int)g_playback_count;
}

int boom_device_at(int idx,
                   void*    id_buf,    size_t id_buf_size,
                   char*    name_buf,  int    name_buf_size,
                   int*     is_default,
                   uint32_t* num_channels) {
    if (idx < 0 || (ma_uint32)idx >= g_playback_count) return -1;
    if (!id_buf || id_buf_size < sizeof(ma_device_id))  return -2;
    if (!name_buf || name_buf_size <= 0)                return -3;

    const ma_device_info* info = &g_playback_devices[idx];
    memcpy(id_buf, &info->id, sizeof(ma_device_id));

    // miniaudio's name is null-terminated and at most MA_MAX_DEVICE_NAME_LENGTH+1.
    size_t name_len = strlen(info->name);
    if ((int)name_len >= name_buf_size) name_len = (size_t)name_buf_size - 1;
    memcpy(name_buf, info->name, name_len);
    name_buf[name_len] = '\0';

    if (is_default) *is_default = info->isDefault ? 1 : 0;

    // Channel count is only filled if we ask miniaudio to populate detailed
    // info; the cheap enumeration above leaves it zero. Hint at 2 (stereo)
    // when we don't have a real number — the wrapper opens stereo streams
    // anyway and miniaudio will channel-route as needed.
    if (num_channels) *num_channels = 2;

    return 0;
}

// ─── Stream ──────────────────────────────────────────────────────────

struct boom_stream {
    ma_device        device;
    int              device_started;

    // SPSC ring (interleaved float32). Size = ring_frames * channels.
    float*           ring;
    uint32_t         ring_frames;
    uint32_t         channels;
    uint32_t         buffer_frames;
    uint32_t         sample_rate;

    // Monotonic frame counters; used = write_pos - read_pos.
    _Atomic uint64_t write_pos;
    _Atomic uint64_t read_pos;

    _Atomic uint64_t underruns;
    _Atomic int32_t  closed;
};

// Forward declaration; defined below.
static void boom_data_callback(ma_device* device, void* output,
                               const void* input, ma_uint32 frame_count);

int boom_stream_open(const void* id_bytes, size_t id_bytes_size,
                     uint32_t sample_rate,
                     uint32_t channels,
                     uint32_t buffer_frames,
                     uint32_t ring_frames,
                     boom_stream** out_stream) {
    if (!out_stream || channels == 0 || ring_frames == 0) return -1;
    if (!g_context_initialised) {
        int rc = boom_audio_init();
        if (rc != 0) return rc;
    }

    boom_stream* s = (boom_stream*)calloc(1, sizeof(boom_stream));
    if (!s) return -1;
    s->ring          = (float*)calloc((size_t)ring_frames * channels, sizeof(float));
    if (!s->ring) {
        free(s);
        return -1;
    }
    s->ring_frames   = ring_frames;
    s->channels      = channels;
    s->buffer_frames = buffer_frames;
    s->sample_rate   = sample_rate;
    atomic_store(&s->write_pos, 0);
    atomic_store(&s->read_pos, 0);
    atomic_store(&s->underruns, 0);
    atomic_store(&s->closed, 0);

    ma_device_id resolved_id;
    ma_device_id* id_ptr = NULL;
    if (id_bytes && id_bytes_size >= sizeof(ma_device_id)) {
        memcpy(&resolved_id, id_bytes, sizeof(ma_device_id));
        id_ptr = &resolved_id;
    }

    ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
    cfg.playback.pDeviceID = id_ptr;
    cfg.playback.format    = ma_format_f32;
    cfg.playback.channels  = channels;
    cfg.sampleRate         = sample_rate;
    cfg.periodSizeInFrames = buffer_frames;
    cfg.periods            = 2;
    cfg.dataCallback       = boom_data_callback;
    cfg.pUserData          = s;
    // Allow miniaudio to convert if the device cannot provide our exact
    // format/rate; we want a Stream regardless.
    cfg.performanceProfile = ma_performance_profile_low_latency;

    ma_result rc = ma_device_init(&g_context, &cfg, &s->device);
    if (rc != MA_SUCCESS) {
        free(s->ring);
        free(s);
        return (int)rc;
    }

    // Capture the actual values miniaudio negotiated — they may differ
    // from what we requested.
    s->sample_rate   = s->device.playback.internalSampleRate;
    s->channels      = s->device.playback.internalChannels;
    s->buffer_frames = s->device.playback.internalPeriodSizeInFrames;

    rc = ma_device_start(&s->device);
    if (rc != MA_SUCCESS) {
        ma_device_uninit(&s->device);
        free(s->ring);
        free(s);
        return (int)rc;
    }
    s->device_started = 1;

    *out_stream = s;
    return 0;
}

void boom_stream_close(boom_stream* s) {
    if (!s) return;
    atomic_store(&s->closed, 1);
    if (s->device_started) {
        ma_device_stop(&s->device);
    }
    ma_device_uninit(&s->device);
    free(s->ring);
    s->ring = NULL;
    free(s);
}

// ─── Hot path ────────────────────────────────────────────────────────

uint32_t boom_stream_writable(boom_stream* s) {
    if (!s) return 0;
    uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_relaxed);
    uint64_t r = atomic_load_explicit(&s->read_pos,  memory_order_acquire);
    uint64_t used = w - r;
    if (used >= s->ring_frames) return 0;
    return (uint32_t)(s->ring_frames - used);
}

uint32_t boom_stream_write(boom_stream* s, const float* samples, uint32_t frames) {
    if (!s || !samples || frames == 0) return 0;

    uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_relaxed);
    uint64_t r = atomic_load_explicit(&s->read_pos,  memory_order_acquire);
    uint32_t used = (uint32_t)(w - r);
    if (used >= s->ring_frames) return 0;

    uint32_t free_space = s->ring_frames - used;
    if (frames > free_space) frames = free_space;

    uint32_t pos   = (uint32_t)(w % s->ring_frames);
    uint32_t first = s->ring_frames - pos;
    if (first > frames) first = frames;

    memcpy(s->ring + (size_t)pos * s->channels,
           samples,
           (size_t)first * s->channels * sizeof(float));

    if (first < frames) {
        memcpy(s->ring,
               samples + (size_t)first * s->channels,
               (size_t)(frames - first) * s->channels * sizeof(float));
    }

    atomic_store_explicit(&s->write_pos, w + frames, memory_order_release);
    return frames;
}

uint32_t boom_stream_buffer_frames(boom_stream* s) {
    return s ? s->buffer_frames : 0;
}

uint32_t boom_stream_sample_rate(boom_stream* s) {
    return s ? s->sample_rate : 0;
}

uint32_t boom_stream_channels(boom_stream* s) {
    return s ? s->channels : 0;
}

uint64_t boom_stream_underruns(boom_stream* s) {
    return s ? atomic_load(&s->underruns) : 0;
}

// ─── Audio thread ────────────────────────────────────────────────────

// boom_data_callback runs on miniaudio's audio thread. It MUST NOT
// allocate, lock, or call into Go. It just memcpys from the ring buffer
// into the device buffer; underruns get filled with silence.
static void boom_data_callback(ma_device* device, void* output,
                               const void* input, ma_uint32 frame_count) {
    (void)input;
    boom_stream* s = (boom_stream*)device->pUserData;
    if (!s || !output || frame_count == 0) return;

    float* dst = (float*)output;
    uint32_t channels = s->channels;
    uint32_t frames   = frame_count;

    if (atomic_load_explicit(&s->closed, memory_order_acquire)) {
        memset(dst, 0, (size_t)frames * channels * sizeof(float));
        return;
    }

    uint64_t r = atomic_load_explicit(&s->read_pos,  memory_order_relaxed);
    uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_acquire);
    uint32_t available = (uint32_t)(w - r);

    uint32_t to_copy = frames;
    if (to_copy > available) to_copy = available;

    if (to_copy > 0) {
        uint32_t pos   = (uint32_t)(r % s->ring_frames);
        uint32_t first = s->ring_frames - pos;
        if (first > to_copy) first = to_copy;
        memcpy(dst,
               s->ring + (size_t)pos * channels,
               (size_t)first * channels * sizeof(float));
        if (first < to_copy) {
            memcpy(dst + (size_t)first * channels,
                   s->ring,
                   (size_t)(to_copy - first) * channels * sizeof(float));
        }
        atomic_store_explicit(&s->read_pos, r + to_copy, memory_order_release);
    }

    if (to_copy < frames) {
        memset(dst + (size_t)to_copy * channels,
               0,
               (size_t)(frames - to_copy) * channels * sizeof(float));
        atomic_fetch_add_explicit(&s->underruns, 1, memory_order_relaxed);
    }
}
