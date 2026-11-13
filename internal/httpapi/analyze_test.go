package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestAnalyzeStructureReportsTheCleanPairIssue checks the analyze endpoint
// with the B4 pattern of appendix B: the 700 / 1400 row is not a clean pair
// and is reported with a warning.
func TestAnalyzeStructureReportsTheCleanPairIssue(t *testing.T) {
	e := testServer(t)

	body := `{
		"levels": [
			{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000,"ante":1000},"duration_sec":7200},
			{"type":"level","format":"blind-nl","bets":{"sb":700,"bb":1400,"ante":1400},"duration_sec":7200},
			{"type":"level","format":"blind-nl","bets":{"sb":1000,"bb":2000,"ante":2000},"duration_sec":7200}
		],
		"entries": 100,
		"starting_stack": 10000
	}`
	w := e.request(t, http.MethodPost, "/api/structures/analyze", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze status %d: %s", w.Code, w.Body)
	}
	var result analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	// The row 1 is not on the master ladder.
	found := false
	for _, is := range result.Issues {
		if is.Row == 1 && is.Rule == "not_clean_value" {
			found = true
			if is.Severity != "warn" {
				t.Errorf("severity = %q, want warn", is.Severity)
			}
			if is.Suggestion != "use SB 1000 / BB 1500" {
				t.Errorf("suggestion = %q, want the clean pair", is.Suggestion)
			}
		}
	}
	if !found {
		t.Fatalf("no not_clean_value issue on row 1: %+v", result.Issues)
	}

	// Three levels of 120 minutes.
	if result.Simulation.TotalMin != 360 {
		t.Errorf("total minutes = %d, want 360", result.Simulation.TotalMin)
	}
	if result.Simulation.TotalChips != 1000000 {
		t.Errorf("total chips = %d, want 1000000", result.Simulation.TotalChips)
	}
	if len(result.Simulation.Rows) != 3 {
		t.Errorf("simulation rows = %d, want 3", len(result.Simulation.Rows))
	}
	if len(result.Suggestions) == 0 {
		t.Error("no suggestions for the next level")
	}
}

// TestAnalyzeStructureFillsTheAbsoluteTimes checks that the absolute times
// are only filled in when the start of the tournament is known.
func TestAnalyzeStructureFillsTheAbsoluteTimes(t *testing.T) {
	e := testServer(t)

	body := `{
		"levels": [
			{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000},"duration_sec":3600,"markers":["late_reg_end"]},
			{"type":"level","format":"blind-nl","bets":{"sb":1000,"bb":2000},"duration_sec":3600}
		],
		"entries": 100,
		"starting_stack": 10000,
		"start_time_ms": 1800000000000
	}`
	w := e.request(t, http.MethodPost, "/api/structures/analyze", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze status %d: %s", w.Code, w.Body)
	}
	var result analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	const start = int64(1800000000000)
	if want := start + 120*60000; result.EndTimeMs != want {
		t.Errorf("end time = %d, want %d", result.EndTimeMs, want)
	}
	if want := start + 60*60000; result.LateRegCloseTimeMs != want {
		t.Errorf("late registration close = %d, want %d", result.LateRegCloseTimeMs, want)
	}
	// The big blind of 2000 is below a twentieth of the total chips, so the
	// expected end is not reached.
	if result.EndLevelTimeMs != 0 {
		t.Errorf("end level time = %d, want 0", result.EndLevelTimeMs)
	}

	// Without a start time the absolute times stay empty.
	noStart := `{"levels":[{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000},"duration_sec":3600}]}`
	w = e.request(t, http.MethodPost, "/api/structures/analyze", noStart, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze status %d: %s", w.Code, w.Body)
	}
	result = analyzeResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.EndTimeMs != 0 || result.LateRegCloseTimeMs != 0 || result.EndLevelTimeMs != 0 {
		t.Errorf("absolute times = %d/%d/%d, want zeros",
			result.EndTimeMs, result.LateRegCloseTimeMs, result.EndLevelTimeMs)
	}
}

// TestAnalyzeStructureOfABrokenStructure checks that a structure that does
// not validate is answered with 200 and the issues.
func TestAnalyzeStructureOfABrokenStructure(t *testing.T) {
	e := testServer(t)

	body := `{"levels":[{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":0}]}`
	w := e.request(t, http.MethodPost, "/api/structures/analyze", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze status %d, want 200: %s", w.Code, w.Body)
	}
	var result analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, is := range result.Issues {
		if is.Rule == "bad_duration" && is.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no bad_duration issue: %+v", result.Issues)
	}
}

// TestAnalyzeStructureRejectsABrokenBody checks the answer for a body that
// cannot be read.
func TestAnalyzeStructureRejectsABrokenBody(t *testing.T) {
	e := testServer(t)

	w := e.request(t, http.MethodPost, "/api/structures/analyze", `{`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("analyze status %d, want 400: %s", w.Code, w.Body)
	}
}
