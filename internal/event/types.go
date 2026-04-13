package event

import "github.com/janyksteenbeek/boom/pkg/model"

// Topic identifies an event category for subscription filtering.
type Topic string

const (
	TopicDeck    Topic = "deck"
	TopicMixer   Topic = "mixer"
	TopicLibrary Topic = "library"
	TopicMIDI    Topic = "midi"
	TopicEngine  Topic = "engine"
	TopicUI       Topic = "ui"
	TopicAnalysis Topic = "analysis"
	TopicPlaylist Topic = "playlist"
)

// Deck actions.
const (
	ActionPlay            = "play"
	ActionPause           = "pause"
	ActionPlayPause       = "play_pause"
	ActionCue             = "cue"
	ActionCueDelete       = "cue_delete"
	ActionCueGoStart      = "cue_go_start"
	ActionCuePointChanged = "cue_point_changed"
	ActionSeek            = "seek"
	ActionLoadTrack    = "load_track"
	ActionTrackLoaded  = "track_loaded"
	ActionTrackDecoded = "track_decoded"
	ActionVolumeChange = "volume_change"
	ActionTempoChange  = "tempo_change"
	ActionEQHigh       = "eq_high"
	ActionEQMid        = "eq_mid"
	ActionEQLow        = "eq_low"
	ActionJogScratch       = "jog_scratch"
	ActionJogPitch         = "jog_pitch"
	ActionJogTouch         = "jog_touch"
	ActionVinylMode        = "vinyl_mode"
	ActionVinylModeChanged = "vinyl_mode_changed"
	ActionHotCue       = "hot_cue"
	ActionSync         = "sync"
	ActionLoopIn       = "loop_in"
	ActionLoopOut      = "loop_out"
	ActionLoopToggle   = "loop_toggle"
	ActionLoopHalve    = "loop_halve"
	ActionLoopDouble   = "loop_double"
	ActionBeatLoop     = "beat_loop"          // Value = beat count
	ActionLoopStateUpdate = "loop_state_update" // engine -> UI, Payload: *LoopState
)

// Mixer actions.
const (
	ActionCrossfader   = "crossfader"
	ActionMasterVolume = "master_volume"
	ActionCueVolume    = "cue_volume"
	ActionHeadphoneMix = "headphone_mix"
	ActionHeadphoneCue = "headphone_cue"
)

// Beat FX actions.
const (
	ActionFXSelect   = "fx_select"   // Value: FXType (1=echo, 2=flanger, 3=reverb)
	ActionFXActivate = "fx_activate" // Value: 1.0=on, 0.0=off
	ActionFXWetDry   = "fx_wetdry"   // Value: 0.0–1.0
	ActionFXTime     = "fx_time"     // Value: 0.0–1.0 (mapped to ms range)
	ActionFXNext     = "fx_next"     // Cycle to next effect type
)

// Engine feedback actions (engine -> UI / LED).
const (
	ActionPositionUpdate = "position_update"
	ActionWaveformReady  = "waveform_ready"
	ActionVULevel        = "vu_level"
	ActionBPMDetected    = "bpm_detected"
	ActionPlayState      = "play_state"
	ActionGainChange     = "gain_change"
)

// Library actions.
const (
	ActionBrowseScroll   = "browse_scroll"
	ActionBrowseSelect   = "browse_select"
	ActionTrackSelected  = "track_selected"
	ActionSearchQuery    = "search_query"
	ActionSearchResults  = "search_results"
	ActionFilterCategory = "filter_category"
	ActionSortColumn     = "sort_column"
)

// Playlist actions.
const (
	ActionPlaylistTreeChanged   = "playlist_tree_changed"   // Payload: nil
	ActionPlaylistTracksChanged = "playlist_tracks_changed" // Payload: playlistID string
	ActionPlaylistInvalidated   = "playlist_invalidated"    // Payload: playlistID string
	ActionPlaylistAddTracks     = "playlist_add_tracks"     // Payload: *AddTracksCmd
	ActionPlaylistSelect        = "playlist_select"         // Payload: playlistID string
)

// AddTracksCmd is the payload for ActionPlaylistAddTracks.
type AddTracksCmd struct {
	PlaylistID string
	TrackIDs   []string
}

// Analysis actions.
const (
	ActionAnalyzeRequest   = "analyze_request"   // Payload: []model.Track
	ActionAnalyzeProgress  = "analyze_progress"  // Payload: *AnalysisProgress
	ActionAnalyzeComplete  = "analyze_complete"  // Payload: *AnalysisResult
	ActionAnalyzeBatchDone = "analyze_batch_done" // No payload
	ActionAnalyzeCancel    = "analyze_cancel"     // No payload
	ActionKeyDetected      = "key_detected"       // Payload: key string via Value
)

// AnalysisProgress carries batch analysis progress information.
type AnalysisProgress struct {
	Current int
	Total   int
	TrackID string
}

// LoopState carries the current loop configuration for a deck. A deck has
// no loop when Start < 0 or End <= Start. Active is true while playback
// wraps inside the boundaries.
type LoopState struct {
	Start  float64 // normalized 0..1; <0 = unset
	End    float64 // normalized 0..1; <=Start = unset
	Beats  float64 // 0 = manual loop (no beat length known)
	Active bool
}

// TrackDecodedPayload accompanies ActionTrackDecoded, which fires once a
// deck has finished streaming its full PCM buffer. Carries a reference to
// the decoded samples so downstream consumers (analyzer) can avoid a second
// file decode pass. The slice must be treated as read-only.
type TrackDecodedPayload struct {
	Track      *model.Track
	Samples    [][2]float32
	SampleRate int
}

// AnalysisResult carries completed analysis data for a single track.
type AnalysisResult struct {
	TrackID  string
	BPM      float64
	Key      string
	Gain     float64 // dB offset vs. target loudness (0 = unset/neutral)
	BeatGrid []float64
	DeckID   int // 0=batch, 1/2=deck
}

// DeckIDUnresolved marks an FX event from MIDI that needs target resolution
// via the UI's current FX target. Subsystems that apply effects directly
// (e.g. the audio engine) must ignore this sentinel — only the UI layer
// resolves it and republishes with a concrete DeckID (0=master, 1/2=deck).
const DeckIDUnresolved = -1

// Handler processes an event.
type Handler func(Event) error

// Event is the unit of communication between all subsystems.
type Event struct {
	Topic   Topic
	Action  string
	DeckID  int         // -1=unresolved (MIDI, see DeckIDUnresolved), 0=master, 1=deck1, 2=deck2
	Value   float64     // Normalized 0.0-1.0 for continuous, 1.0/0.0 for toggle
	Pressed bool        // True for press events, false for release (button-style triggers)
	Payload interface{} // Optional typed payload
}
