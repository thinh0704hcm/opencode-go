package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextContentMarshalsAsString(t *testing.T) {
	m := ChatMessage{Role: "user", Content: TextContent("hello")}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"content":"hello"`) {
		t.Fatalf("text content not a plain string: %s", b)
	}
}

func TestMultimodalContentMarshalsAsArray(t *testing.T) {
	m := ChatMessage{Role: "user", Content: MultimodalContent("look", []string{"data:image/png;base64,AAA"})}
	b, _ := json.Marshal(m)
	s := string(b)
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, `"type":"image_url"`) || !strings.Contains(s, `"url":"data:image/png;base64,AAA"`) {
		t.Fatalf("multimodal content not built correctly: %s", s)
	}
}

func TestMultimodalEmptyImagesIsString(t *testing.T) {
	if _, ok := MultimodalContent("hi", nil).(string); !ok {
		t.Fatal("no images should return plain string content")
	}
}
