package server

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// skillsSubdir is where bundled skills live, relative to the workspace root.
const skillsSubdir = ".config/opencode/skills"

var (
	skillIndexMu    sync.Mutex
	skillIndexCache = map[string]string{}
)

// loadSkillIndex scans <workdir>/.config/opencode/skills for SKILL.md files,
// extracts each skill's name + description, and returns a compact index block
// for the system prompt. Result is cached per workdir. Returns "" when no
// skills directory exists or no skills have descriptions.
func loadSkillIndex(workdir string) string {
	skillIndexMu.Lock()
	defer skillIndexMu.Unlock()
	if cached, ok := skillIndexCache[workdir]; ok {
		return cached
	}
	index := buildSkillIndex(workdir)
	skillIndexCache[workdir] = index
	return index
}

func buildSkillIndex(workdir string) string {
	root := filepath.Join(workdir, skillsSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	type skill struct{ name, desc string }
	var skills []skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, desc := readSkillFrontmatter(filepath.Join(root, e.Name(), "SKILL.md"))
		if name == "" {
			name = e.Name()
		}
		if desc == "" {
			continue
		}
		skills = append(skills, skill{name: name, desc: desc})
	}
	if len(skills) == 0 {
		return ""
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].name < skills[j].name })
	var b strings.Builder
	b.WriteString("<available_skills>\n")
	b.WriteString("Specialized skills you can self-equip. When a task matches a skill, read its file ")
	b.WriteString(skillsSubdir)
	b.WriteString("/<name>/SKILL.md FIRST (with your read tool, relative path) and follow it; do not narrate that you are loading it.\n")
	for _, s := range skills {
		b.WriteString("- ")
		b.WriteString(s.name)
		b.WriteString(": ")
		b.WriteString(s.desc)
		b.WriteString("\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// readSkillFrontmatter reads name+description from the YAML frontmatter of a
// SKILL.md. Only the first frontmatter block is consulted.
func readSkillFrontmatter(path string) (name, desc string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFront := false
	lineNo := 0
	for sc.Scan() {
		line := sc.Text()
		lineNo++
		trimmed := strings.TrimSpace(line)
		if lineNo == 1 && trimmed == "---" {
			inFront = true
			continue
		}
		if inFront && trimmed == "---" {
			break
		}
		if !inFront {
			if lineNo > 1 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return name, truncateSkillDesc(desc)
}

func truncateSkillDesc(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	const max = 160
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}
