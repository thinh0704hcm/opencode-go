//go:build opencode_wip

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type devcontainerConfig struct {
	Enabled bool
	Image   string
	Timeout time.Duration
}

func loadDevcontainerConfig() devcontainerConfig {
	cfg := devcontainerConfig{}
	// DEV_CONTAINER_ENABLED
	if val := os.Getenv("DEV_CONTAINER_ENABLED"); val != "" {
		lowered := strings.ToLower(val)
		if lowered == "1" || lowered == "true" || lowered == "yes" {
			cfg.Enabled = true
		}
	}
	cfg.Image = os.Getenv("DEV_CONTAINER_IMAGE")
	timeoutSec := 300 // default 300 seconds
	if val := os.Getenv("DEV_CONTAINER_TIMEOUT"); val != "" {
		if i, err := strconv.Atoi(val); err == nil && i > 0 {
			timeoutSec = i
		}
	}
	cfg.Timeout = time.Duration(timeoutSec) * time.Second
	return cfg
}

type devcontainerRequest struct {
	SessionID string   `json:"sessionID"`
	Cmd       []string `json:"cmd"`
}

type devcontainerResponse struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

var devcontainerRunner = runDevcontainerDocker

func runDevcontainerDocker(ctx context.Context, cfg devcontainerConfig, workdir string, cmd []string) (string, error) {
	// Ensure docker binary exists
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker not found in PATH")
	}
	// Build docker args
	uid := os.Getuid()
	gid := os.Getgid()
	userArg := fmt.Sprintf("%d:%d", uid, gid)
	args := []string{"run", "--rm", "--network", "none", "--user", userArg, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "512", "-v", fmt.Sprintf("%s:/work", workdir), "-w", "/work", cfg.Image}
	args = append(args, cmd...)
	c := exec.CommandContext(ctx, "docker", args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func (s *Server) handleDevcontainerBootstrap(w http.ResponseWriter, r *http.Request) {
	var req devcontainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, devcontainerResponse{Error: "invalid request body"})
		return
	}
	cfg := loadDevcontainerConfig()
	if !cfg.Enabled {
		writeJSON(w, http.StatusForbidden, devcontainerResponse{Error: "devcontainer bootstrap disabled (set DEV_CONTAINER_ENABLED=1)"})
		return
	}
	if cfg.Image == "" {
		writeJSON(w, http.StatusBadRequest, devcontainerResponse{Error: "DEV_CONTAINER_IMAGE not configured"})
		return
	}
	if req.SessionID == "" {
		writeJSON(w, http.StatusBadRequest, devcontainerResponse{Error: "sessionID required"})
		return
	}
	workdir := s.SessionWorkdir(req.SessionID)
	ctx, cancel := context.WithTimeout(r.Context(), cfg.Timeout)
	defer cancel()
	out, err := devcontainerRunner(ctx, cfg, workdir, req.Cmd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, devcontainerResponse{Output: out, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, devcontainerResponse{Output: out})
}
