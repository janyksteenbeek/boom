package event

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
)

// Deck actions.
const (
	ActionPlay            = "play"
	ActionPause           = "pause"
	ActionPlayPause       = "play_pause"
	ActionCue             = "cue"
	ActionCueDelete       = "cue_delete"
	ActionCuePointChanged = "cue_point_changed"
	ActionSeek            = "seek"
	ActionLoadTrack    = "load_track"
	ActionTrackLoaded  = "track_loaded"
	ActionVolumeChange = "volume_change"
	ActionTempoChange  = "tempo_change"
	ActionEQHigh       = "eq_high"
	ActionEQMid        = "eq_mid"
	ActionEQLow        = "eq_low"
	ActionJogScratch   = "jog_scratch"
	ActionJogPitch     = "jog_pitch"
	ActionHotCue       = "hot_cue"
	ActionSync         = "sync"
	ActionLoopIn       = "loop_in"
	ActionLoopOut      = "loop_out"
	ActionLoopToggle   = "loop_toggle"
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

// AnalysisResult carries completed analysis data for a single track.
type AnalysisResult struct {
	TrackID  string
	BPM      float64
	Key      string
	BeatGrid []float64
	DeckID   int // 0=batch, 1/2=deck
}

// Handler processes an event.
type Handler func(Event) error

// Event is the unit of communication between all subsystems.
type Event struct {
	Topic   Topic
	Action  string
	DeckID  int         // 0=master, 1=deck1, 2=deck2
	Value   float64     // Normalized 0.0-1.0 for continuous, 1.0/0.0 for toggle
	Pressed bool        // True for press events, false for release (button-style triggers)
	Payload interface{} // Optional typed payload
}
