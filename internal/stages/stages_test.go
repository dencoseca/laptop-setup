package stages

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadBrewEntries(t *testing.T) {
	brewfile := filepath.Join(t.TempDir(), "Brewfile")
	content := strings.Join([]string{
		`# comment`,
		`brew "go"`,
		`cask "warp"`,
		`tap "homebrew/cask"`,
		"",
	}, "\n")
	if err := os.WriteFile(brewfile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test Brewfile: %v", err)
	}

	entries, err := LoadBrewEntries(brewfile)
	if err != nil {
		t.Fatalf("load Brewfile entries: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Kind != "brew" || entries[0].ID != "go" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Kind != "cask" || entries[1].ID != "warp" {
		t.Fatalf("unexpected second entry: %+v", entries[1])
	}
}

func TestGenerateBrewfileUsesSelectedIDs(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}

	templatePath := filepath.Join(templatesDir, "Brewfile")
	content := strings.Join([]string{
		`brew "go"`,
		`brew "jq"`,
		`cask "warp"`,
		"",
	}, "\n")
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	path, selectedIDs, err := GenerateBrewfile(ExecutionContext{
		RepoRoot:        repoRoot,
		RunDir:          runDir,
		SelectedBrewIDs: []string{"warp", "go"},
	})
	if err != nil {
		t.Fatalf("generate Brewfile: %v", err)
	}

	if path != filepath.Join(runDir, "Brewfile.generated") {
		t.Fatalf("unexpected generated path: %s", path)
	}
	expectedSelected := []string{"go", "warp"}
	if !slices.Equal(selectedIDs, expectedSelected) {
		t.Fatalf("selected ids mismatch: got=%v want=%v", selectedIDs, expectedSelected)
	}

	generatedEntries, err := LoadBrewEntries(path)
	if err != nil {
		t.Fatalf("load generated Brewfile: %v", err)
	}
	if len(generatedEntries) != 2 {
		t.Fatalf("expected 2 generated entries, got %d", len(generatedEntries))
	}
	if generatedEntries[0].ID != "go" || generatedEntries[1].ID != "warp" {
		t.Fatalf("unexpected generated entry order: %+v", generatedEntries)
	}
}

func TestGenerateBrewfileRejectsEmptyOutput(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}

	templatePath := filepath.Join(templatesDir, "Brewfile")
	if err := os.WriteFile(templatePath, []byte(`brew "go"`+"\n"), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	_, _, err := GenerateBrewfile(ExecutionContext{
		RepoRoot:        repoRoot,
		RunDir:          runDir,
		SelectedBrewIDs: []string{"missing"},
	})
	if err == nil {
		t.Fatal("expected empty generated Brewfile error")
	}
	if !strings.Contains(err.Error(), "generated Brewfile would be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSelectedBrewIDs(t *testing.T) {
	repoRoot := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}

	templatePath := filepath.Join(templatesDir, "Brewfile")
	content := strings.Join([]string{
		`brew "go"`,
		`brew "jq"`,
		`brew "go"`,
		`cask "warp"`,
		"",
	}, "\n")
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	ids, err := ResolveSelectedBrewIDs(repoRoot)
	if err != nil {
		t.Fatalf("resolve selected brew ids: %v", err)
	}
	expected := []string{"go", "jq", "go", "warp"}
	if !slices.Equal(ids, expected) {
		t.Fatalf("selected id mismatch: got=%v want=%v", ids, expected)
	}
}
