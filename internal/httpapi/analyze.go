package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/yppp/ponytimer/internal/core"
)

// msPerMinute converts the minutes of the simulation into milliseconds.
const msPerMinute = 60_000

// analyzeRequest is the body of POST /api/structures/analyze.
type analyzeRequest struct {
	// Levels is the structure to check.
	Levels []core.Level `json:"levels"`
	// Entries is the expected number of entries.
	Entries int `json:"entries"`
	// StartingStack is the stack handed out per entry (chips).
	StartingStack int `json:"starting_stack"`
	// StartTimeMs is the absolute start of the tournament in unix
	// milliseconds. When it is zero the absolute times are left out.
	StartTimeMs int64 `json:"start_time_ms"`
	// ChipUnit is the smallest chip unit of the tournament. Zero means the
	// default (25).
	ChipUnit int `json:"chip_unit"`
	// SuggestAfterIndex is the row the next level suggestions are built
	// from. When it is absent the suggestions are built from the last row.
	SuggestAfterIndex *int `json:"suggest_after_index"`
}

// analyzeIssue is a validation issue in its wire form: the severity is a
// name and not a number.
type analyzeIssue struct {
	// Row is the index of the row the issue belongs to, -1 for the
	// structure as a whole.
	Row int `json:"row"`
	// Severity is "error", "warn" or "info".
	Severity string `json:"severity"`
	// Rule is the machine readable code of the issue.
	Rule string `json:"rule"`
	// Message explains the issue.
	Message string `json:"message"`
	// Suggestion holds the proposed value, when there is one.
	Suggestion string `json:"suggestion,omitempty"`
}

// analyzeResponse is the answer of POST /api/structures/analyze.
type analyzeResponse struct {
	// Issues are the findings of the validation.
	Issues []analyzeIssue `json:"issues"`
	// Simulation is the pressure simulation of the structure.
	Simulation core.Simulation `json:"simulation"`
	// EndTimeMs is the expected end of the tournament.
	EndTimeMs int64 `json:"end_time_ms"`
	// LateRegCloseTimeMs is the end of the level that closes the late
	// registration. It is zero when there is no such level.
	LateRegCloseTimeMs int64 `json:"late_reg_close_time_ms"`
	// EndLevelTimeMs is the start of the level where the tournament is
	// expected to end (R13). It is zero when that level is not reached.
	EndLevelTimeMs int64 `json:"end_level_time_ms"`
	// Suggestions are the proposed forced bets of the next level.
	Suggestions []core.BetSuggestion `json:"suggestions"`
}

// handleAnalyzeStructure validates and simulates a structure.
//
// The editor calls it on every change, so a structure that does not validate
// is answered with 200 and the issues, not with an error. Only a body that
// cannot be read is a 400.
func (s *Server) handleAnalyzeStructure(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	issues := core.ValidateStructure(req.Levels, core.ValidateOptions{ChipUnit: req.ChipUnit})
	out := analyzeResponse{Issues: make([]analyzeIssue, 0, len(issues))}
	for _, is := range issues {
		out.Issues = append(out.Issues, analyzeIssue{
			Row:        is.Row,
			Severity:   is.Severity.String(),
			Rule:       is.Rule,
			Message:    is.Message,
			Suggestion: is.Suggestion,
		})
	}

	sim := core.Simulate(req.Levels, req.Entries, req.StartingStack)
	out.Simulation = sim

	// The absolute times are only filled in when the start of the tournament
	// is known.
	if req.StartTimeMs > 0 {
		out.EndTimeMs = req.StartTimeMs + int64(sim.TotalMin)*msPerMinute
		if sim.LateRegLevel > 0 {
			out.LateRegCloseTimeMs = req.StartTimeMs + int64(sim.LateRegCloseMin)*msPerMinute
		}
		if sim.EndLevelIndex >= 0 {
			out.EndLevelTimeMs = req.StartTimeMs + int64(sim.EndLevelStartMin)*msPerMinute
		}
	}

	// Without an index the suggestions are built from the last row.
	afterIndex := -1
	if req.SuggestAfterIndex != nil {
		afterIndex = *req.SuggestAfterIndex
	}
	suggestions := core.NextBetSuggestions(req.Levels, afterIndex)
	if suggestions == nil {
		suggestions = []core.BetSuggestion{}
	}
	out.Suggestions = suggestions

	writeJSON(w, http.StatusOK, out)
}
