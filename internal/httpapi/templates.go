package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/store"
)

// templateRequest is the body of the template routes.
type templateRequest struct {
	// Name is the name the template is listed under.
	Name string `json:"name"`
	// Levels is the ordered structure of the template.
	Levels []core.Level `json:"levels"`
}

// handleCreateTemplate stores a new template.
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Levels) == 0 {
		writeError(w, http.StatusBadRequest, "levels are required")
		return
	}

	tpl, err := s.store.CreateTemplate(req.Name, req.Levels)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot create template: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tpl)
}

// handleListTemplates returns every template.
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.store.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list templates: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

// handleGetTemplate returns one template.
func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateID(w, r)
	if !ok {
		return
	}
	tpl, err := s.store.GetTemplate(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read template: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

// handleUpdateTemplate replaces the name and the levels of a template.
func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateID(w, r)
	if !ok {
		return
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Levels) == 0 {
		writeError(w, http.StatusBadRequest, "levels are required")
		return
	}

	tpl, err := s.store.UpdateTemplate(id, req.Name, req.Levels)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update template: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

// handleDeleteTemplate removes a template.
func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteTemplate(id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "template not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot delete template: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// templateID reads the template identifier of the path.
//
// It answers a 400 when the identifier is not a number and returns false, so
// that the caller stops.
func templateID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the template id must be a number")
		return 0, false
	}
	return id, true
}
