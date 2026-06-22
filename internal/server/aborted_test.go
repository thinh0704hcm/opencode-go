//go:build opencode_recovery_wip

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
)

func TestAbortedChildNotification(t *testing.T) {
	tmp, err := os.MkdirTemp("", "persist-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	sessDir := filepath.Join(tmp, "sessions")
	os.Mkdir(sessDir, 0o755)

	st := session.NewStore()
	st.SetPersistDir(tmp)

	// 1. Live notification: active parent with owning subtask
	parent1 := st.CreateSession("", "ParentLive", "/tmp")
	u1, _ := st.AppendUserMessage(parent1.ID, "user_1", "prov", "model", "agent", []string{"prompt"})
	asst1, _ := st.NewAssistantMessage(parent1.ID, u1.Info.ID, "prov", "model", "agent", "mode", false)
	child1ID := "child_live"
	st.AppendSubtaskPart(parent1.ID, asst1.Info.ID, "prompt", "desc", "agent", "prov", "model", child1ID)
	st.SetAssistantFinish(parent1.ID, asst1.Info.ID, "tool_calls")

	// Server with this store
	srv := New(Options{Provider: provider.NewMock("mock"), DataDir: tmp})
	srv.store = st

	// Inject aborted notification
	srv.injectTaskError(parent1.ID, asst1.Info.ID, child1ID, "aborted before completion")

	msgs1, _ := st.Messages(parent1.ID)
	found1 := false
	for _, m := range msgs1 {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "aborted before completion") && strings.Contains(p.Text, child1ID) {
				found1 = true
			}
		}
	}
	if !found1 {
		t.Error("parent1 missing aborted notification")
	}
	if sess, _ := st.GetSession(parent1.ID); sess.State == "aborted" {
		t.Error("parent1 should NOT be marked aborted")
	}

	// 2. Restart case: parent was running, child was running. Load marks parent aborted.
	// But if we use notifyAbortedChildren, it should handle the case.
	parent2 := st.CreateSession("", "ParentRestart", "/tmp")
	u2, _ := st.AppendUserMessage(parent2.ID, "user_2", "prov", "model", "agent", []string{"prompt"})
	asst2, _ := st.NewAssistantMessage(parent2.ID, u2.Info.ID, "prov", "model", "agent", "mode", false)
	child2ID := "child_restart"
	st.AppendSubtaskPart(parent2.ID, asst2.Info.ID, "prompt", "desc", "agent", "prov", "model", child2ID)
	// IMPORTANT: Use "tool_calls" to stay mutable
	st.SetAssistantFinish(parent2.ID, asst2.Info.ID, "tool_calls")

	// Create child in separate store to avoid memory sharing, mark aborted
	stC := session.NewStore()
	stC.SetPersistDir(tmp)
	stC.CreateSessionWithID(child2ID, parent2.ID, "Child", "/tmp")
	stC.SessionMarkAborted(child2ID)
	stC.PersistSession(child2ID)

	st.PersistSession(parent2.ID)

	// Load fresh
	stLoad := session.NewStore()
	stLoad.SetPersistDir(tmp)
	stLoad.Load()

	srv2 := New(Options{Provider: provider.NewMock("mock"), DataDir: tmp})
	srv2.store = stLoad
	srv2.notifyAbortedChildren(stLoad)

	msgs2, _ := stLoad.Messages(parent2.ID)
	found2 := false
	for _, m := range msgs2 {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "aborted before completion") && strings.Contains(p.Text, child2ID) {
				found2 = true
			}
		}
	}
	if !found2 {
		// Debug output
		t.Errorf("parent2 missing aborted notification after restart. msgs: %+v", msgs2)
	}

	// 3. Completed stop parent: should NOT receive notification
	parent3 := st.CreateSession("", "ParentStop", "/tmp")
	u3, _ := st.AppendUserMessage(parent3.ID, "user_3", "prov", "model", "agent", []string{"prompt"})
	asst3, _ := st.NewAssistantMessage(parent3.ID, u3.Info.ID, "prov", "model", "agent", "mode", false)
	child3ID := "child_stop"
	st.AppendSubtaskPart(parent3.ID, asst3.Info.ID, "prompt", "desc", "agent", "prov", "model", child3ID)
	st.SetAssistantFinish(parent3.ID, asst3.Info.ID, "stop")
	st.CompleteAssistantMessage(parent3.ID, asst3.Info.ID)
	st.PersistSession(parent3.ID)

	stC3 := session.NewStore()
	stC3.SetPersistDir(tmp)
	stC3.CreateSessionWithID(child3ID, parent3.ID, "Child", "/tmp")
	stC3.SessionMarkAborted(child3ID)
	stC3.PersistSession(child3ID)

	stLoad3 := session.NewStore()
	stLoad3.SetPersistDir(tmp)
	stLoad3.Load()
	srv3 := New(Options{Provider: provider.NewMock("mock"), DataDir: tmp})
	srv3.store = stLoad3
	srv3.notifyAbortedChildren(stLoad3)

	msgs3, _ := stLoad3.Messages(parent3.ID)
	for _, m := range msgs3 {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "aborted before completion") {
				t.Errorf("parent3 (stop) incorrectly notified of child %s", child3ID)
			}
		}
	}
}
