package store

import (
	"reflect"
	"testing"

	"github.com/yppp/ponytimer/internal/core"
)

// templateLevels is the structure of the templates of the tests.
func templateLevels() []core.Level {
	return []core.Level{
		{Type: core.LevelTypeLevel, Format: core.FormatBlindNL, Bets: core.Bets{SB: 100, BB: 200}, DurationSec: 300},
		{Type: core.LevelTypeBreak, DurationSec: 600},
		{
			Type:        core.LevelTypeLevel,
			Format:      core.FormatBlindNL,
			Bets:        core.Bets{SB: 200, BB: 400},
			DurationSec: 300,
			Markers:     []string{core.MarkerColorUp},
		},
	}
}

// TestTemplateRoundTrip walks through the whole life of a template.
func TestTemplateRoundTrip(t *testing.T) {
	st := openTestStore(t)
	levels := templateLevels()

	created, err := st.CreateTemplate("Deep", levels)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("the created template has no id")
	}
	if created.Name != "Deep" {
		t.Fatalf("name = %q, want Deep", created.Name)
	}
	if created.CreatedAtMs == 0 || created.UpdatedAtMs != created.CreatedAtMs {
		t.Fatalf("timestamps = %d/%d", created.CreatedAtMs, created.UpdatedAtMs)
	}

	got, err := st.GetTemplate(created.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Name != "Deep" {
		t.Fatalf("name = %q, want Deep", got.Name)
	}
	if !reflect.DeepEqual(got.Levels, levels) {
		t.Fatalf("levels = %+v, want %+v", got.Levels, levels)
	}

	templates, err := st.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("template count = %d, want 1", len(templates))
	}
	if templates[0].ID != created.ID {
		t.Fatalf("template id = %d, want %d", templates[0].ID, created.ID)
	}
	if !reflect.DeepEqual(templates[0].Levels, levels) {
		t.Fatalf("levels = %+v, want %+v", templates[0].Levels, levels)
	}

	// The template is changed.
	newLevels := []core.Level{
		{Type: core.LevelTypeLevel, Format: core.FormatBlindNL, Bets: core.Bets{SB: 500, BB: 1000}, DurationSec: 600},
	}
	updated, err := st.UpdateTemplate(created.ID, "Deeper", newLevels)
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Name != "Deeper" {
		t.Fatalf("name = %q, want Deeper", updated.Name)
	}
	if !reflect.DeepEqual(updated.Levels, newLevels) {
		t.Fatalf("levels = %+v, want %+v", updated.Levels, newLevels)
	}
	if updated.CreatedAtMs != created.CreatedAtMs {
		t.Fatalf("creation time = %d, want %d", updated.CreatedAtMs, created.CreatedAtMs)
	}

	// The template is deleted.
	if err := st.DeleteTemplate(created.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := st.GetTemplate(created.ID); err != ErrNotFound {
		t.Fatalf("GetTemplate after delete = %v, want ErrNotFound", err)
	}
	templates, err = st.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Fatalf("template count = %d, want 0", len(templates))
	}
}

// TestTemplateOfUnknownID checks the errors of the unknown templates.
func TestTemplateOfUnknownID(t *testing.T) {
	st := openTestStore(t)

	if _, err := st.GetTemplate(42); err != ErrNotFound {
		t.Fatalf("GetTemplate = %v, want ErrNotFound", err)
	}
	if _, err := st.UpdateTemplate(42, "X", templateLevels()); err != ErrNotFound {
		t.Fatalf("UpdateTemplate = %v, want ErrNotFound", err)
	}
	if err := st.DeleteTemplate(42); err != ErrNotFound {
		t.Fatalf("DeleteTemplate = %v, want ErrNotFound", err)
	}
}
