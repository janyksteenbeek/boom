package model

import "time"

// Track represents a music file's metadata.
type Track struct {
	ID       string        `json:"id" db:"id"`
	Path     string        `json:"path" db:"path"`
	Title    string        `json:"title" db:"title"`
	Artist   string        `json:"artist" db:"artist"`
	Album    string        `json:"album" db:"album"`
	Genre    string        `json:"genre" db:"genre"`
	BPM      float64       `json:"bpm" db:"bpm"`
	Key      string        `json:"key" db:"key"`
	Duration time.Duration `json:"duration" db:"duration"`
	Bitrate  int           `json:"bitrate" db:"bitrate"`
	Format   string        `json:"format" db:"format"`
	Size     int64         `json:"size" db:"size"`
	Source     string        `json:"source" db:"source"`
	AddedAt    time.Time     `json:"added_at" db:"added_at"`
	AnalyzedAt time.Time    `json:"analyzed_at" db:"analyzed_at"`
	BeatGrid   string       `json:"beat_grid" db:"beat_grid"`
	CuePoint   float64      `json:"cue_point" db:"cue_point"` // -1 = unset, 0..1 = normalized fraction
}

// HasCuePoint reports whether the track has a saved memory cue.
func (t *Track) HasCuePoint() bool { return t.CuePoint >= 0 }
