package mcp

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// mockServerScript is a python MCP-stdio server: responds to initialize,
// tools/list (one echo tool), and tools/call (echoes args back as text).
const mockServerScript = `
import sys, json
for line in sys.stdin:
    line=line.strip()
    if not line: continue
    msg=json.loads(line)
    mid=msg.get("id")
    method=msg.get("method")
    if method=="notifications/initialized":
        continue
    if method=="initialize":
        out={"jsonrpc":"2.0","id":mid,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"0"}}}
    elif method=="tools/list":
        out={"jsonrpc":"2.0","id":mid,"result":{"tools":[{"name":"echo","description":"echoes","inputSchema":{"type":"object"}}]}}
    elif method=="tools/call":
        args=msg.get("params",{}).get("arguments",{})
        out={"jsonrpc":"2.0","id":mid,"result":{"content":[{"type":"text","text":json.dumps(args)}],"isError":False}}
    else:
        out={"jsonrpc":"2.0","id":mid,"error":{"code":-32601,"message":"method not found"}}
    sys.stdout.write(json.dumps(out)+"\n"); sys.stdout.flush()
`

func skipIfNoPython(t *testing.T) string {
	for _, p := range []string{"python3", "python"} {
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	t.Skip("python not available")
	return ""
}

func TestStdioClientLifecycle(t *testing.T) {
	py := skipIfNoPython(t)
	c, err := NewStdioClient("mock", []string{py, "-c", mockServerScript}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}

	args, _ := json.Marshal(map[string]any{"hello": "world"})
	text, isErr, err := c.CallTool("echo", args)
	if err != nil || isErr {
		t.Fatalf("CallTool err=%v isErr=%v", err, isErr)
	}
	if text == "" || text[0] != '{' {
		t.Fatalf("echo text = %q", text)
	}
}
