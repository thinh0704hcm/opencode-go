//go:build opencode_wip

package server

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// TableMarkdown formats a slice of structs into a markdown table.
func TableMarkdown[T any](rows []T) string {
	if len(rows) == 0 {
		return ""
	}
	// Ensure element is struct
	v := reflect.ValueOf(rows[0])
	if v.Kind() != reflect.Struct {
		return ""
	}
	t := v.Type()
	// Header
	var headers []string
	for i := 0; i < t.NumField(); i++ {
		headers = append(headers, t.Field(i).Name)
	}
	// Separator line of ---
	separators := make([]string, len(headers))
	for i := range separators {
		separators[i] = "---"
	}
	// Determine max rows
	maxRows := 1000
	if env := os.Getenv("TABLE_FORMAT_MAX_ROWS"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n > 0 {
			maxRows = n
		}
	}
	limit := len(rows)
	if limit > maxRows {
		limit = maxRows
	}
	var sb strings.Builder
	sb.WriteString(strings.Join(headers, "|"))
	sb.WriteString("\n")
	sb.WriteString(strings.Join(separators, "|"))
	sb.WriteString("\n")
	for i := 0; i < limit; i++ {
		rv := reflect.ValueOf(rows[i])
		var cells []string
		for j := 0; j < rv.NumField(); j++ {
			cell := fmt.Sprint(rv.Field(j).Interface())
			// Escape pipe characters to avoid breaking table
			cell = strings.ReplaceAll(cell, "|", "\\|")
			cells = append(cells, cell)
		}
		sb.WriteString(strings.Join(cells, "|"))
		if i < limit-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
