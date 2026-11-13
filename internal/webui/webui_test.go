package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDisplayAssets checks that the embedded display page still carries
// the pieces the display relies on.
//
// The display page is static HTML, CSS and JavaScript: there is no
// browser in the test suite, so this test pins down the parts that a
// refactoring could silently drop (the level, the statistics, the
// fullscreen control and the portrait layout).
func TestDisplayAssets(t *testing.T) {
	page := renderDisplayPage(t)
	for _, want := range []string{
		`id="slots"`,
		`id="fullscreen"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("display page does not contain %q", want)
		}
	}

	script := readAsset(t, "static/display.js")
	for _, want := range []string{
		"LEVEL ",
		"BIG BET",
		"BRING-IN",
		"COMPLETE",
		"PLAYERS",
		"ENTRIES",
		"AVG STACK",
		"requestFullscreen",
		"exitFullscreen",
		"fullscreenchange",
		"wakeLock",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("display.js does not contain %q", want)
		}
	}

	style := readAsset(t, "static/style.css")
	for _, want := range []string{
		".slot-level",
		".stat-label",
		".fullscreen-btn",
		"orientation: portrait",
	} {
		if !strings.Contains(style, want) {
			t.Errorf("style.css does not contain %q", want)
		}
	}
	portraitAt := strings.Index(style, "@media (orientation: portrait)")
	if portraitAt < 0 {
		t.Fatal("style.css does not contain the portrait media section")
	}
	fourSlotAt := strings.Index(style[portraitAt:], `.slots[data-slots="4"] {`)
	if fourSlotAt < 0 {
		t.Fatal("portrait CSS does not contain a four-slot grid rule")
	}
	fourSlotRule := style[portraitAt+fourSlotAt:]
	ruleEnd := strings.Index(fourSlotRule, "}")
	if ruleEnd < 0 || !strings.Contains(fourSlotRule[:ruleEnd], "grid-template-columns: 1fr;") {
		t.Fatal("portrait four-slot grid does not specify one column")
	}
}

func TestEditorAssets(t *testing.T) {
	page := renderEditorPage(t)
	for _, want := range []string{`id="target-label"`, `id="level-rows"`, `id="draft-discard"`, `id="issue-list"`} {
		if !strings.Contains(page, want) {
			t.Errorf("editor page does not contain %q", want)
		}
	}

	rec := httptest.NewRecorder()
	StaticHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/editor.js", nil))
	if rec.Code != 200 {
		t.Fatalf("editor.js status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ponytimer-editor-draft:") {
		t.Fatal("editor.js response does not contain editor script")
	}
}

func renderEditorPage(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	RenderEditor(rec, httptest.NewRequest("GET", "/admin/editor", nil))
	if rec.Code != 200 {
		t.Fatalf("editor page status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// renderDisplayPage renders the display page and returns its body.
func renderDisplayPage(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	RenderDisplay(rec, httptest.NewRequest("GET", "/display", nil))
	if rec.Code != 200 {
		t.Fatalf("display page status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// readAsset reads one of the embedded static files.
func readAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := content.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
