package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type mdTableFormatterInput struct {
	Text string `json:"text"`
}

type mdTableFormatterTool struct{}

func (mdTableFormatterTool) Name() string   { return "md_table_formatter" }
func (mdTableFormatterTool) Mutating() bool { return false }

// NewMDTableFormatterTool creates the tool instance.
func NewMDTableFormatterTool() Tool { return mdTableFormatterTool{} }

func (mdTableFormatterTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in mdTableFormatterInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Text) > 1<<20 {
		return Result{}, fmt.Errorf("input too large (%d bytes), max 1 MiB", len(in.Text))
	}
	lines := strings.Split(in.Text, "\n")
	// Collect rows for alignment.
	var rows [][]string
	for _, line := range lines {
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			// Trim leading/trailing empty parts from optional surrounding pipes.
			if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
				parts = parts[1:]
			}
			if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
				parts = parts[:len(parts)-1]
			}
			for i, p := range parts {
				parts[i] = strings.TrimSpace(p)
			}
			rows = append(rows, parts)
		} else {
			rows = append(rows, nil) // sentinel for non‑table line.
		}
	}
	// Determine column widths.
	maxCols := 0
	for _, r := range rows {
		if r == nil {
			continue
		}
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	colWidths := make([]int, maxCols)
	for _, r := range rows {
		if r == nil {
			continue
		}
		for i, cell := range r {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}
	// Re‑build output with padded columns.
	var b strings.Builder
	for _, line := range lines {
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
				parts = parts[1:]
			}
			if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
				parts = parts[:len(parts)-1]
			}
			for i, p := range parts {
				parts[i] = strings.TrimSpace(p)
			}
			// Pad each cell to column width.
			for i := range parts {
				width := colWidths[i]
				parts[i] = fmt.Sprintf("%-*s", width, parts[i])
			}
			b.WriteString("| ")
			b.WriteString(strings.Join(parts, " | "))
			b.WriteString(" |")
		} else {
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	out := strings.TrimRight(b.String(), "\n")
	return Result{Output: out}, nil
}
