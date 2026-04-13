package model

import "time"

// PlaylistKind distinguishes the three node types the tree can hold. Folders
// group other nodes; manual playlists store an explicit track order; auto
// playlists evaluate a rule set against the library.
type PlaylistKind string

const (
	KindFolder PlaylistKind = "folder"
	KindManual PlaylistKind = "playlist"
	KindSmart  PlaylistKind = "smart"
)

// PlaylistNode is a single entry in the playlist tree. Folders and playlists
// share the same row so the tree is a simple self-referencing table.
//
// ExternalID and Source are reserved for external imports — local nodes
// leave them empty, but keeping the columns means that import flows can
// land later without another schema migration.
type PlaylistNode struct {
	ID         string
	ParentID   string
	Kind       PlaylistKind
	Name       string
	Position   int
	Rules      SmartRules // hydrated only for KindSmart
	ExternalID string
	Source     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SmartRules is the rule set an auto playlist evaluates against the
// library. Match is either "all" (AND) or "any" (OR).
type SmartRules struct {
	Match string
	Rules []SmartRule
}

// SmartRule is a single predicate. Field names are whitelisted at
// evaluation time so the set is safe to turn into SQL with bound
// parameters.
//
// Value is interface{} rather than a concrete type because it has to carry
// one of three shapes depending on Op and Field:
//   - string for text fields (title, artist, album, genre, key)
//   - float64 for numeric fields (bpm, gain)
//   - []interface{}{lo, hi} for "between"
//
// The library service converts this to typed columns on write and back
// again on read, so the DB never sees the untyped form.
type SmartRule struct {
	Field string
	Op    string
	Value interface{}
}

// IsEmpty reports whether the rule set would produce an unbounded query.
func (r SmartRules) IsEmpty() bool { return len(r.Rules) == 0 }
