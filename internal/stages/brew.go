package stages

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var brewEntryPattern = regexp.MustCompile(`^\s*(brew|cask)\s+"([^"]+)"`)

type BrewEntry struct {
	Kind string
	ID   string
	Line string
}

func LoadBrewEntries(path string) ([]BrewEntry, error) {
	content, err := OSFileSystem{}.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

	return parseBrewEntries(string(content)), nil
}

func parseBrewEntries(content string) []BrewEntry {
	lines := strings.Split(string(content), "\n")
	entries := make([]BrewEntry, 0, len(lines))
	for _, line := range lines {
		matches := brewEntryPattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			continue
		}
		entries = append(entries, BrewEntry{
			Kind: matches[1],
			ID:   matches[2],
			Line: strings.TrimSpace(line),
		})
	}
	return entries
}

func GenerateBrewfile(execCtx ExecutionContext) (string, []string, error) {
	if strings.TrimSpace(execCtx.RunDir) == "" {
		return "", nil, errors.New("run directory is required to generate Brewfile")
	}
	store := execCtx.templateStore()
	entries, templatePath, err := store.LoadBrewEntries(defaultBrewTemplate)
	if err != nil {
		return "", nil, err
	}

	selectedSet := make(map[string]struct{}, len(execCtx.SelectedBrewIDs))
	for _, id := range execCtx.SelectedBrewIDs {
		selectedSet[id] = struct{}{}
	}

	selected := make([]BrewEntry, 0, len(entries))
	selectedIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(selectedSet) > 0 {
			if _, ok := selectedSet[entry.ID]; !ok {
				continue
			}
		}
		selected = append(selected, entry)
		selectedIDs = append(selectedIDs, entry.ID)
	}

	if len(selected) == 0 {
		return "", nil, errors.New("generated Brewfile would be empty")
	}

	path, err := store.WriteGeneratedBrewfile(execCtx.RunDir, templatePath, selected)
	if err != nil {
		return "", nil, err
	}

	return path, selectedIDs, nil
}
func ResolveSelectedBrewIDs(repoRoot string) ([]string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return nil, errors.New("repository root is required")
	}
	entries, err := LoadBrewEntries(filepath.Join(repoRoot, "templates", defaultBrewTemplate))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return slices.Compact(ids), nil
}
