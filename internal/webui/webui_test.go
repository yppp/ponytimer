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
