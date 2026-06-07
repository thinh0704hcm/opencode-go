package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	id := int64(1)
	req := rpcRequest{JSONRPC: jsonRPCVersion, ID: &id, Method: "tools/list"}
	if err := writeMessage(&buf, req); err != nil {
		t.Fatal(err)
	}
	if buf.Bytes()[buf.Len()-1] != '\n' {
		t.Fatal("message not newline-terminated")
	}
	// A response on the wire reads back.
	resp := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` + "\n"
	r := bufio.NewReader(bytes.NewBufferString(resp))
	got, err := readMessage(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == nil || *got.ID != 1 {
		t.Fatalf("id = %v", got.ID)
	}
}

func TestToolsCallResultText(t *testing.T) {
	var res toolsCallResult
	json.Unmarshal([]byte(`{"content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}],"isError":false}`), &res)
	if res.Text() != "hello\nworld" {
		t.Fatalf("Text() = %q", res.Text())
	}
}

func TestReadMessageSkipsBlankLines(t *testing.T) {
	r := bufio.NewReader(bytes.NewBufferString("\n\n" + `{"jsonrpc":"2.0","id":2,"result":{}}` + "\n"))
	got, err := readMessage(r)
	if err != nil || got.ID == nil || *got.ID != 2 {
		t.Fatalf("got %+v err %v", got, err)
	}
}
