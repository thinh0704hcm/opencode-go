package session

import "testing"

func TestDCPStatsShape(t *testing.T) {
	s := NewStore()
	sessID := "ses_test"
	s.CreateSessionWithID(sessID, "", "", "")
	// add two compression blocks
	b1 := CompressionBlock{ID: "b1", SessionID: sessID, Mode: "range", Summary: "summary1", OriginalCount: 2, OriginalChars: 10, Created: 0, Focus: ""}
	b2 := CompressionBlock{ID: "b2", SessionID: sessID, Mode: "range", Summary: "summary longer", OriginalCount: 3, OriginalChars: 20, Created: 0, Focus: ""}
	s.AddCompressionBlock(sessID, b1)
	s.AddCompressionBlock(sessID, b2)
	stats := s.DCPStats(sessID)
	if stats["blocks"].(int) != 2 {
		t.Errorf("blocks count %v", stats["blocks"])
	}
	if stats["originalCount"].(int) != 5 {
		t.Errorf("originalCount %v", stats["originalCount"])
	}
	if stats["originalChars"].(int) != 30 {
		t.Errorf("originalChars %v", stats["originalChars"])
	}
	// savedChars = totalOriginalChars - totalSummaryChars
	// summary chars = len("summary1") + len("summary longer")
	expectedSaved := 30 - (len("summary1") + len("summary longer"))
	if stats["savedChars"].(int) != expectedSaved {
		t.Errorf("savedChars %v, expected %d", stats["savedChars"], expectedSaved)
	}
}
