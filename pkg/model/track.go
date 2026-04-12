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
	FileMtime  int64     `json:"file_mtime" db:"file_mtime"` // unix seconds; 0 = unknown
	Source     string    `json:"source" db:"source"`
	AddedAt    time.Time `json:"added_at" db:"added_at"`
	AnalyzedAt time.Time `json:"analyzed_at" db:"analyzed_at"`
	BeatGrid   []float64 `json:"beat_grid" db:"-"`
	Gain       float64   `json:"gain" db:"gain"`             // track gain in dB (0 = unset/neutral)
	CuePoint   float64   `json:"cue_point" db:"cue_point"`   // -1 = unset, 0..1 = normalized fraction
	PlayCount    int       `json:"play_count" db:"play_count"`
	FirstPlayed  time.Time `json:"first_played" db:"first_played"`
	LastPlayed   time.Time `json:"last_played" db:"last_played"`
}

// HasCuePoint reports whether the track has a saved memory cue.
func (t *Track) HasCuePoint() bool { return t.CuePoint >= 0 }
