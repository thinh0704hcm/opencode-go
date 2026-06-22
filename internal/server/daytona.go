//go:build opencode_wip

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SandboxInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
}

type ExecResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type SandboxAdapter interface {
	Create(ctx context.Context, env map[string]string) (SandboxInfo, error)
	Execute(ctx context.Context, sandboxID string, cmd []string) (ExecResult, error)
	Delete(ctx context.Context, sandboxID string) error
	Status(ctx context.Context, sandboxID string) (SandboxInfo, error)
	List(ctx context.Context) ([]SandboxInfo, error)
}

type daytonaAdapter struct {
	apiKey  string
	baseURL *url.URL
	client  *http.Client
	timeout time.Duration
	target  string // optional target env
}

// validateSandboxID ensures ID matches allowed charset, length <=128, non-empty.
var sandboxIDRegex = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// parseSandboxPath extracts sandbox ID and optional action from request URL.
// Expected paths:
//
//	/api/sandbox/{id}
//	/api/sandbox/{id}/execute
//
// Returns (id, 0, "") on success.
// On error, returns ("", httpStatus, errorMessage).
func parseSandboxPath(r *http.Request, expectAction string) (string, int, string) {
	// Trim the known prefix
	const prefix = "/api/sandbox/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	// Reject empty path or leading/trailing slashes that would produce empty segments
	if path == "" {
		return "", http.StatusBadRequest, "sandbox id required"
	}
	// Split into at most two parts
	parts := strings.Split(path, "/")
	// ID must be first segment and non-empty
	id := parts[0]
	if id == "" {
		return "", http.StatusBadRequest, "sandbox id required"
	}
	// Validate ID format early
	if err := validateSandboxID(id); err != nil {
		return "", http.StatusBadRequest, "invalid sandbox id"
	}
	// No action expected: ensure no extra segments
	if expectAction == "" {
		if len(parts) != 1 {
			return "", http.StatusBadRequest, "invalid path"
		}
		return id, 0, ""
	}
	// Action expected: must have exactly two segments and second matches
	if len(parts) != 2 || parts[1] != expectAction {
		return "", http.StatusBadRequest, "invalid path"
	}
	return id, 0, ""
}

// validateSandboxID ensures ID matches allowed charset, length <=128, non-empty.
func validateSandboxID(id string) error {
	if len(id) == 0 {
		return fmt.Errorf("sandbox id required")
	}
	if len(id) > 128 {
		return fmt.Errorf("sandbox id too long")
	}
	if !sandboxIDRegex.MatchString(id) {
		return fmt.Errorf("sandbox id contains invalid characters")
	}
	return nil
}

func newDaytonaAdapter() (SandboxAdapter, error) {
	key := os.Getenv("DAYTONA_API_KEY")
	raw := os.Getenv("DAYTONA_API_URL")
	if key == "" || raw == "" {
		return nil, errors.New("daytona env not set")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid DAYTONA_API_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("DAYTONA_API_URL must start with http or https")
	}
	timeoutSec := 300
	if t := os.Getenv("DEV_CONTAINER_TIMEOUT"); t != "" {
		if i, err := strconv.Atoi(t); err == nil && i > 0 {
			timeoutSec = i
		}
	}
	return &daytonaAdapter{apiKey: key, baseURL: u, client: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}, timeout: time.Duration(timeoutSec) * time.Second, target: os.Getenv("DAYTONA_TARGET")}, nil
}

func (d *daytonaAdapter) doRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	fullURL := *d.baseURL
	fullURL.Path = strings.TrimSuffix(fullURL.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL.String(), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return d.client.Do(req)
}

func (d *daytonaAdapter) Create(ctx context.Context, env map[string]string) (SandboxInfo, error) {
	// child timeout
	ct, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := d.doRequest(ct, "POST", "/api/sandbox", map[string]any{"env": env})
	if err != nil {
		return SandboxInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return SandboxInfo{}, fmt.Errorf("daytona create failed: %s", resp.Status)
	}
	var info SandboxInfo
	// limit body to 1MiB
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return SandboxInfo{}, err
	}
	return info, nil
}

func (d *daytonaAdapter) Execute(ctx context.Context, sandboxID string, cmd []string) (ExecResult, error) {
	// child timeout
	ct, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	payload := map[string]any{"cmd": cmd}
	resp, err := d.doRequest(ct, "POST", fmt.Sprintf("/api/sandbox/%s/execute", sandboxID), payload)
	if err != nil {
		return ExecResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ExecResult{}, fmt.Errorf("daytona execute failed: %s", resp.Status)
	}
	var res ExecResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&res); err != nil {
		return ExecResult{}, err
	}
	return res, nil
}

func (d *daytonaAdapter) Delete(ctx context.Context, sandboxID string) error {
	// child timeout
	ct, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := d.doRequest(ct, "DELETE", fmt.Sprintf("/api/sandbox/%s", sandboxID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("daytona delete failed: %s", resp.Status)
	}
	return nil
}

func (d *daytonaAdapter) Status(ctx context.Context, sandboxID string) (SandboxInfo, error) {
	// child timeout
	ct, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := d.doRequest(ct, "GET", fmt.Sprintf("/api/sandbox/%s", sandboxID), nil)
	if err != nil {
		return SandboxInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SandboxInfo{}, fmt.Errorf("daytona status failed: %s", resp.Status)
	}
	var info SandboxInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return SandboxInfo{}, err
	}
	return info, nil
}

func (d *daytonaAdapter) List(ctx context.Context) ([]SandboxInfo, error) {
	// child timeout
	ct, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := d.doRequest(ct, "GET", "/api/sandbox", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daytona list failed: %s", resp.Status)
	}
	var list []SandboxInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// Handler wrappers (methods on Server). The Server struct already exists.
func (s *Server) handleDaytonaCreate(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DAYTONA_API_KEY") == "" || os.Getenv("DAYTONA_API_URL") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Daytona disabled (set DAYTONA_API_KEY & DAYTONA_API_URL)"})
		return
	}
	// optional env mapping – for now empty
	adapter, err := newDaytonaAdapter()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	info, err := adapter.Create(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Validate returned sandbox ID
	if err := validateSandboxID(info.ID); err != nil {
		// Do not expose internal details, return gateway error
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid sandbox id"})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleDaytonaList(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DAYTONA_API_KEY") == "" || os.Getenv("DAYTONA_API_URL") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Daytona disabled"})
		return
	}
	adapter, err := newDaytonaAdapter()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	list, err := adapter.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDaytonaStatus(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DAYTONA_API_KEY") == "" || os.Getenv("DAYTONA_API_URL") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Daytona disabled"})
		return
	}
	id, status, errMsg := parseSandboxPath(r, "")
	if status != 0 {
		writeJSON(w, status, map[string]string{"error": errMsg})
		return
	}
	// id already validated in parseSandboxPath
	// proceed with adapter call
	adapter, err := newDaytonaAdapter()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	info, err := adapter.Status(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleDaytonaExecute(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DAYTONA_API_KEY") == "" || os.Getenv("DAYTONA_API_URL") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Daytona disabled"})
		return
	}
	// extract id and action
	id, status, errMsg := parseSandboxPath(r, "execute")
	if status != 0 {
		writeJSON(w, status, map[string]string{"error": errMsg})
		return
	}
	var req struct {
		Cmd []string `json:"cmd"`
	}
	// limit request body to 1MiB
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	adapter, err := newDaytonaAdapter()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	res, err := adapter.Execute(r.Context(), id, req.Cmd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDaytonaDelete(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DAYTONA_API_KEY") == "" || os.Getenv("DAYTONA_API_URL") == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Daytona disabled"})
		return
	}
	id, status, errMsg := parseSandboxPath(r, "")
	if status != 0 {
		writeJSON(w, status, map[string]string{"error": errMsg})
		return
	}
	// id already validated in parseSandboxPath
	// proceed with adapter call
	adapter, err := newDaytonaAdapter()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := adapter.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "deleted"})
}
