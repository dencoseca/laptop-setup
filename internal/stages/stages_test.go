package stages

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dencoseca/laptop-setup/internal/runner"
)

type recordingRunner struct {
	commands []runner.Command
}

func (r *recordingRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	return runner.Result{}, nil
}

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

func TestRunVitePlusInstallUsesViteChoice(t *testing.T) {
	runnerStub := &recordingRunner{}
	err := runVitePlusInstall(context.Background(), ExecutionContext{
		Runner: runnerStub,
		Decisions: map[string]any{
			DecisionNodeToolchain: NodeToolchainVitePlus,
		},
	})
	if err != nil {
		t.Fatalf("runVitePlusInstall returned error: %v", err)
	}

	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runnerStub.commands))
	}
	if got := runnerStub.commands[0].String(); !strings.Contains(got, "https://vite.plus") {
		t.Fatalf("expected vite installer command, got %q", got)
	}
}

func TestRunVitePlusInstallUsesNvmAndPnpmChoice(t *testing.T) {
	runnerStub := &recordingRunner{}
	err := runVitePlusInstall(context.Background(), ExecutionContext{
		Runner: runnerStub,
		Decisions: map[string]any{
			DecisionNodeToolchain: NodeToolchainNvmPnpm,
		},
	})
	if err != nil {
		t.Fatalf("runVitePlusInstall returned error: %v", err)
	}

	if len(runnerStub.commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(runnerStub.commands))
	}
	if got := runnerStub.commands[0].String(); !strings.Contains(got, "nvm-sh/nvm") {
		t.Fatalf("expected nvm command first, got %q", got)
	}
	if got := runnerStub.commands[1].String(); !strings.Contains(got, "get.pnpm.io") {
		t.Fatalf("expected pnpm command second, got %q", got)
	}
}

func TestRunShellSetupRespectsShellDecisions(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "zshrc"), []byte("new-zshrc\n"), 0o644); err != nil {
		t.Fatalf("write zshrc template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "starship.toml"), []byte("starship\n"), 0o644); err != nil {
		t.Fatalf("write starship template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("old-zshrc\n"), 0o644); err != nil {
		t.Fatalf("write existing zshrc: %v", err)
	}

	runnerStub := &recordingRunner{}
	err := runShellSetup(context.Background(), ExecutionContext{
		Runner:   runnerStub,
		RepoRoot: repoRoot,
		HomeDir:  homeDir,
		Decisions: map[string]any{
			DecisionShellInstallOhMyZsh: false,
			DecisionShellApplyZshrc:     true,
			DecisionShellApplyStarship:  false,
		},
	})
	if err != nil {
		t.Fatalf("runShellSetup returned error: %v", err)
	}

	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected no command execution, got %d commands", len(runnerStub.commands))
	}
	backupPath := filepath.Join(homeDir, ".zshrc.bak")
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected zshrc backup to exist: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(homeDir, ".zshrc")); err != nil {
		t.Fatalf("read resulting zshrc: %v", err)
	} else if string(content) != "new-zshrc\n" {
		t.Fatalf("unexpected zshrc contents: %q", string(content))
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".config", "starship.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no starship config when disabled, got err=%v", err)
	}
}

func TestRunGitConfigCustomIdentityWritesUserValues(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "gitignore"), []byte("*.tmp\n"), 0o644); err != nil {
		t.Fatalf("write gitignore template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "gitconfig"), []byte("[core]\n  autocrlf = input\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig template: %v", err)
	}

	err := runGitConfig(context.Background(), ExecutionContext{
		RepoRoot: repoRoot,
		HomeDir:  homeDir,
		Decisions: map[string]any{
			DecisionGitConfigMode: GitConfigModeCustom,
			DecisionGitUserName:   "Alice",
			DecisionGitUserEmail:  "alice@example.com",
		},
	})
	if err != nil {
		t.Fatalf("runGitConfig returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(homeDir, ".gitconfig"))
	if err != nil {
		t.Fatalf("read generated gitconfig: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, "name = Alice") {
		t.Fatalf("expected custom user.name in gitconfig, got %q", body)
	}
	if !strings.Contains(body, "email = alice@example.com") {
		t.Fatalf("expected custom user.email in gitconfig, got %q", body)
	}
}
