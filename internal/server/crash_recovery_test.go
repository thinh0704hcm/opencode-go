//go:build opencode_recovery_wip

package server

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/stretchr/testify/require"
)

func TestCrashRecoveryAbortedChildResumesParent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "opencode-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	st := session.NewStore()
	st.SetPersistDir(tmpDir)

	// Setup parent
	parentSess := st.CreateSession("", "test parent", "")
	parentID := parentSess.ID

	// Setup child
	childSess := st.CreateSession(parentID, "test child", "")
	childID := childSess.ID
	st.SetSessionState(childID, "aborted")

	mwp, ok := st.NewAssistantMessage(parentID, "", "concactao", "model-1", "chat", "")
	require.True(t, ok)
	assistantID := mwp.Info.ID
	st.AppendSubtaskPart(parentID, assistantID, "run test", "desc", "general", "provider", "model", childID)
	// IMPORTANT: Set assistant as mutable via finish == "aborted" for recovery logic compatibility
	st.SetAssistantFinish(parentID, assistantID, "aborted")

	srv := &Server{
		bus:           event.NewBus(),
		store:         st,
		sesQueue:      make(map[string]*sessionWork),
		sessionStatus: make(map[string]string),
		cancels:       make(map[string]context.CancelFunc),
		provider:      provider.NewMock("concactao"),
	}

	// First pass
	srv.notifyAbortedChildren(st)

	// Assertions
	msgs, _ := st.Messages(parentID)
	// We might have multiple parts, but they should only contain ONE task_error total per assistant message.
	errorCount, synthCount := 0, 0
	for _, m := range msgs {
		if m.Info.Role == "assistant" && m.Info.ID == assistantID {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "id=\""+childID+"\"") && strings.Contains(p.Text, "<task_error>") {
					errorCount++
				}
			}
		}
		// Synthetic messages might be in USER role or linked to assistant via marker.
		// QueueSyntheticMessage appends a USER message.
		if m.Info.Role == "user" {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "All delegated tasks have completed") {
					synthCount++
				}
			}
		}
	}
	require.Equal(t, 1, errorCount, "Should have exactly one task_error")
	require.Equal(t, 1, synthCount, "Should have exactly one synthetic resume message")

	// Second pass: assert no duplicates
	srv.notifyAbortedChildren(st)
	msgs3, _ := st.Messages(parentID)

	errorCount2, synthCount2 := 0, 0
	for _, m := range msgs3 {
		if m.Info.Role == "assistant" && m.Info.ID == assistantID {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "id=\""+childID+"\"") && strings.Contains(p.Text, "<task_error>") {
					errorCount2++
				}
			}
		}
		if m.Info.Role == "user" {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "All delegated tasks have completed") {
					synthCount2++
				}
			}
		}
	}
	require.Equal(t, 1, errorCount2, "Should not duplicate error")
	require.Equal(t, 1, synthCount2, "Should not duplicate synthetic message")
}

func TestCrashRecoveryDedupeAcrossRestart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "opencode-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	st1 := session.NewStore()
	st1.SetPersistDir(tmpDir)

	parentSess := st1.CreateSession("", "parent-dedupe", "")
	parentID := parentSess.ID
	childSess := st1.CreateSession(parentID, "child-dedupe", "")
	childID := childSess.ID
	st1.SetSessionState(childID, "aborted")

	mwp, ok := st1.NewAssistantMessage(parentID, "", "concactao", "model-1", "chat", "")
	require.True(t, ok)
	assistantID := mwp.Info.ID
	st1.AppendSubtaskPart(parentID, assistantID, "run test", "desc", "general", "provider", "model", childID)
	st1.SetAssistantFinish(parentID, assistantID, "aborted")

	srv1 := &Server{
		bus:           event.NewBus(),
		store:         st1,
		sesQueue:      make(map[string]*sessionWork),
		sessionStatus: make(map[string]string),
		cancels:       make(map[string]context.CancelFunc),
		provider:      provider.NewMock("concactao"),
	}

	// Notify once
	srv1.notifyAbortedChildren(st1)

	// Persist child session explicitly to ensure Load() sees it as aborted
	st1.PersistSession(childID)

	// Create fresh Store from same persist dir
	st2 := session.NewStore()
	st2.SetPersistDir(tmpDir)
	require.NoError(t, st2.Load())

	// Assert child exists and State==aborted
	childSess2, ok := st2.GetSession(childID)
	require.True(t, ok)
	require.Equal(t, "aborted", childSess2.State)

	srv2 := &Server{
		bus:           event.NewBus(),
		store:         st2,
		sesQueue:      make(map[string]*sessionWork),
		sessionStatus: make(map[string]string),
		cancels:       make(map[string]context.CancelFunc),
		provider:      provider.NewMock("concactao"),
	}

	// Notify again
	srv2.notifyAbortedChildren(st2)

	msgs, _ := st2.Messages(parentID)
	synthCount := 0
	marker := "<!-- opencode-resume-assistant:" + assistantID + " -->"

	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "All delegated tasks have completed") && strings.Contains(p.Text, marker) {
				synthCount++
			}
		}
	}
	require.Equal(t, 1, synthCount, "Should have exactly one synthetic resume message with marker across restart")
}

func TestCrashRecoverySyntheticMissingResumesParent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "opencode-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	st := session.NewStore()
	st.SetPersistDir(tmpDir)

	parentSess := st.CreateSession("", "parent-2", "")
	parentID := parentSess.ID
	childSess := st.CreateSession(parentID, "child-2", "")
	childID := childSess.ID
	st.SetSessionState(childID, "aborted")

	mwp, ok := st.NewAssistantMessage(parentID, "", "concactao", "model-1", "chat", "")
	require.True(t, ok)
	assistantID := mwp.Info.ID
	st.AppendSubtaskPart(parentID, assistantID, "run test", "desc", "general", "provider", "model", childID)
	st.AppendAssistantTextPart(parentID, assistantID, renderTaskResult(childID, "system", "aborted", "aborted before completion"))
	st.SetAssistantFinish(parentID, assistantID, "aborted")

	srv := &Server{
		bus:           event.NewBus(),
		store:         st,
		sesQueue:      make(map[string]*sessionWork),
		sessionStatus: make(map[string]string),
		cancels:       make(map[string]context.CancelFunc),
		provider:      provider.NewMock("concactao"),
	}

	srv.notifyAbortedChildren(st)

	msgs, _ := st.Messages(parentID)
	foundSynthetic := false
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "All delegated tasks have completed") {
				foundSynthetic = true
			}
		}
	}
	require.True(t, foundSynthetic, "Should create missing synthetic resume")
}

func TestCrashRecoveryTerminalParentNoResume(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "opencode-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	st := session.NewStore()
	st.SetPersistDir(tmpDir)

	parentSess := st.CreateSession("", "parent-terminal", "")
	parentID := parentSess.ID
	childSess := st.CreateSession(parentID, "child-terminal", "")
	childID := childSess.ID
	st.SetSessionState(childID, "aborted")

	mwp, ok := st.NewAssistantMessage(parentID, "", "concactao", "model-1", "chat", "")
	require.True(t, ok)
	assistantID := mwp.Info.ID
	st.AppendSubtaskPart(parentID, assistantID, "run test", "desc", "general", "provider", "model", childID)
	st.SetAssistantFinish(parentID, assistantID, "stop")

	srv := &Server{
		bus:           event.NewBus(),
		store:         st,
		sesQueue:      make(map[string]*sessionWork),
		sessionStatus: make(map[string]string),
		cancels:       make(map[string]context.CancelFunc),
		provider:      provider.NewMock("concactao"),
	}

	srv.notifyAbortedChildren(st)

	msgs, _ := st.Messages(parentID)
	for _, m := range msgs {
		for _, p := range m.Parts {
			require.NotContains(t, p.Text, "<task_error>", "Terminal parent should not get error notification")
			require.NotContains(t, p.Text, "All delegated tasks have completed", "Terminal parent should not get synthetic resume")
		}
	}
}
