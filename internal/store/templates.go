package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/yppp/ponytimer/internal/core"
)

// Template is a reusable structure (spec §11): a name and a level list.
//
// The templates live outside the tournaments: they are meant to be applied to
// a tournament that does not exist yet.
type Template struct {
	// ID is the identifier of the template.
	ID int64 `json:"id"`
	// Name is the name the template is listed under.
	Name string `json:"name"`
	// Levels is the ordered structure of the template.
	Levels []core.Level `json:"levels"`
	// CreatedAtMs is the creation time in unix milliseconds.
	CreatedAtMs int64 `json:"created_at_ms"`
	// UpdatedAtMs is the time of the last change in unix milliseconds.
	UpdatedAtMs int64 `json:"updated_at_ms"`
}

// CreateTemplate stores a new template and returns it with its identifier.
//
// The levels are stored as they are: the index of a row is the position it
// has in the template.
func (s *Store) CreateTemplate(name string, levels []core.Level) (Template, error) {
	blob, err := marshalLevels(levels)
	if err != nil {
		return Template{}, err
	}
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`
		INSERT INTO templates (name, levels_json, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, name, blob, now, now)
	if err != nil {
		return Template{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Template{}, err
	}
	return Template{
		ID:          id,
		Name:        name,
		Levels:      levels,
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}, nil
}

// ListTemplates returns every template, newest first.
func (s *Store) ListTemplates() ([]Template, error) {
	rows, err := s.db.Query(`
		SELECT id, name, levels_json, created_at, updated_at
		FROM templates ORDER BY created_at DESC, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Template{}
	for rows.Next() {
		var t Template
		var levelsJSON string
		if err := rows.Scan(&t.ID, &t.Name, &levelsJSON, &t.CreatedAtMs, &t.UpdatedAtMs); err != nil {
			return nil, err
		}
		levels, err := unmarshalLevels(levelsJSON)
		if err != nil {
			return nil, err
		}
		t.Levels = levels
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTemplate returns one template.
//
// It returns ErrNotFound when no template has that identifier.
func (s *Store) GetTemplate(id int64) (Template, error) {
	var t Template
	var levelsJSON string
	err := s.db.QueryRow(`
		SELECT id, name, levels_json, created_at, updated_at
		FROM templates WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &levelsJSON, &t.CreatedAtMs, &t.UpdatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	if err != nil {
		return Template{}, err
	}
	levels, err := unmarshalLevels(levelsJSON)
	if err != nil {
		return Template{}, err
	}
	t.Levels = levels
	return t, nil
}

// UpdateTemplate replaces the name and the levels of a template.
//
// It returns ErrNotFound when no template has that identifier.
func (s *Store) UpdateTemplate(id int64, name string, levels []core.Level) (Template, error) {
	blob, err := marshalLevels(levels)
	if err != nil {
		return Template{}, err
	}
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`
		UPDATE templates SET name = ?, levels_json = ?, updated_at = ? WHERE id = ?
	`, name, blob, now, id)
	if err != nil {
		return Template{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Template{}, err
	}
	if affected == 0 {
		return Template{}, ErrNotFound
	}
	return s.GetTemplate(id)
}

// DeleteTemplate removes a template.
//
// It returns ErrNotFound when no template has that identifier.
func (s *Store) DeleteTemplate(id int64) error {
	res, err := s.db.Exec(`DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// marshalLevels encodes the levels of a template.
func marshalLevels(levels []core.Level) (string, error) {
	if levels == nil {
		levels = []core.Level{}
	}
	blob, err := json.Marshal(levels)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

// unmarshalLevels decodes the levels of a template.
func unmarshalLevels(blob string) ([]core.Level, error) {
	levels := []core.Level{}
	if err := json.Unmarshal([]byte(blob), &levels); err != nil {
		return nil, err
	}
	return levels, nil
}
