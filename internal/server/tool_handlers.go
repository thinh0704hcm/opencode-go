package server

import (
	"net/http"
)

type experimentalToolInfo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Mutating    bool           `json:"mutating"`
}

func (s *Server) handleExperimentalToolIDs(w http.ResponseWriter, r *http.Request) {
	tools := s.tools.List()
	ids := make([]string, 0, len(tools))
	for _, t := range tools {
		ids = append(ids, t.Name())
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) handleExperimentalTool(w http.ResponseWriter, r *http.Request) {
	tools := s.tools.List()
	out := make([]experimentalToolInfo, 0, len(tools))
	for _, t := range tools {
		schema := schemaForTool(t.Name())
		out = append(out, experimentalToolInfo{
			ID:          t.Name(),
			Name:        schema.Name,
			Description: schema.Description,
			Parameters:  schema.Parameters,
			Mutating:    t.Mutating(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
