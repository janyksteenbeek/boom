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
	Source   string        `json:"source" db:"source"`
	AddedAt  time.Time     `json:"added_at" db:"added_at"`
}
