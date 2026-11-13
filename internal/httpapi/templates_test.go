package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/yppp/ponytimer/internal/store"
)

// templateLevels is the structure of the templates of the tests.
const templateLevels = `[
	{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":300},
	{"type":"level","format":"blind-nl","bets":{"sb":200,"bb":400},"duration_sec":300}
]`

// createTemplate stores a template and returns it.
func (e *testEnv) createTemplate(t *testing.T, body string) store.Template {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/templates", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create template status %d: %s", w.Code, w.Body)
	}
	var tpl store.Template
	if err := json.Unmarshal(w.Body.Bytes(), &tpl); err != nil {
		t.Fatal(err)
	}
	return tpl
}

// TestTemplateRoundTrip walks through the whole life of a template: it is
// created, listed, read, changed and deleted.
func TestTemplateRoundTrip(t *testing.T) {
	e := testServer(t)

	created := e.createTemplate(t, `{"name":"Deep","levels":`+templateLevels+`}`)
	if created.ID == 0 {
		t.Fatal("the created template has no id")
	}
	if created.Name != "Deep" || len(created.Levels) != 2 {
		t.Fatalf("created template = %+v", created)
	}
	if created.Levels[0].Bets.BB != 200 || created.Levels[1].DurationSec != 300 {
		t.Fatalf("created levels = %+v", created.Levels)
	}

	// The list holds the template.
	list := e.request(t, http.MethodGet, "/api/templates", "", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", list.Code, list.Body)
	}
	var templates []store.Template
	if err := json.Unmarshal(list.Body.Bytes(), &templates); err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 || templates[0].ID != created.ID {
		t.Fatalf("templates = %+v, want the created one", templates)
	}

	// The template is read on its own.
	one := e.request(t, http.MethodGet, "/api/templates/"+strconv.FormatInt(created.ID, 10), "", nil)
	if one.Code != http.StatusOK {
		t.Fatalf("get status %d: %s", one.Code, one.Body)
	}
	var got store.Template
	if err := json.Unmarshal(one.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Deep" || len(got.Levels) != 2 {
		t.Fatalf("template = %+v", got)
	}

	// The template is changed.
	updated := `{"name":"Deeper","levels":[{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000},"duration_sec":600}]}`
	w := e.request(t, http.MethodPut, "/api/templates/"+strconv.FormatInt(created.ID, 10), updated, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", w.Code, w.Body)
	}
	var changed store.Template
	if err := json.Unmarshal(w.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.Name != "Deeper" || len(changed.Levels) != 1 {
		t.Fatalf("changed template = %+v", changed)
	}
	if changed.Levels[0].Bets.BB != 1000 {
		t.Fatalf("changed levels = %+v", changed.Levels)
	}

	// The template is deleted.
	del := e.request(t, http.MethodDelete, "/api/templates/"+strconv.FormatInt(created.ID, 10), "", nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status %d, want 204: %s", del.Code, del.Body)
	}
	gone := e.request(t, http.MethodGet, "/api/templates/"+strconv.FormatInt(created.ID, 10), "", nil)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("get status %d, want 404: %s", gone.Code, gone.Body)
	}
}

// TestTemplateRejectsAnEmptyNameOrStructure checks the validation of the
// template routes.
func TestTemplateRejectsAnEmptyNameOrStructure(t *testing.T) {
	e := testServer(t)

	w := e.request(t, http.MethodPost, "/api/templates", `{"name":"","levels":`+templateLevels+`}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create status %d, want 400: %s", w.Code, w.Body)
	}

	w = e.request(t, http.MethodPost, "/api/templates", `{"name":"Empty","levels":[]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create status %d, want 400: %s", w.Code, w.Body)
	}
}

// TestTemplateOfUnknownID checks the answer for a template that does not
// exist.
func TestTemplateOfUnknownID(t *testing.T) {
	e := testServer(t)

	w := e.request(t, http.MethodGet, "/api/templates/999", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get status %d, want 404: %s", w.Code, w.Body)
	}

	w = e.request(t, http.MethodPut, "/api/templates/999", `{"name":"X","levels":`+templateLevels+`}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("update status %d, want 404: %s", w.Code, w.Body)
	}

	w = e.request(t, http.MethodDelete, "/api/templates/999", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete status %d, want 404: %s", w.Code, w.Body)
	}
}
