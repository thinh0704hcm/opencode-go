//go:build opencode_wip

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/opencode-go/opencode-go/internal/config"
)

const (
	maxMarketplaceBytes int64 = 1 << 20
	maxThemeStateBytes        = 4 << 10
	maxMarketplaceIDLen       = 64
)

var themeIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type marketplaceItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	BuiltIn     bool   `json:"builtin,omitempty"`
	Installed   bool   `json:"installed,omitempty"`
}

type marketplaceListResponse struct {
	Items    []marketplaceItem `json:"items"`
	Warnings []string          `json:"warnings,omitempty"`
	Status   string            `json:"status,omitempty"`
}

type themeStateResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

type tuiOpenThemesResponse struct {
	Opened   bool               `json:"opened"`
	Current  themeStateResponse `json:"current"`
	Items    []marketplaceItem  `json:"items"`
	Warnings []string           `json:"warnings,omitempty"`
	Status   string             `json:"status,omitempty"`
}

func builtInThemes() []marketplaceItem {
	return []marketplaceItem{
		{ID: "default", Name: "Default", Description: "Default theme", BuiltIn: true},
		{ID: "dark", Name: "Dark", Description: "Dark theme", BuiltIn: true},
		{ID: "light", Name: "Light", Description: "Light theme", BuiltIn: true},
		{ID: "high-contrast", Name: "High Contrast", Description: "High contrast theme", BuiltIn: true},
	}
}

func builtInPlugins() []marketplaceItem {
	return []marketplaceItem{
		{ID: "shell", Name: "Shell", Description: "Native shell execution", BuiltIn: true},
		{ID: "file", Name: "File", Description: "Native file read and write", BuiltIn: true},
		{ID: "find", Name: "Find", Description: "Native workspace search", BuiltIn: true},
		{ID: "vcs", Name: "VCS", Description: "Native version control helpers", BuiltIn: true},
		// lsp and formatter are not implemented in this pass; they are advertised only as catalog entries.
		{ID: "permission", Name: "Permission", Description: "Native permission prompts", BuiltIn: true},
		{ID: "delegate", Name: "Delegate", Description: "Native delegation tools", BuiltIn: true},
	}
}

func (s *Server) handleTUIOpenThemes(w http.ResponseWriter, r *http.Request) {
	items, warnings := s.themeCatalog(r)
	workdir := s.resolveWorkdirForRequest(r)
	id, source := currentThemeID(workdir, config.Load(workdir))
	writeJSON(w, http.StatusOK, tuiOpenThemesResponse{
		Opened:   true,
		Current:  themeStateResponse{ID: id, Name: themeName(id, items), Source: source},
		Items:    items,
		Warnings: warnings,
		Status:   marketplaceStatus(warnings),
	})
}

func (s *Server) handleMarketplacePlugins(w http.ResponseWriter, r *http.Request) {
	items, warnings := s.pluginCatalog(r)
	writeJSON(w, http.StatusOK, marketplaceListResponse{Items: items, Warnings: warnings, Status: marketplaceStatus(warnings)})
}

func (s *Server) handleMarketplaceThemes(w http.ResponseWriter, r *http.Request) {
	items, warnings := s.themeCatalog(r)
	writeJSON(w, http.StatusOK, marketplaceListResponse{Items: items, Warnings: warnings, Status: marketplaceStatus(warnings)})
}

func (s *Server) handleThemeGet(w http.ResponseWriter, r *http.Request) {
	workdir := s.resolveWorkdirForRequest(r)
	id, source := currentThemeID(workdir, config.Load(workdir))
	writeJSON(w, http.StatusOK, themeStateResponse{ID: id, Name: themeName(id, s.themeCatalogItems(r)), Source: source})
}

func (s *Server) handleThemeSelect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" || !themeIDRe.MatchString(req.ID) {
		writeError(w, http.StatusBadRequest, "invalid theme id")
		return
	}
	if !catalogHasID(s.themeCatalogItems(r), req.ID) {
		writeError(w, http.StatusBadRequest, "unknown theme id")
		return
	}
	workdir := s.resolveWorkdirForRequest(r)
	if err := writeThemeState(workdir, req.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "theme write failed")
		return
	}
	writeJSON(w, http.StatusOK, themeStateResponse{ID: req.ID, Name: themeName(req.ID, s.themeCatalogItems(r)), Source: "project"})
}

func (s *Server) pluginCatalog(r *http.Request) ([]marketplaceItem, []string) {
	workdir := s.resolveWorkdirForRequest(r)
	installed := installedPlugins(config.Load(workdir))
	items := builtInPlugins()
	for i := range items {
		items[i].Installed = installed[items[i].ID]
	}
	manifest, warnings := readMarketplaceManifest(workdir)
	installedItems := make([]marketplaceItem, 0, len(installed))
	for id := range installed {
		installedItems = append(installedItems, marketplaceItem{ID: id, Name: id, Installed: true})
	}
	items = appendCatalog(items, append(manifest.Plugins, installedItems...), installed)
	return sortedCatalog(items), warnings
}

func (s *Server) themeCatalog(r *http.Request) ([]marketplaceItem, []string) {
	items := s.themeCatalogItems(r)
	_, warnings := readMarketplaceManifest(s.resolveWorkdirForRequest(r))
	return items, warnings
}

func (s *Server) themeCatalogItems(r *http.Request) []marketplaceItem {
	workdir := s.resolveWorkdirForRequest(r)
	manifest, _ := readMarketplaceManifest(workdir)
	return sortedCatalog(appendCatalog(builtInThemes(), manifest.Themes, nil))
}

type marketplaceManifest struct {
	Plugins []marketplaceItem `json:"plugins"`
	Themes  []marketplaceItem `json:"themes"`
}

func readMarketplaceManifest(workdir string) (marketplaceManifest, []string) {
	path := os.Getenv("OPENCODE_MARKETPLACE_PATH")
	if path == "" && workdir != "" {
		path = filepath.Join(workdir, ".opencode", "marketplace.json")
	}
	if path == "" {
		return marketplaceManifest{}, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return marketplaceManifest{}, nil
	}
	if err != nil {
		return marketplaceManifest{}, []string{"marketplace manifest unreadable"}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return marketplaceManifest{}, []string{"marketplace manifest unreadable"}
	}
	if info.Size() > maxMarketplaceBytes {
		return marketplaceManifest{}, []string{"marketplace manifest too large"}
	}
	var manifest marketplaceManifest
	dec := json.NewDecoder(io.LimitReader(f, maxMarketplaceBytes+1))
	if err := dec.Decode(&manifest); err != nil {
		return marketplaceManifest{}, []string{"marketplace manifest malformed"}
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return marketplaceManifest{}, []string{"marketplace manifest malformed"}
	}
	warnings := sanitizeMarketplaceManifest(&manifest)
	return manifest, warnings
}

func sanitizeMarketplaceManifest(manifest *marketplaceManifest) []string {
	warnings := sanitizeMarketplaceItems(&manifest.Plugins)
	if sanitizeMarketplaceItems(&manifest.Themes) {
		warnings = true
	}
	if warnings {
		return []string{"marketplace manifest contained invalid ids"}
	}
	return nil
}

func sanitizeMarketplaceItems(items *[]marketplaceItem) bool {
	out := (*items)[:0]
	invalid := false
	for _, item := range *items {
		item.ID = strings.TrimSpace(item.ID)
		if !validMarketplaceID(item.ID) {
			invalid = true
			continue
		}
		out = append(out, item)
	}
	*items = out
	return invalid
}

func appendCatalog(base, extra []marketplaceItem, installed map[string]bool) []marketplaceItem {
	seen := map[string]bool{}
	out := make([]marketplaceItem, 0, len(base)+len(extra))
	for _, item := range append(base, extra...) {
		item.ID = strings.TrimSpace(item.ID)
		if !validMarketplaceID(item.ID) || seen[item.ID] {
			continue
		}
		if item.Name == "" {
			item.Name = item.ID
		}
		if installed != nil {
			item.Installed = item.Installed || installed[item.ID]
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out
}

func sortedCatalog(items []marketplaceItem) []marketplaceItem {
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func installedPlugins(cfg *config.Config) map[string]bool {
	out := map[string]bool{}
	plugins, _ := cfg.Raw["plugin"].([]any)
	for _, plugin := range plugins {
		switch v := plugin.(type) {
		case string:
			if v != "" {
				out[v] = true
			}
		case map[string]any:
			for _, key := range []string{"id", "name", "package"} {
				if id, ok := v[key].(string); ok && id != "" {
					out[id] = true
					break
				}
			}
		}
	}
	return out
}

func currentThemeID(workdir string, cfg *config.Config) (string, string) {
	if id := strings.TrimSpace(os.Getenv("OPENCODE_THEME")); id != "" {
		return id, "env"
	}
	if id := readThemeState(workdir); id != "" {
		return id, "project"
	}
	if id, ok := cfg.Raw["theme"].(string); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id), "config"
	}
	if m, ok := cfg.Raw["theme"].(map[string]any); ok {
		if id, ok := m["id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), "config"
		}
	}
	return "default", "default"
}

func readThemeState(workdir string) string {
	if workdir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(workdir, ".opencode", "theme.json"))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxThemeStateBytes+1))
	if err != nil || len(data) > maxThemeStateBytes {
		return ""
	}
	var state struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &state) != nil {
		return ""
	}
	id := strings.TrimSpace(state.ID)
	if !validMarketplaceID(id) {
		return ""
	}
	return id
}

func writeThemeState(workdir, id string) error {
	dir := filepath.Join(workdir, ".opencode")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{"id": id}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, "theme-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, "theme.json")); err != nil {
		return err
	}
	ok = true
	return nil
}

func validMarketplaceID(id string) bool {
	return id != "" && len(id) <= maxMarketplaceIDLen && themeIDRe.MatchString(id)
}

func catalogHasID(items []marketplaceItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func themeName(id string, items []marketplaceItem) string {
	for _, item := range items {
		if item.ID == id {
			return item.Name
		}
	}
	return id
}

func marketplaceStatus(warnings []string) string {
	if len(warnings) > 0 {
		return "warning"
	}
	return "ok"
}
