package stages

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLocalTemplateStoreContract(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := filepath.Join(t.TempDir(), "runs", "run-1")
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(strings.Join([]string{
		`brew "jq"`,
		`cask "warp"`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write Brewfile template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "zshrc"), []byte("export TEST=1\n"), 0o644); err != nil {
		t.Fatalf("write zshrc template: %v", err)
	}

	store := NewFilesystemTemplateStore(repoRoot, nil)
	entries, sourcePath, err := store.LoadBrewEntries("Brewfile")
	if err != nil {
		t.Fatalf("LoadBrewEntries returned error: %v", err)
	}
	if sourcePath != filepath.Join(templatesDir, "Brewfile") {
		t.Fatalf("source path mismatch: got=%q", sourcePath)
	}
	if gotIDs := []string{entries[0].ID, entries[1].ID}; !slices.Equal(gotIDs, []string{"jq", "warp"}) {
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
	if len(generatedEntries) != 1 || generatedEntries[0].ID != "jq" {
		t.Fatalf("generated entries mismatch: %+v", generatedEntries)
	}
	assertGeneratedBrewfileSource(t, generatedPath, sourcePath)
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

func TestEmbeddedTemplateStoreContract(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "runs", "run-1")
	store := FSTemplateStore{
		FS: fstest.MapFS{
			"Brewfile": {
				Data: []byte(strings.Join([]string{
					`brew "jq"`,
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
	if gotIDs := []string{entries[0].ID, entries[1].ID}; !slices.Equal(gotIDs, []string{"jq", "warp"}) {
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
	if len(generatedEntries) != 1 || generatedEntries[0].ID != "jq" {
		t.Fatalf("generated entries mismatch: %+v", generatedEntries)
	}
	assertGeneratedBrewfileSource(t, generatedPath, sourcePath)
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

func TestFSTemplateStoreWritesThroughFileSystemPort(t *testing.T) {
	fsys := &recordingFileSystem{}
	store := FSTemplateStore{
		FS: fstest.MapFS{
			"Brewfile": {
				Data: []byte(`brew "jq"` + "\n"),
			},
			"zshrc": {
				Data: []byte("export TEST=1\n"),
			},
		},
		SourceName: "test templates",
		FileSystem: fsys,
	}

	entries, sourcePath, err := store.LoadBrewEntries("Brewfile")
	if err != nil {
		t.Fatalf("LoadBrewEntries returned error: %v", err)
	}
	runDir := filepath.Join(t.TempDir(), "runs", "run-1")
	if _, err = store.WriteGeneratedBrewfile(runDir, sourcePath, entries); err != nil {
		t.Fatalf("WriteGeneratedBrewfile returned error: %v", err)
	}

	destination := filepath.Join(t.TempDir(), ".zshrc")
	if err = store.Copy("zshrc", destination); err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}

	if !slices.Contains(fsys.mkdirAllPaths, runDir) {
		t.Fatalf("expected generated Brewfile run directory to use FileSystem.MkdirAll, got %v", fsys.mkdirAllPaths)
	}
	if !slices.Contains(fsys.mkdirAllPaths, filepath.Dir(destination)) {
		t.Fatalf("expected copied template destination directory to use FileSystem.MkdirAll, got %v", fsys.mkdirAllPaths)
	}
	if len(fsys.writeFilePaths) < 2 {
		t.Fatalf("expected generated and copied templates to use FileSystem.WriteFile, got %v", fsys.writeFilePaths)
	}
	if len(fsys.renamePaths) < 2 {
		t.Fatalf("expected generated and copied templates to use FileSystem.Rename, got %v", fsys.renamePaths)
	}
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

func assertGeneratedBrewfileSource(t *testing.T, path string, sourcePath string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated Brewfile: %v", err)
	}
	want := "# Source: " + sourcePath + "\n"
	if !strings.Contains(string(content), want) {
		t.Fatalf("generated Brewfile source mismatch: want line %q in %q", want, content)
	}
}

type recordingFileSystem struct {
	OSFileSystem

	mkdirAllPaths  []string
	writeFilePaths []string
	renamePaths    []string
}

func (f *recordingFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	f.mkdirAllPaths = append(f.mkdirAllPaths, path)
	return f.OSFileSystem.MkdirAll(path, perm)
}

func (f *recordingFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	f.writeFilePaths = append(f.writeFilePaths, name)
	return f.OSFileSystem.WriteFile(name, data, perm)
}

func (f *recordingFileSystem) Rename(oldpath string, newpath string) error {
	f.renamePaths = append(f.renamePaths, newpath)
	return f.OSFileSystem.Rename(oldpath, newpath)
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
	satisfied, err := manager.BrewBundleSatisfied(context.Background(), ExecutionContext{Runner: runnerStub}, brewfilePath)
	if err != nil {
		t.Fatalf("BrewBundleSatisfied returned error: %v", err)
	}
	if !satisfied {
		t.Fatal("expected BrewBundleSatisfied to report satisfied")
	}
	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected one brew check command, got %d", len(runnerStub.commands))
	}
	command := runnerStub.commands[0]
	if command.Name != "/usr/local/bin/brew" || !slices.Equal(command.Args, []string{"bundle", "check", "--file", brewfilePath}) {
		t.Fatalf("unexpected brew check command: %s", command.String())
	}

	runnerStub.commands = nil
	if err := manager.RunBrewBundle(context.Background(), ExecutionContext{Runner: runnerStub}, brewfilePath); err != nil {
		t.Fatalf("RunBrewBundle returned error: %v", err)
	}
	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected one brew command, got %d", len(runnerStub.commands))
	}
	command = runnerStub.commands[0]
	if command.Name != "/usr/local/bin/brew" || !slices.Equal(command.Args, []string{"bundle", "install", "--file", brewfilePath}) {
		t.Fatalf("unexpected brew command: %s", command.String())
	}
}
