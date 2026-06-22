//go:build opencode_recovery_wip

package server

import (
	"fmt"
	"strings"

	"github.com/opencode-go/opencode-go/internal/session"
)

// maybeResumeParentAfterDelegatesExtended checks if parent can be resumed.
// If isRecovery is true, aborted parents are eligible.
func (s *Server) maybeResumeParentAfterDelegatesExtended(parentSessionID, parentMsgID string, isRecovery bool) {
	s.delegateResumeMu.Lock()
	defer s.delegateResumeMu.Unlock()

	// 1. Check parent session state
	s.sesMu.Lock()
	w, active := s.sesQueue[parentSessionID]
	if active && (w.running || w.draining) {
		s.sesMu.Unlock()
		s.statusMu.Lock()
		if s.pendingResumeParentMsgIDs == nil {
			s.pendingResumeParentMsgIDs = make(map[string]map[string]bool)
		}
		if _, ok := s.pendingResumeParentMsgIDs[parentSessionID]; !ok {
			s.pendingResumeParentMsgIDs[parentSessionID] = make(map[string]bool)
		}
		s.pendingResumeParentMsgIDs[parentSessionID][parentMsgID] = true
		s.statusMu.Unlock()
		return
	}
	s.sesMu.Unlock()

	msgs, ok := s.store.Messages(parentSessionID)
	if !ok {
		return
	}

	var parentMsg *session.MessageWithParts
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Info.ID == parentMsgID {
			parentMsg = &msgs[i]
			break
		}
	}
	if parentMsg == nil || parentMsg.Info.Role != "assistant" {
		return
	}

	// Normal path must NOT allow Finish=="aborted".
	if !isRecovery && parentMsg.Info.Finish == "aborted" {
		return
	}

	if !parentAssistantMutable(parentMsg) && parentMsg.Info.Finish != "aborted" && parentMsg.Info.Finish != "tool_calls" {
		return
	}

	// 2. Count subtasks in THIS message and check their child terminal state
	targetChildIDs := make(map[string]bool)
	for _, p := range parentMsg.Parts {
		if p.Type == "subtask" && p.TargetSessionID != "" {
			targetChildIDs[p.TargetSessionID] = true
		}
	}

	notificationIDs := make(map[string]bool)
	for _, p := range parentMsg.Parts {
		// Detect <task id="ID"...> or <task-notification> containing child ID
		if strings.Contains(p.Text, "<task ") && strings.Contains(p.Text, "</task>") {
			// Extract id="..."
			idx := strings.Index(p.Text, "id=\"")
			if idx != -1 {
				rem := p.Text[idx+4:]
				end := strings.Index(rem, "\"")
				if end != -1 {
					notificationIDs[rem[:end]] = true
				}
			}
		} else if strings.Contains(p.Text, "<task-notification>") {
			// Legacy fallback: find matching target session ID in text
			for cid := range targetChildIDs {
				if strings.Contains(p.Text, cid) {
					notificationIDs[cid] = true
				}
			}
		}
	}

	if len(targetChildIDs) == 0 {
		return
	}

	// require all target child sessions have terminal completed assistant (or child not running)
	s.sesMu.Lock()
	for childID := range targetChildIDs {
		if cw, ok := s.sesQueue[childID]; ok && cw.running {
			s.sesMu.Unlock()
			return
		}
	}
	s.sesMu.Unlock()

	for childID := range targetChildIDs {
		if !notificationIDs[childID] {
			return
		}
	}

	// 3. Deduplicate per parent msgID + task block count
	key := fmt.Sprintf("%s:%s", parentMsgID, parentSessionID)
	if s.delegateResumeWatermark == nil {
		s.delegateResumeWatermark = make(map[string]int)
	}
	if s.delegateResumeWatermark[key] >= len(notificationIDs) {
		return
	}
	s.delegateResumeWatermark[key] = len(notificationIDs)

	// Aggregate all child results for synthetic resume message
	var taskBlocks strings.Builder
	for _, p := range parentMsg.Parts {
		// Expect parts to contain the <task> block from completion logic
		if strings.Contains(p.Text, "<task ") && strings.Contains(p.Text, "</task>") {
			// Extract just the block
			taskBlocks.WriteString(p.Text)
			taskBlocks.WriteString("\n")
		}
	}

	resumeMsg := fmt.Sprintf("All delegated tasks have completed. Consolidate these results:\n%s\n\nRead the provided <task_result> or <task_error> blocks carefully, continue your original request based on these results, and do not ask me to provide or paste the results again. Provide the final consolidated response.", taskBlocks.String())
	s.QueueSyntheticMessage(parentSessionID, resumeMsg, parentMsgID)
}
