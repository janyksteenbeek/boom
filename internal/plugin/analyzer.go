package plugin

import "github.com/janyksteenbeek/boom/pkg/model"

// AnalysisResult holds the output of a track analysis.
type AnalysisResult struct {
	BPM      float64            `json:"bpm,omitempty"`
	Key      string             `json:"key,omitempty"`
	Beats    []float64          `json:"beats,omitempty"`
	Sections []Section          `json:"sections,omitempty"`
	Custom   map[string]interface{} `json:"custom,omitempty"`
}

// Section represents a detected segment of a track.
type Section struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Label string  `json:"label"` // "intro", "verse", "chorus", "drop", "outro"
}

// Analyzer performs offline analysis on a track.
type Analyzer interface {
	Name() string
	Analyze(track *model.Track, samples []float64, sampleRate int) (AnalysisResult, error)
}
