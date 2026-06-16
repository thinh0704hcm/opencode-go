package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// ctxSessionID is the context key carrying the current session ID so delegate/task
// can link sub-sessions to their parent for Ctrl+X+Down navigation.
type ctxSessionIDKey struct{}

func withSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxSessionIDKey{}, id)
}

func sessionIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(ctxSessionIDKey{}).(string)
	return id
}

// ctxPermSessionKey carries the "visible" session ID for permission events.
// For sub-sessions (delegate/task) this is the parent's session ID so that:
//  1. IsAllowed checks inherit the parent's "always allow" grants.
//  2. Permission-asked events carry the parent's session ID and appear in
//     the parent session's TUI panel rather than being invisible.
type ctxPermSessionKey struct{}

func withPermSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxPermSessionKey{}, id)
}

func permSessionIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(ctxPermSessionKey{}).(string)
	return id
}

type delegateTool struct{ srv *Server }

type taskTool struct{ srv *Server }

func (delegateTool) Name() string   { return "delegate" }
func (delegateTool) Mutating() bool { return false }
func (t delegateTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	return t.srv.runDelegated(ctx, input)
}

func (taskTool) Name() string   { return "task" }
func (taskTool) Mutating() bool { return false }
func (t taskTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	return t.srv.runDelegated(ctx, input)
}

type delegateInput struct {
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
}

func (s *Server) runDelegated(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var in delegateInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tool.Result{}, err
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(in.Description)
	}
	if prompt == "" {
		return tool.Result{}, fmt.Errorf("delegate/task requires prompt or description")
	}

	agentName := strings.TrimSpace(in.Agent)
	agent, _ := resolveAgent(s.workdir, agentName)
	if agent.Name == "" {
		agent.Name = "build"
	}
	// Prevent infinite recursion: sub-agent cannot call delegate or task.
	if agent.Tools == nil {
		agent.Tools = map[string]bool{}
	}
	agent.Tools["delegate"] = false
	agent.Tools["task"] = false

	modelID := s.model
	if strings.TrimSpace(in.Model) != "" {
		modelID = strings.TrimSpace(in.Model)
		if idx := strings.Index(modelID, "/"); idx >= 0 && idx < len(modelID)-1 {
			modelID = modelID[idx+1:]
		}
	}

	// Get parent session ID from context.
	parentSessionID := sessionIDFromCtx(ctx)
	if parentSessionID == "" {
		return tool.Result{}, fmt.Errorf("delegate: no parent session in context")
	}

	// Create child session.
	childSession := s.store.CreateSession(parentSessionID, fmt.Sprintf("Task: %s", agentName), s.workdir)
	childSessionID := childSession.ID

	s.logger.Debug("delegate child run started", "agent", agentName, "model", modelID, "parent_session", parentSessionID, "child_session", childSessionID)

	mode := agent.Mode
	if mode == "" {
		mode = "build"
	}

	// Create initial message in child session.
	asst, ok := s.store.NewAssistantMessage(childSessionID, "", s.configuredProviderID, modelID, agent.Name, mode, false)
	if !ok {
		return tool.Result{}, fmt.Errorf("delegate: failed to create sub-agent message in child session")
	}

	// Build a minimal history for the sub-agent.
	minimalHistory := []provider.ChatMessage{
		{Role: "user", Content: provider.TextContent(prompt)},
	}

	// Propagate parent's permission grants.
	subCtx := withPermSessionID(context.Background(), parentSessionID)

	go func() {
		defer s.logger.Debug("delegate child run completed", "agent", agentName, "model", modelID, "child_session", childSessionID)
		
		// Publish initial event to child session.
		s.bus.Publish(event.NewMessageUpdated(childSessionID, asst.Info, false))
		if len(asst.Parts) > 0 {
			s.bus.Publish(event.NewMessagePartUpdated(childSessionID, asst.Parts[0], time.Now().UnixMilli()))
		}

		finishReason := s.runAgentLoop(subCtx, childSessionID, asst.Info.ID, "", modelID, []string{prompt}, nil, "", agent, minimalHistory)

		reason := finishReason
		if reason == "" {
			reason = "stop"
		}
		if subCtx.Err() != nil {
			reason = "aborted"
		}
		
		var stepTokens *session.Tokens
		var stepCost float64
		if info, ok2 := s.store.MessageInfo(childSessionID, asst.Info.ID); ok2 && info.Tokens != nil {
			stepTokens = info.Tokens
			stepCost = computeCost(info.ModelID, info.Tokens.Input, info.Tokens.Output)
		}
		if stepTokens == nil {
			stepTokens = &session.Tokens{}
		}
		if sf, ok2 := s.store.AppendStepFinish(childSessionID, asst.Info.ID, reason, stepCost, stepTokens); ok2 {
			s.bus.Publish(event.NewMessagePartUpdated(childSessionID, sf, time.Now().UnixMilli()))
		}
		s.finishGeneration(childSessionID, asst.Info.ID)
	}()

	return tool.Result{
		Output: fmt.Sprintf("Delegated task to %s. Session ID: %s. Use delegation_read(id) to read result later.", agentName, childSessionID),
	}, nil
}
