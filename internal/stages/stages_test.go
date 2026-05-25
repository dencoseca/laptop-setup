package stages

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dencoseca/laptop-setup/internal/runner"
)

type recordingRunner struct {
	commands      []runner.Command
	lookPathCalls []string
	lookPathErr   error
}

func (r *recordingRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	return runner.Result{}, nil
}

func (r *recordingRunner) LookPath(_ context.Context, name string) (string, error) {
	r.lookPathCalls = append(r.lookPathCalls, name)
	if r.lookPathErr != nil {
		return "", r.lookPathErr
	}
	return filepath.Join("/usr/local/bin", name), nil
}

type scriptedRunner struct {
	commands []runner.Command
	result   runner.Result
	err      error
}

func (r *scriptedRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	return r.result, r.err
}

func (r *scriptedRunner) LookPath(_ context.Context, name string) (string, error) {
	return filepath.Join("/usr/local/bin", name), nil
}

type recordingEventLogger struct {
	events []runner.Event
}

func (l *recordingEventLogger) Log(event runner.Event) error {
	l.events = append(l.events, event)
	return nil
}

type recordingPackageManager struct {
	availableCalls int
	bundlePaths    []string
}

func (m *recordingPackageManager) HomebrewAvailable(context.Context) error {
	m.availableCalls++
	return nil
}

func (m *recordingPackageManager) RunBrewBundle(_ context.Context, _ ExecutionContext, brewfilePath string) error {
	m.bundlePaths = append(m.bundlePaths, brewfilePath)
	return nil
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

func TestRunBrewBundleUsesGeneratedBrewfileMatchingSelectedEntries(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(strings.Join([]string{
		`brew "go"`,
		`brew "jq"`,
		`cask "warp"`,
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	brewPath := filepath.Join(binDir, "brew")
	if err := os.WriteFile(brewPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake brew binary: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	runnerStub := &recordingRunner{}
	generatedPath := ""
	err := runBrewBundle(context.Background(), ExecutionContext{
		Runner:          runnerStub,
		RepoRoot:        repoRoot,
		RunDir:          runDir,
		SelectedBrewIDs: []string{"jq", "warp"},
		SetGeneratedBrewfilePath: func(path string) {
			generatedPath = path
		},
	})
	if err != nil {
		t.Fatalf("runBrewBundle returned error: %v", err)
	}
	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected one brew invocation, got %d", len(runnerStub.commands))
	}
	command := runnerStub.commands[0]
	if command.Name != "brew" {
		t.Fatalf("expected brew command, got %q", command.Name)
	}
	if len(command.Args) != 4 || command.Args[0] != "bundle" || command.Args[1] != "install" || command.Args[2] != "--file" {
		t.Fatalf("unexpected brew args: %v", command.Args)
	}
	if generatedPath == "" {
		t.Fatal("expected generated Brewfile path to be recorded")
	}
	if command.Args[3] != generatedPath {
		t.Fatalf("command file path mismatch: got=%q want=%q", command.Args[3], generatedPath)
	}

	entries, err := LoadBrewEntries(generatedPath)
	if err != nil {
		t.Fatalf("load generated Brewfile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 generated entries, got %d", len(entries))
	}
	gotIDs := []string{entries[0].ID, entries[1].ID}
	wantIDs := []string{"jq", "warp"}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("generated brew entries mismatch: got=%v want=%v", gotIDs, wantIDs)
	}
}

func TestRunBrewBundleFailsWhenBrewMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	runnerStub := &recordingRunner{lookPathErr: errors.New("not found")}
	err := runBrewBundle(context.Background(), ExecutionContext{
		Runner: runnerStub,
	})
	if err == nil {
		t.Fatal("expected runBrewBundle to fail when brew is missing")
	}
	if !strings.Contains(err.Error(), "brew executable not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected no command execution when brew is missing, got %d commands", len(runnerStub.commands))
	}
	if !slices.Equal(runnerStub.lookPathCalls, []string{"brew"}) {
		t.Fatalf("expected brew availability check through runner, got %v", runnerStub.lookPathCalls)
	}
}

func TestRunBrewBundleUsesInjectedPackageManager(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(`brew "go"`+"\n"), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	packages := &recordingPackageManager{}
	generatedPath := ""
	err := runBrewBundle(context.Background(), ExecutionContext{
		RepoRoot:       repoRoot,
		RunDir:         runDir,
		PackageManager: packages,
		SetGeneratedBrewfilePath: func(path string) {
			generatedPath = path
		},
	})
	if err != nil {
		t.Fatalf("runBrewBundle returned error: %v", err)
	}
	if packages.availableCalls != 1 {
		t.Fatalf("expected one availability check, got %d", packages.availableCalls)
	}
	if len(packages.bundlePaths) != 1 {
		t.Fatalf("expected one bundle execution, got %d", len(packages.bundlePaths))
	}
	if generatedPath == "" || packages.bundlePaths[0] != generatedPath {
		t.Fatalf("bundle path mismatch: generated=%q bundle=%v", generatedPath, packages.bundlePaths)
	}
}

func TestSimulateBrewBundleDoesNotRequireBrew(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(`brew "go"`+"\n"), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	logger := &recordingEventLogger{}
	err := simulateBrewBundle(context.Background(), ExecutionContext{
		Logger:   logger,
		RepoRoot: repoRoot,
		RunDir:   runDir,
	})
	if err != nil {
		t.Fatalf("simulateBrewBundle returned error: %v", err)
	}
	if len(logger.events) != 2 {
		t.Fatalf("expected 2 simulation events, got %d", len(logger.events))
	}
	if _, err := os.Stat(filepath.Join(runDir, "Brewfile.generated")); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run simulation not to generate Brewfile, stat err=%v", err)
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

func TestRunNodeToolchainInstallUsesViteChoice(t *testing.T) {
	runnerStub := &recordingRunner{}
	err := runNodeToolchainInstall(context.Background(), ExecutionContext{
		Runner:    runnerStub,
		Decisions: DefaultDecisions(),
	})
	if err != nil {
		t.Fatalf("runNodeToolchainInstall returned error: %v", err)
	}

	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runnerStub.commands))
	}
	if got := runnerStub.commands[0].String(); !strings.Contains(got, "https://vite.plus") {
		t.Fatalf("expected vite installer command, got %q", got)
	}
}

func TestRunNodeToolchainInstallUsesNvmAndPnpmChoice(t *testing.T) {
	runnerStub := &recordingRunner{}
	err := runNodeToolchainInstall(context.Background(), ExecutionContext{
		Runner: runnerStub,
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainNvmPnpm,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			GitConfigMode:       GitConfigModeTemplate,
		},
	})
	if err != nil {
		t.Fatalf("runNodeToolchainInstall returned error: %v", err)
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
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: false,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  false,
			GitConfigMode:       GitConfigModeTemplate,
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
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			GitConfigMode:       GitConfigModeCustom,
			GitUserName:         "Alice",
			GitUserEmail:        "alice@example.com",
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

func TestRunGitConfigTemplateWritesEnteredIdentity(t *testing.T) {
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
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			GitConfigMode:       GitConfigModeTemplate,
			GitUserName:         "Ada",
			GitUserEmail:        "ada@example.com",
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
	if !strings.Contains(body, "name = Ada") {
		t.Fatalf("expected entered user.name in gitconfig, got %q", body)
	}
	if !strings.Contains(body, "email = ada@example.com") {
		t.Fatalf("expected entered user.email in gitconfig, got %q", body)
	}
}

func TestRunGitConfigOverwritesExistingGitConfig(t *testing.T) {
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
	existing := "[user]\n  name = Existing\n  email = existing@example.com\n"
	if err := os.WriteFile(filepath.Join(homeDir, ".gitconfig"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing gitconfig: %v", err)
	}

	err := runGitConfig(context.Background(), ExecutionContext{
		RepoRoot: repoRoot,
		HomeDir:  homeDir,
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			GitConfigMode:       GitConfigModeTemplate,
			GitUserName:         "Grace",
			GitUserEmail:        "grace@example.com",
		},
	})
	if err != nil {
		t.Fatalf("runGitConfig returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(homeDir, ".gitconfig"))
	if err != nil {
		t.Fatalf("read gitconfig: %v", err)
	}
	body := string(content)
	if body == existing {
		t.Fatal("expected existing gitconfig to be overwritten")
	}
	if !strings.Contains(body, "name = Grace") {
		t.Fatalf("expected entered user.name in gitconfig, got %q", body)
	}
	if !strings.Contains(body, "email = grace@example.com") {
		t.Fatalf("expected entered user.email in gitconfig, got %q", body)
	}
}

func TestRunCommandLogsOutputAndLifecycleOnSuccess(t *testing.T) {
	command := runner.Command{Name: "echo", Args: []string{"hello"}}
	run := &scriptedRunner{
		result: runner.Result{
			ExitCode: 0,
			Stdout:   "line one\nline two\n",
			Stderr:   "warn one\n",
		},
	}
	logger := &recordingEventLogger{}
	execCtx := ExecutionContext{
		Runner:  run,
		Logger:  logger,
		RunID:   "run-123",
		StageID: "brew_bundle",
		Attempt: 2,
		Mode:    "normal",
	}

	if err := runCommand(context.Background(), execCtx, command); err != nil {
		t.Fatalf("runCommand returned error: %v", err)
	}

	if len(logger.events) != 5 {
		t.Fatalf("expected 5 log events, got %d", len(logger.events))
	}

	expectedTypes := []runner.EventType{
		runner.EventTypeCommandStarted,
		runner.EventTypeCommandStdout,
		runner.EventTypeCommandStdout,
		runner.EventTypeCommandStderr,
		runner.EventTypeCommandCompleted,
	}
	for idx, event := range logger.events {
		if event.EventType != expectedTypes[idx] {
			t.Fatalf("event %d type mismatch: got=%s want=%s", idx, event.EventType, expectedTypes[idx])
		}
		if event.StageID != execCtx.StageID.String() {
			t.Fatalf("event %d stage mismatch: got=%s want=%s", idx, event.StageID, execCtx.StageID)
		}
		if event.Attempt != execCtx.Attempt {
			t.Fatalf("event %d attempt mismatch: got=%d want=%d", idx, event.Attempt, execCtx.Attempt)
		}
	}

	if got := logger.events[1].Message; got != "line one" {
		t.Fatalf("unexpected stdout line 1: %q", got)
	}
	if got := logger.events[2].Message; got != "line two" {
		t.Fatalf("unexpected stdout line 2: %q", got)
	}
	if got := logger.events[3].Message; got != "warn one" {
		t.Fatalf("unexpected stderr line: %q", got)
	}
	if got := logger.events[3].Level; got != "warn" {
		t.Fatalf("expected stderr event level warn, got %q", got)
	}
	completed := logger.events[4]
	if completed.Command != command.String() {
		t.Fatalf("command mismatch in completion event: got=%q want=%q", completed.Command, command.String())
	}
	if completed.ExitCode == nil || *completed.ExitCode != 0 {
		t.Fatalf("expected completion exit code 0, got %+v", completed.ExitCode)
	}
	if completed.Message != "ok" {
		t.Fatalf("expected completion message ok, got %q", completed.Message)
	}
}

func TestRunCommandLogsOutputAndFailureContext(t *testing.T) {
	command := runner.Command{Name: "brew", Args: []string{"bundle", "install"}}
	run := &scriptedRunner{
		result: runner.Result{
			ExitCode: 7,
			Stdout:   "before failure\n",
			Stderr:   "fatal details\n",
		},
		err: errors.New("exit status 7"),
	}
	logger := &recordingEventLogger{}
	execCtx := ExecutionContext{
		Runner:  run,
		Logger:  logger,
		RunID:   "run-123",
		StageID: "brew_bundle",
		Attempt: 3,
		Mode:    "normal",
	}

	err := runCommand(context.Background(), execCtx, command)
	if err == nil {
		t.Fatal("expected runCommand error")
	}
	if !strings.Contains(err.Error(), "command failed (exit=7): brew bundle install") {
		t.Fatalf("expected contextual failure error, got %v", err)
	}

	if len(logger.events) != 4 {
		t.Fatalf("expected 4 log events, got %d", len(logger.events))
	}
	completed := logger.events[3]
	if completed.EventType != runner.EventTypeCommandCompleted {
		t.Fatalf("expected command_completed event, got %q", completed.EventType)
	}
	if completed.Level != "error" {
		t.Fatalf("expected command_completed error level, got %q", completed.Level)
	}
	if completed.Command != command.String() {
		t.Fatalf("completion command mismatch: got=%q want=%q", completed.Command, command.String())
	}
	if completed.ExitCode == nil || *completed.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %+v", completed.ExitCode)
	}
	if !strings.Contains(completed.Message, "exit status 7") {
		t.Fatalf("expected error details in completion message, got %q", completed.Message)
	}
}
