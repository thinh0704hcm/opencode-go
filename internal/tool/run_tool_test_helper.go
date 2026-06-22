package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// runTool is a lightweight test helper to execute a registered tool.
func runTool(t *testing.T, r *Registry, name string, sb *Sandbox, in any) (Result, error) {
	t.Helper()
	tool, ok := r.Lookup(name)
	if !ok {
		return Result{}, fmt.Errorf("tool %s not found", name)
	}
	var raw json.RawMessage
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return Result{}, err
		}
		raw = b
	}
	res, err := tool.Execute(context.Background(), raw, sb)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}
