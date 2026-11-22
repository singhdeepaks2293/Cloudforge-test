package advanced

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// HTTP handlers for the Advanced section. They intentionally take
// the authenticated user id as an argument instead of reading auth
// themselves: the router (backend/internal/httpapi) owns auth and
// passes s.requireAuth's user through, exactly as the existing
// curriculum handlers do. See docs/advanced/ROUTES.md for the
// wiring snippet.
//
// Error bodies use the same {"error": {"code", "message"}} envelope
// as the rest of the API; the request id is added by the router's
// middleware, so it is not repeated here.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}

// HandleProjects serves GET /api/v1/advanced/projects: the catalogue.
func (s *Service) HandleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": Projects})
}

// HandleInterviews serves GET /api/v1/advanced/interviews.
func (s *Service) HandleInterviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}
	interviews, err := s.ListInterviews()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "advanced_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interviews": interviews})
}

// HandleSkillsChecklist serves GET /api/v1/advanced/skills-checklist:
// the JD archetypes and their skill mappings.
func (s *Service) HandleSkillsChecklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}
	archetypes, err := s.ListArchetypes()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "advanced_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"archetypes": archetypes})
}

// HandleChecklistState serves GET and PUT
// /api/v1/advanced/checklist-state: the user's checked skills.
// GET returns {"checked": {skillID: true}}; PUT accepts the same
// shape and returns the stored state. State is in-memory per
// process (see Service); a PUT never claims durability it does not
// have.
func (s *Service) HandleChecklistState(w http.ResponseWriter, r *http.Request, userID string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"checked": s.Checklist(userID)})
	case http.MethodPut:
		var body struct {
			Checked map[string]bool `json:"checked"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
			return
		}
		if body.Checked == nil {
			body.Checked = map[string]bool{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"checked": s.SetChecklist(userID, body.Checked)})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PUT are allowed")
	}
}

// HandlePortfolio serves POST /api/v1/advanced/portfolio/{project}:
// it generates the portfolio artefact for a completed Advanced
// project and returns it as a Markdown download. A project that is
// not completed returns 409 with code project_not_completed —
// never an artefact for work that did not happen.
func (s *Service) HandlePortfolio(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is allowed")
		return
	}
	projectID := strings.TrimPrefix(r.URL.Path, "/api/v1/advanced/portfolio/")
	projectID = strings.Trim(projectID, "/")
	if projectID == "" || strings.Contains(projectID, "/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "project id is required")
		return
	}
	filename, md, err := s.GeneratePortfolio(r.Context(), userID, projectID)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownProject):
			writeError(w, http.StatusNotFound, "project_not_found", "no advanced project with that id")
		case errors.Is(err, ErrProjectNotCompleted):
			writeError(w, http.StatusConflict, "project_not_completed", "complete the project track before generating its portfolio artefact")
		default:
			writeError(w, http.StatusServiceUnavailable, "advanced_unavailable", err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}
