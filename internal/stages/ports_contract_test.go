package stages

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFilesystemTemplateStoreContract(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := filepath.Join(t.TempDir(), "runs", "run-1")
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(strings.Join([]string{
		`brew "go"`,
		`cask "warp"`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write Brewfile template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "zshrc"), []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write zshrc template: %v", err)
	}

	store := FilesystemTemplateStore{RepoRoot: repoRoot}
	entries, sourcePath, err := store.LoadBrewEntries("Brewfile")
	if err != nil {
		t.Fatalf("LoadBrewEntries returned error: %v", err)
	}
	if sourcePath != filepath.Join(templatesDir, "Brewfile") {
		t.Fatalf("source path mismatch: got=%q", sourcePath)
	}
	if gotIDs := []string{entries[0].ID, entries[1].ID}; !slices.Equal(gotIDs, []string{"go", "warp"}) {
		t.Fatalf("entry ids mismatch: %v", gotIDs)
	}

	payload, readPath, err := store.Read("zshrc")
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if readPath != filepath.Join(templatesDir, "zshrc") || string(payload) != "export TEST=1\n" {
		t.Fatalf("unexpected Read result: path=%q payload=%q", readPath, payload)
	}

	generatedPath, err := store.WriteGeneratedBrewfile(runDir, sourcePath, entries[:1])
	if err != nil {
		t.Fatalf("WriteGeneratedBrewfile returned error: %v", err)
	}
	generatedEntries, err := LoadBrewEntries(generatedPath)
	if err != nil {
		t.Fatalf("load generated Brewfile: %v", err)
	}
	if len(generatedEntries) != 1 || generatedEntries[0].ID != "go" {
		t.Fatalf("generated entries mismatch: %+v", generatedEntries)
	}
	assertPathPerm(t, runDir, 0o700)
	assertPathPerm(t, generatedPath, 0o600)

	destination := filepath.Join(t.TempDir(), ".zshrc")
	if err := store.Copy("zshrc", destination); err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	copied, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read copied template: %v", err)
	}
	if string(copied) != "export TEST=1\n" {
		t.Fatalf("copied template mismatch: %q", copied)
	}
	assertPathPerm(t, destination, 0o600)
}

func TestFSTemplateStoreContract(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "runs", "run-1")
	store := FSTemplateStore{
		FS: fstest.MapFS{
			"Brewfile": {
				Data: []byte(strings.Join([]string{
					`brew "go"`,
					`cask "warp"`,
					"",
				}, "\n")),
			},
			"zshrc": {
				Data: []byte("export TEST=1\n"),
			},
		},
		SourceName: "test templates",
	}

	entries, sourcePath, err := store.LoadBrewEntries("Brewfile")
	if err != nil {
		t.Fatalf("LoadBrewEntries returned error: %v", err)
	}
	if sourcePath != "test templates:Brewfile" {
		t.Fatalf("source path mismatch: got=%q", sourcePath)
	}
	if gotIDs := []string{entries[0].ID, entries[1].ID}; !slices.Equal(gotIDs, []string{"go", "warp"}) {
		t.Fatalf("entry ids mismatch: %v", gotIDs)
	}

	payload, readPath, err := store.Read("zshrc")
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if readPath != "test templates:zshrc" || string(payload) != "export TEST=1\n" {
		t.Fatalf("unexpected Read result: path=%q payload=%q", readPath, payload)
	}

	generatedPath, err := store.WriteGeneratedBrewfile(runDir, sourcePath, entries[:1])
	if err != nil {
		t.Fatalf("WriteGeneratedBrewfile returned error: %v", err)
	}
	generatedEntries, err := LoadBrewEntries(generatedPath)
	if err != nil {
		t.Fatalf("load generated Brewfile: %v", err)
	}
	if len(generatedEntries) != 1 || generatedEntries[0].ID != "go" {
		t.Fatalf("generated entries mismatch: %+v", generatedEntries)
	}
	assertPathPerm(t, runDir, 0o700)
	assertPathPerm(t, generatedPath, 0o600)

	destination := filepath.Join(t.TempDir(), ".zshrc")
	if err := store.Copy("zshrc", destination); err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	copied, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read copied template: %v", err)
	}
	if string(copied) != "export TEST=1\n" {
		t.Fatalf("copied template mismatch: %q", copied)
	}
	assertPathPerm(t, destination, 0o600)
}

func assertPathPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s: got=%#o want=%#o", path, got, want)
	}
}

func TestHomebrewPackageManagerContract(t *testing.T) {
	runnerStub := &recordingRunner{}
	manager := HomebrewPackageManager{Runner: runnerStub}

	if err := manager.HomebrewAvailable(context.Background()); err != nil {
		t.Fatalf("HomebrewAvailable returned error: %v", err)
	}
	if !slices.Equal(runnerStub.lookPathCalls, []string{"brew"}) {
		t.Fatalf("expected HomebrewAvailable to use LookPath for brew, got %v", runnerStub.lookPathCalls)
	}

	brewfilePath := filepath.Join(t.TempDir(), "Brewfile.generated")
	if err := manager.RunBrewBundle(context.Background(), ExecutionContext{Runner: runnerStub}, brewfilePath); err != nil {
		t.Fatalf("RunBrewBundle returned error: %v", err)
	}
	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected one brew command, got %d", len(runnerStub.commands))
	}
	command := runnerStub.commands[0]
	if command.Name != "/usr/local/bin/brew" || !slices.Equal(command.Args, []string{"bundle", "install", "--file", brewfilePath}) {
		t.Fatalf("unexpected brew command: %s", command.String())
	}
}
