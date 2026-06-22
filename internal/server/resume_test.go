//go:build opencode_recovery_wip

package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

func TestParentAssistantAgentResumesWithSameAgent(t *testing.T) {
	dir := t.TempDir()

	// Create a reviewer agent
	agentDir := filepath.Join(dir, ".opencode", "agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte("---\nname: reviewer\nmode: reviewer\n---\nReviewer prompt"), 0644)

	srv := New(Options{
		Provider: provider.NewScriptedToolProvider(nil, "final text"),
		Model:    "test-model",
		Tools:    tool.NewDefaultRegistry(),
		Workdir:  dir,
	})

	// Setup a session with an assistant message using the reviewer agent
	sid := "ses_test"
	srv.store.CreateSessionWithID(sid, "", "Test Session", dir)

	msg, ok := srv.store.NewAssistantMessage(sid, "", "reviewer", "reviewer", "reviewer", "reviewer", false)
	if !ok {
		t.Fatal("failed to create assistant msg")
	}
	msgID := msg.Info.ID
	srv.store.PersistSession(sid)

	// Trigger resume
	srv.QueueSyntheticMessage(sid, "resuming", msgID)

	// SessionWork starts in background. Poll for completion.
	var resumed bool
	for i := 0; i < 50; i++ {
		msgs, _ := srv.store.Messages(sid)
		for _, m := range msgs {
			if m.Info.Role == "assistant" && m.Info.ID != msgID {
				if m.Info.Agent != "reviewer" {
					t.Errorf("resumed message agent = %q, want reviewer", m.Info.Agent)
				}
				if m.Info.Mode != "reviewer" {
					t.Errorf("resumed message mode = %q, want reviewer", m.Info.Mode)
				}
				resumed = true
				break
			}
		}
		if resumed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !resumed {
		t.Fatal("new assistant message was not created for resume")
	}
}

func TestQueueSyntheticMessageInvalidResumeID(t *testing.T) {
	srv := New(Options{Provider: provider.NewScriptedToolProvider(nil, "final text")})
	sid := "ses_invalid"
	srv.store.CreateSessionWithID(sid, "", "Test Session", t.TempDir())

	// Should not panic/error, just return
	srv.QueueSyntheticMessage(sid, "test", "non-existent")

	msgs, _ := srv.store.Messages(sid)
	for _, m := range msgs {
		if m.Info.Role == "user" {
			t.Fatal("should not have appended user message")
		}
	}
}

func TestQueueSyntheticMessageMultipleAssistants(t *testing.T) {
	srv := New(Options{Provider: provider.NewScriptedToolProvider(nil, "final text")})
	sid := "ses_multi"
	srv.store.CreateSessionWithID(sid, "", "Test Session", t.TempDir())

	// Older reviewer
	m1, _ := srv.store.NewAssistantMessage(sid, "", "reviewer", "reviewer", "reviewer", "reviewer", true)
	m1.Info.ProviderID = "p1"
	m1.Info.ModelID = "m1"
	srv.store.CompleteAssistantMessage(sid, m1.Info.ID)

	// Newer build
	m2, _ := srv.store.NewAssistantMessage(sid, "", "build", "build", "build", "build", true)
	m2.Info.ProviderID = "p2"
	m2.Info.ModelID = "m2"
	srv.store.CompleteAssistantMessage(sid, m2.Info.ID)

	// Resume older
	srv.QueueSyntheticMessage(sid, "resume", m1.Info.ID)

	// Verify messages
	msgs, _ := srv.store.Messages(sid)
	for _, m := range msgs {
		if m.Info.Role == "assistant" && m.Info.ID != m1.Info.ID && m.Info.ID != m2.Info.ID {
			if m.Info.ProviderID != "p1" || m.Info.ModelID != "m1" {
				t.Errorf("resumed message wrong provider/model, got %s/%s, want p1/m1", m.Info.ProviderID, m.Info.ModelID)
			}
		}
	}
}
