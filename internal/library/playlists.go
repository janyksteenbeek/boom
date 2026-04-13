package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/janyksteenbeek/boom/internal/event"
	"github.com/janyksteenbeek/boom/pkg/model"
)

// positionGap is the spacing between adjacent positions when a node or track
// is inserted. Leaving gaps turns most reorder operations into a single
// UPDATE; we only renumber a block when the gaps run out.
const positionGap = 1024

// PlaylistService owns the playlist tree and playlist membership tables. All
// mutations funnel through here so a single spot is responsible for emitting
// TopicPlaylist events on the bus.
type PlaylistService struct {
	store *Store
	bus   *event.Bus
}

// NewPlaylistService wires the service to the shared Store and event bus and
// subscribes to analysis completions so auto playlists get invalidated when
// new BPM/key/gain data lands.
func NewPlaylistService(store *Store, bus *event.Bus) *PlaylistService {
	p := &PlaylistService{store: store, bus: bus}
	if bus != nil {
		bus.Subscribe(event.TopicAnalysis, p.onAnalysis)
	}
	return p
}

// Tree returns every node ordered so that siblings follow their stored
// position and rows are stable inside a parent. Callers build the visible
// tree from the flat list using ParentID. Rules are NOT loaded here — that
// happens on demand via Node() or Tracks() to keep the tree query cheap.
func (p *PlaylistService) Tree() ([]*model.PlaylistNode, error) {
	rows, err := p.store.db.Query(`
		SELECT id, COALESCE(parent_id, ''), kind, name, position, match,
		       external_id, source, created_at, updated_at
		FROM playlist_nodes
		ORDER BY COALESCE(parent_id, ''), position ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	defer rows.Close()

	var out []*model.PlaylistNode
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Node fetches a single playlist node by ID, fully hydrated with any rules.
func (p *PlaylistService) Node(id string) (*model.PlaylistNode, error) {
	row := p.store.db.QueryRow(`
		SELECT id, COALESCE(parent_id, ''), kind, name, position, match,
		       external_id, source, created_at, updated_at
		FROM playlist_nodes WHERE id = ?
	`, id)
	n, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if n.Kind == model.KindSmart {
		rules, err := p.loadRules(id)
		if err != nil {
			return nil, err
		}
		n.Rules = rules
	}
	return n, nil
}

// scanNode is used by both Tree and Node so they stay in lockstep when the
// column list changes.
func scanNode(r rowScanner) (*model.PlaylistNode, error) {
	n := &model.PlaylistNode{}
	var createdStr, updatedStr string
	if err := r.Scan(&n.ID, &n.ParentID, &n.Kind, &n.Name, &n.Position,
		&n.Rules.Match, &n.ExternalID, &n.Source, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	n.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	n.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return n, nil
}

// loadRules reads the normalized rule rows for a playlist, in their stored
// order, and converts each row back into a model.SmartRule with a typed
// Value field (float64 for numeric fields, string for text, []float64 for
// between).
func (p *PlaylistService) loadRules(playlistID string) (model.SmartRules, error) {
	out := model.SmartRules{Match: "all"}
	// Read match from the node row — callers that came through Node() have
	// it already, but loadRules is also used after an UpdateRules where we
	// want the freshest value.
	var match string
	if err := p.store.db.QueryRow(`SELECT match FROM playlist_nodes WHERE id = ?`, playlistID).Scan(&match); err == nil {
		out.Match = match
	}

	rows, err := p.store.db.Query(`
		SELECT field, op, value_text, value_num, value_num2
		FROM playlist_rules
		WHERE playlist_id = ?
		ORDER BY position ASC
	`, playlistID)
	if err != nil {
		return out, fmt.Errorf("load rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r model.SmartRule
		var vText string
		var vNum, vNum2 float64
		if err := rows.Scan(&r.Field, &r.Op, &vText, &vNum, &vNum2); err != nil {
			return out, fmt.Errorf("scan rule: %w", err)
		}
		r.Value = hydrateRuleValue(r.Field, r.Op, vText, vNum, vNum2)
		out.Rules = append(out.Rules, r)
	}
	return out, rows.Err()
}

// hydrateRuleValue turns the typed DB columns back into the Value shape the
// rest of the code expects — float64 for numeric, string for text, two
// floats for between.
func hydrateRuleValue(field, op, vText string, vNum, vNum2 float64) interface{} {
	if op == "between" {
		return []interface{}{vNum, vNum2}
	}
	if isNumericField(field) {
		return vNum
	}
	return vText
}

func isNumericField(field string) bool {
	return field == "bpm" || field == "gain"
}

// CreateFolder creates a folder under parentID ("" = root).
func (p *PlaylistService) CreateFolder(parentID, name string) (*model.PlaylistNode, error) {
	return p.createNode(parentID, name, model.KindFolder, model.SmartRules{})
}

// CreatePlaylist creates an empty manual playlist.
func (p *PlaylistService) CreatePlaylist(parentID, name string) (*model.PlaylistNode, error) {
	return p.createNode(parentID, name, model.KindManual, model.SmartRules{})
}

// CreateSmart creates an auto playlist with the given rule set.
func (p *PlaylistService) CreateSmart(parentID, name string, rules model.SmartRules) (*model.PlaylistNode, error) {
	return p.createNode(parentID, name, model.KindSmart, rules)
}

func (p *PlaylistService) createNode(parentID, name string, kind model.PlaylistKind, rules model.SmartRules) (*model.PlaylistNode, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("playlist name cannot be empty")
	}
	pos, err := p.nextPosition(parentID)
	if err != nil {
		return nil, err
	}
	match := rules.Match
	if match == "" {
		match = "all"
	}
	now := time.Now().UTC()
	node := &model.PlaylistNode{
		ID:        uuid.NewString(),
		ParentID:  parentID,
		Kind:      kind,
		Name:      name,
		Position:  pos,
		Rules:     rules,
		Source:    "local",
		CreatedAt: now,
		UpdatedAt: now,
	}
	var parent interface{}
	if parentID != "" {
		parent = parentID
	}

	tx, err := p.store.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO playlist_nodes (id, parent_id, kind, name, position, match, external_id, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, '', 'local', ?, ?)
	`, node.ID, parent, string(kind), name, pos, match,
		now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		return nil, fmt.Errorf("insert node: %w", err)
	}
	if kind == model.KindSmart {
		if err := insertRules(tx, node.ID, rules.Rules); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	p.emitTreeChanged()
	return node, nil
}

// insertRules writes rule rows for a playlist in their source order. Caller
// is responsible for clearing any pre-existing rows first when replacing.
func insertRules(tx *sql.Tx, playlistID string, rules []model.SmartRule) error {
	for i, r := range rules {
		if _, ok := smartFields[r.Field]; !ok {
			return fmt.Errorf("unknown field %q", r.Field)
		}
		vText, vNum, vNum2, err := dehydrateRuleValue(r)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO playlist_rules (playlist_id, position, field, op, value_text, value_num, value_num2)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, playlistID, i, r.Field, r.Op, vText, vNum, vNum2); err != nil {
			return fmt.Errorf("insert rule: %w", err)
		}
	}
	return nil
}

// dehydrateRuleValue unpacks the Value interface{} into the three concrete
// columns we store. Anything unexpected returns an error — we'd rather fail
// fast at write time than store garbage.
func dehydrateRuleValue(r model.SmartRule) (vText string, vNum, vNum2 float64, err error) {
	if r.Op == "between" {
		arr, ok := r.Value.([]interface{})
		if !ok || len(arr) != 2 {
			return "", 0, 0, fmt.Errorf("between needs [low, high] for %s", r.Field)
		}
		lo, errLo := toFloat(arr[0])
		hi, errHi := toFloat(arr[1])
		if errLo != nil || errHi != nil {
			return "", 0, 0, fmt.Errorf("between bounds must be numeric for %s", r.Field)
		}
		return "", lo, hi, nil
	}
	if isNumericField(r.Field) {
		n, err := toFloat(r.Value)
		if err != nil {
			return "", 0, 0, fmt.Errorf("field %s needs numeric value", r.Field)
		}
		return "", n, 0, nil
	}
	return fmt.Sprint(r.Value), 0, 0, nil
}

func toFloat(v interface{}) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	}
	return 0, fmt.Errorf("not numeric: %T", v)
}

// Rename changes the display name of a node.
func (p *PlaylistService) Rename(id, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("playlist name cannot be empty")
	}
	_, err := p.store.db.Exec(`UPDATE playlist_nodes SET name = ?, updated_at = ? WHERE id = ?`,
		name, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	p.emitTreeChanged()
	return nil
}

// Delete removes a node and (through ON DELETE CASCADE) everything beneath
// it — subfolders, child playlists, their tracks, and their rules.
func (p *PlaylistService) Delete(id string) error {
	if _, err := p.store.db.Exec(`DELETE FROM playlist_nodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	p.emitTreeChanged()
	return nil
}

// Move relocates a node to a new parent at a new position. Siblings get
// renumbered inside a single transaction.
func (p *PlaylistService) Move(id, newParentID string, newPosition int) error {
	tx, err := p.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var parent interface{}
	if newParentID != "" {
		parent = newParentID
	}
	if _, err := tx.Exec(`UPDATE playlist_nodes SET parent_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		parent, newPosition*positionGap, time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return fmt.Errorf("move: %w", err)
	}
	if err := renumberNodes(tx, newParentID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.emitTreeChanged()
	return nil
}

// Tracks returns the tracks belonging to a playlist. For manual playlists
// the result honours the stored position order; for auto playlists it runs
// the rule set against the library.
func (p *PlaylistService) Tracks(playlistID string) ([]model.Track, error) {
	node, err := p.Node(playlistID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case model.KindManual:
		return p.manualTracks(playlistID)
	case model.KindSmart:
		return p.smartTracks(node.Rules)
	default:
		return nil, nil
	}
}

func (p *PlaylistService) manualTracks(playlistID string) ([]model.Track, error) {
	rows, err := p.store.db.Query(`
		SELECT `+trackCols+`
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		LEFT JOIN track_analysis a ON a.track_id = t.id
		WHERE pt.playlist_id = ?
		ORDER BY pt.position ASC
	`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("manual tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// AddTracks appends tracks to a manual playlist in the given order. Tracks
// already present are silently skipped — a track cannot appear twice in
// the same playlist.
func (p *PlaylistService) AddTracks(playlistID string, trackIDs []string) error {
	if len(trackIDs) == 0 {
		return nil
	}
	tx, err := p.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxPos sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(position) FROM playlist_tracks WHERE playlist_id = ?`, playlistID).Scan(&maxPos); err != nil {
		return fmt.Errorf("max position: %w", err)
	}
	pos := int64(0)
	if maxPos.Valid {
		pos = maxPos.Int64 + positionGap
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, tid := range trackIDs {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO playlist_tracks (playlist_id, track_id, position, added_at)
			VALUES (?, ?, ?, ?)
		`, playlistID, tid, pos, now); err != nil {
			return fmt.Errorf("insert track: %w", err)
		}
		pos += positionGap
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.emitTracksChanged(playlistID)
	return nil
}

// RemoveTracks deletes the given tracks from a manual playlist.
func (p *PlaylistService) RemoveTracks(playlistID string, trackIDs []string) error {
	if len(trackIDs) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(trackIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, 0, len(trackIDs)+1)
	args = append(args, playlistID)
	for _, tid := range trackIDs {
		args = append(args, tid)
	}
	_, err := p.store.db.Exec(
		`DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("remove tracks: %w", err)
	}
	p.emitTracksChanged(playlistID)
	return nil
}

// Reorder moves a single track to a new visual index within a manual
// playlist.
func (p *PlaylistService) Reorder(playlistID, trackID string, newIndex int) error {
	return p.ReorderMany(playlistID, []string{trackID}, newIndex)
}

// ReorderMany moves the given tracks (in the order supplied) so that the
// first one sits at newIndex. The runs are always contiguous after the
// operation. This is the primitive behind drag-and-drop of a multi-selection.
func (p *PlaylistService) ReorderMany(playlistID string, trackIDs []string, newIndex int) error {
	if len(trackIDs) == 0 {
		return nil
	}
	tx, err := p.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT track_id FROM playlist_tracks WHERE playlist_id = ? ORDER BY position ASC`, playlistID)
	if err != nil {
		return fmt.Errorf("load order: %w", err)
	}
	var order []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			rows.Close()
			return err
		}
		order = append(order, tid)
	}
	rows.Close()

	moving := make(map[string]bool, len(trackIDs))
	for _, tid := range trackIDs {
		moving[tid] = true
	}
	remaining := order[:0:len(order)]
	for _, tid := range order {
		if !moving[tid] {
			remaining = append(remaining, tid)
		}
	}

	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex > len(remaining) {
		newIndex = len(remaining)
	}

	final := make([]string, 0, len(order))
	final = append(final, remaining[:newIndex]...)
	final = append(final, trackIDs...)
	final = append(final, remaining[newIndex:]...)

	for i, tid := range final {
		if _, err := tx.Exec(`UPDATE playlist_tracks SET position = ? WHERE playlist_id = ? AND track_id = ?`,
			int64(i)*positionGap, playlistID, tid); err != nil {
			return fmt.Errorf("update position: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	p.emitTracksChanged(playlistID)
	return nil
}

// UpdateRules replaces the rule set for an auto playlist and emits the
// invalidation event so open views refetch. The write is one transaction:
// update match on the node, wipe old rule rows, insert new ones.
func (p *PlaylistService) UpdateRules(playlistID string, rules model.SmartRules) error {
	match := rules.Match
	if match == "" {
		match = "all"
	}
	tx, err := p.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE playlist_nodes SET match = ?, updated_at = ? WHERE id = ? AND kind = 'smart'`,
		match, time.Now().UTC().Format(time.RFC3339), playlistID); err != nil {
		return fmt.Errorf("update match: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM playlist_rules WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("clear rules: %w", err)
	}
	if err := insertRules(tx, playlistID, rules.Rules); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.emitInvalidated(playlistID)
	return nil
}

// nextPosition finds the next free gap-based position under a parent.
func (p *PlaylistService) nextPosition(parentID string) (int, error) {
	var maxPos sql.NullInt64
	var row *sql.Row
	if parentID == "" {
		row = p.store.db.QueryRow(`SELECT MAX(position) FROM playlist_nodes WHERE parent_id IS NULL`)
	} else {
		row = p.store.db.QueryRow(`SELECT MAX(position) FROM playlist_nodes WHERE parent_id = ?`, parentID)
	}
	if err := row.Scan(&maxPos); err != nil {
		return 0, fmt.Errorf("max position: %w", err)
	}
	if !maxPos.Valid {
		return 0, nil
	}
	return int(maxPos.Int64) + positionGap, nil
}

func renumberNodes(tx *sql.Tx, parentID string) error {
	var rows *sql.Rows
	var err error
	if parentID == "" {
		rows, err = tx.Query(`SELECT id FROM playlist_nodes WHERE parent_id IS NULL ORDER BY position ASC, name ASC`)
	} else {
		rows, err = tx.Query(`SELECT id FROM playlist_nodes WHERE parent_id = ? ORDER BY position ASC, name ASC`, parentID)
	}
	if err != nil {
		return fmt.Errorf("load siblings: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE playlist_nodes SET position = ? WHERE id = ?`, i*positionGap, id); err != nil {
			return fmt.Errorf("renumber: %w", err)
		}
	}
	return nil
}

// smartTracks turns a rule set into a parameterised SELECT and executes it.
// Field names are strictly whitelisted to keep this injection-safe.
func (p *PlaylistService) smartTracks(rules model.SmartRules) ([]model.Track, error) {
	where, args, err := buildSmartWhere(rules)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + trackCols + `
		FROM tracks t
		LEFT JOIN track_analysis a ON a.track_id = t.id`
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY t.artist ASC, t.title ASC LIMIT 2000"
	rows, err := p.store.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("auto tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// smartFields maps a public field name to the SQL column expression. Anything
// not in this map is rejected — this is the injection boundary.
var smartFields = map[string]string{
	"title":  "t.title",
	"artist": "t.artist",
	"album":  "t.album",
	"genre":  "t.genre",
	"bpm":    "COALESCE(a.bpm, 0)",
	"key":    "COALESCE(a.key, '')",
	"gain":   "COALESCE(a.gain, 0)",
}

// analysisDependentFields are those whose values can change when a track is
// (re)analyzed. Auto playlists that reference any of them get invalidated
// when ActionAnalyzeComplete fires.
var analysisDependentFields = []string{"bpm", "key", "gain"}

func buildSmartWhere(rules model.SmartRules) (string, []interface{}, error) {
	if len(rules.Rules) == 0 {
		return "", nil, nil
	}
	glue := " AND "
	if strings.EqualFold(rules.Match, "any") {
		glue = " OR "
	}
	parts := make([]string, 0, len(rules.Rules))
	args := make([]interface{}, 0, len(rules.Rules))
	for _, r := range rules.Rules {
		col, ok := smartFields[r.Field]
		if !ok {
			return "", nil, fmt.Errorf("unknown field %q", r.Field)
		}
		switch r.Op {
		case "eq":
			parts = append(parts, col+" = ?")
			args = append(args, r.Value)
		case "neq":
			parts = append(parts, col+" != ?")
			args = append(args, r.Value)
		case "contains":
			parts = append(parts, col+" LIKE ?")
			args = append(args, "%"+fmt.Sprint(r.Value)+"%")
		case "gt":
			parts = append(parts, col+" > ?")
			args = append(args, r.Value)
		case "lt":
			parts = append(parts, col+" < ?")
			args = append(args, r.Value)
		case "between":
			arr, ok := r.Value.([]interface{})
			if !ok || len(arr) != 2 {
				return "", nil, fmt.Errorf("between needs [low, high] for %s", r.Field)
			}
			parts = append(parts, col+" BETWEEN ? AND ?")
			args = append(args, arr[0], arr[1])
		default:
			return "", nil, fmt.Errorf("unknown op %q", r.Op)
		}
	}
	return "(" + strings.Join(parts, glue) + ")", args, nil
}

// onAnalysis fires every time an analysis completes. A single indexed
// SELECT over playlist_rules gives us the set of auto playlists that
// reference bpm/key/gain so we can invalidate exactly those.
func (p *PlaylistService) onAnalysis(ev event.Event) error {
	if ev.Action != event.ActionAnalyzeComplete {
		return nil
	}
	placeholders := strings.Repeat("?,", len(analysisDependentFields))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(analysisDependentFields))
	for i, f := range analysisDependentFields {
		args[i] = f
	}
	rows, err := p.store.db.Query(
		`SELECT DISTINCT playlist_id FROM playlist_rules WHERE field IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		p.emitInvalidated(id)
	}
	return nil
}

func (p *PlaylistService) emitTreeChanged() {
	if p.bus == nil {
		return
	}
	p.bus.PublishAsync(event.Event{Topic: event.TopicPlaylist, Action: event.ActionPlaylistTreeChanged})
}

func (p *PlaylistService) emitTracksChanged(playlistID string) {
	if p.bus == nil {
		return
	}
	p.bus.PublishAsync(event.Event{Topic: event.TopicPlaylist, Action: event.ActionPlaylistTracksChanged, Payload: playlistID})
}

func (p *PlaylistService) emitInvalidated(playlistID string) {
	if p.bus == nil {
		return
	}
	p.bus.PublishAsync(event.Event{Topic: event.TopicPlaylist, Action: event.ActionPlaylistInvalidated, Payload: playlistID})
}
