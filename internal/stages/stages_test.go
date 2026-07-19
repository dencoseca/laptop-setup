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
	commands        []runner.Command
	lookPathCalls   []string
	lookPathResults map[string]string
	lookPathErrs    map[string]error
	lookPathErr     error
}

func (r *recordingRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	return runner.Result{}, nil
}

func (r *recordingRunner) LookPath(_ context.Context, name string) (string, error) {
	r.lookPathCalls = append(r.lookPathCalls, name)
	if err, ok := r.lookPathErrs[name]; ok {
		return "", err
	}
	if path, ok := r.lookPathResults[name]; ok {
		return path, nil
	}
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

type scriptedInteractiveRunner struct {
	commands []runner.Command
	result   runner.Result
	err      error
}

func (r *scriptedInteractiveRunner) RunInteractive(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	return r.result, r.err
}

type defaultsRunner struct {
	commands []runner.Command
	values   map[string]string
}

func (r *defaultsRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	if command.Name != "defaults" || len(command.Args) < 3 {
		return runner.Result{}, nil
	}
	action := command.Args[0]
	key := command.Args[1] + "\x00" + command.Args[2]
	switch action {
	case "read":
		value, ok := r.values[key]
		if !ok {
			return runner.Result{ExitCode: 1}, errors.New("not found")
		}
		return runner.Result{Stdout: value + "\n"}, nil
	case "write":
		r.values[key] = command.Args[4]
	}
	return runner.Result{}, nil
}

func (r *defaultsRunner) LookPath(_ context.Context, name string) (string, error) {
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
	checkPaths     []string
	checkSatisfied bool
	checkErr       error
	bundlePaths    []string
}

func (m *recordingPackageManager) HomebrewAvailable(context.Context) error {
	m.availableCalls++
	return nil
}

func (m *recordingPackageManager) BrewBundleSatisfied(_ context.Context, _ ExecutionContext, brewfilePath string) (bool, error) {
	m.checkPaths = append(m.checkPaths, brewfilePath)
	return m.checkSatisfied, m.checkErr
}

func (m *recordingPackageManager) RunBrewBundle(_ context.Context, _ ExecutionContext, brewfilePath string) error {
	m.bundlePaths = append(m.bundlePaths, brewfilePath)
	return nil
}

func TestLoadBrewEntries(t *testing.T) {
	brewfile := filepath.Join(t.TempDir(), "Brewfile")
	content := strings.Join([]string{
		`# comment`,
		`brew "jq"`,
		`cask "ghostty"`,
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
	if entries[0].Kind != "brew" || entries[0].ID != "jq" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Kind != "cask" || entries[1].ID != "ghostty" {
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
		`brew "ripgrep"`,
		`brew "jq"`,
		`cask "ghostty"`,
		"",
	}, "\n")
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	path, selectedIDs, err := GenerateBrewfile(ExecutionContext{
		RepoRoot:        repoRoot,
		RunDir:          runDir,
		SelectedBrewIDs: []string{"ghostty", "ripgrep"},
	})
	if err != nil {
		t.Fatalf("generate Brewfile: %v", err)
	}

	if path != filepath.Join(runDir, "Brewfile.generated") {
		t.Fatalf("unexpected generated path: %s", path)
	}
	expectedSelected := []string{"ripgrep", "ghostty"}
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
	if generatedEntries[0].ID != "ripgrep" || generatedEntries[1].ID != "ghostty" {
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
	if err := os.WriteFile(templatePath, []byte(`brew "jq"`+"\n"), 0o644); err != nil {
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
		`brew "ripgrep"`,
		`brew "jq"`,
		`cask "ghostty"`,
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
		SelectedBrewIDs: []string{"jq", "ghostty"},
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
	if command.Name != "/usr/local/bin/brew" {
		t.Fatalf("expected resolved brew command, got %q", command.Name)
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
	wantIDs := []string{"jq", "ghostty"}
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
	if !slices.Equal(runnerStub.lookPathCalls, []string{"brew", defaultAppleSiliconBrewPath}) {
		t.Fatalf("expected brew availability check through runner, got %v", runnerStub.lookPathCalls)
	}
}

func TestPrecheckHomebrewRequiresShellenvConfiguration(t *testing.T) {
	homeDir := t.TempDir()
	packages := &recordingPackageManager{}

	result, err := precheckHomebrew(context.Background(), ExecutionContext{
		HomeDir:        homeDir,
		PackageManager: packages,
	})
	if err != nil {
		t.Fatalf("precheckHomebrew returned error: %v", err)
	}
	if result.Satisfied {
		t.Fatal("expected Homebrew precheck not to be satisfied without shellenv")
	}

	if err := os.WriteFile(filepath.Join(homeDir, ".zprofile"), []byte(brewShellenvLine+"\n"), 0o600); err != nil {
		t.Fatalf("write .zprofile: %v", err)
	}
	result, err = precheckHomebrew(context.Background(), ExecutionContext{
		HomeDir:        homeDir,
		PackageManager: packages,
	})
	if err != nil {
		t.Fatalf("precheckHomebrew returned error after shellenv: %v", err)
	}
	if !result.Satisfied {
		t.Fatal("expected Homebrew precheck to be satisfied with shellenv")
	}
}

func TestPrecheckBrewBundleUsesInjectedPackageManager(t *testing.T) {
	brewfilePath := filepath.Join(t.TempDir(), "Brewfile.generated")
	packages := &recordingPackageManager{checkSatisfied: true}

	result, err := precheckBrewBundle(context.Background(), ExecutionContext{
		GeneratedBrewfilePath: brewfilePath,
		PackageManager:        packages,
	})
	if err != nil {
		t.Fatalf("precheckBrewBundle returned error: %v", err)
	}
	if !result.Satisfied {
		t.Fatal("expected Brew bundle precheck to be satisfied")
	}
	if packages.availableCalls != 1 {
		t.Fatalf("expected one availability check, got %d", packages.availableCalls)
	}
	if !slices.Equal(packages.checkPaths, []string{brewfilePath}) {
		t.Fatalf("expected injected package manager check path %q, got %v", brewfilePath, packages.checkPaths)
	}
}

func TestHomebrewPackageManagerFallsBackToAppleSiliconPath(t *testing.T) {
	fallbackPath := filepath.Join(t.TempDir(), "brew")
	runnerStub := &recordingRunner{
		lookPathErrs: map[string]error{
			"brew": errors.New("not found"),
		},
		lookPathResults: map[string]string{
			fallbackPath: fallbackPath,
		},
	}
	manager := HomebrewPackageManager{Runner: runnerStub, BrewPath: fallbackPath}

	resolved, err := manager.ResolveBrewPath(context.Background())
	if err != nil {
		t.Fatalf("ResolveBrewPath returned error: %v", err)
	}
	if resolved != fallbackPath {
		t.Fatalf("resolved path mismatch: got=%q want=%q", resolved, fallbackPath)
	}
	if !slices.Equal(runnerStub.lookPathCalls, []string{"brew", fallbackPath}) {
		t.Fatalf("look path calls mismatch: %v", runnerStub.lookPathCalls)
	}
}

func TestPrecheckBrewBundleUsesResolvedBrewPath(t *testing.T) {
	brewfilePath := filepath.Join(t.TempDir(), "Brewfile.generated")
	runnerStub := &recordingRunner{
		lookPathErrs: map[string]error{
			"brew": errors.New("not found"),
		},
		lookPathResults: map[string]string{
			defaultAppleSiliconBrewPath: defaultAppleSiliconBrewPath,
		},
	}

	result, err := precheckBrewBundle(context.Background(), ExecutionContext{
		Runner:                runnerStub,
		GeneratedBrewfilePath: brewfilePath,
	})
	if err != nil {
		t.Fatalf("precheckBrewBundle returned error: %v", err)
	}
	if !result.Satisfied {
		t.Fatalf("expected Brew bundle precheck to be satisfied")
	}
	if len(runnerStub.commands) != 1 {
		t.Fatalf("expected one brew bundle check, got %d", len(runnerStub.commands))
	}
	command := runnerStub.commands[0]
	if command.Name != defaultAppleSiliconBrewPath {
		t.Fatalf("expected resolved brew command, got %q", command.Name)
	}
	if !slices.Equal(command.Args, []string{"bundle", "check", "--file", brewfilePath}) {
		t.Fatalf("unexpected brew args: %v", command.Args)
	}
	if !slices.Equal(runnerStub.lookPathCalls, []string{"brew", defaultAppleSiliconBrewPath, "brew", defaultAppleSiliconBrewPath}) {
		t.Fatalf("expected brew availability and check to use fallback resolution, got %v", runnerStub.lookPathCalls)
	}
}

func TestRemoteInstallCommandDownloadsBeforeExecuting(t *testing.T) {
	command := remoteInstallCommand("https://example.com/install.sh", []string{"NONINTERACTIVE=1"}, "/bin/bash", "--flag")

	for _, want := range []string{
		"set -e",
		"curl -fsSL 'https://example.com/install.sh' -o \"$install_script\"",
		"NONINTERACTIVE=1 '/bin/bash' \"$install_script\" '--flag'",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected command to contain %q, got:\n%s", want, command)
		}
	}
	if strings.Contains(command, "|") {
		t.Fatalf("expected installer command not to pipe curl into a shell, got:\n%s", command)
	}
}

func TestRunBrewBundleUsesInjectedPackageManager(t *testing.T) {
	repoRoot := t.TempDir()
	runDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(`brew "jq"`+"\n"), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte(`brew "jq"`+"\n"), 0o644); err != nil {
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
		`brew "jq"`,
		`brew "ripgrep"`,
		`brew "jq"`,
		`cask "ghostty"`,
		"",
	}, "\n")
	if err := os.WriteFile(templatePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write template Brewfile: %v", err)
	}

	ids, err := ResolveSelectedBrewIDs(repoRoot)
	if err != nil {
		t.Fatalf("resolve selected brew ids: %v", err)
	}
	expected := []string{"jq", "ripgrep", "jq", "ghostty"}
	if !slices.Equal(ids, expected) {
		t.Fatalf("selected id mismatch: got=%v want=%v", ids, expected)
	}
}

func TestRunMacOSDefaultsSkipsMatchingDefaults(t *testing.T) {
	values := make(map[string]string, len(macOSDefaults))
	for _, setting := range macOSDefaults {
		values[setting.Domain+"\x00"+setting.Key] = setting.Value
	}
	runnerStub := &defaultsRunner{values: values}
	execCtx := ExecutionContext{Runner: runnerStub}

	check, err := precheckMacOSDefaults(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckMacOSDefaults returned error: %v", err)
	}
	if !check.Satisfied {
		t.Fatal("expected macOS defaults precheck to be satisfied")
	}

	if err := runMacOSDefaults(context.Background(), execCtx); err != nil {
		t.Fatalf("runMacOSDefaults returned error: %v", err)
	}
	for _, command := range runnerStub.commands {
		if command.Name == "defaults" && len(command.Args) > 0 && command.Args[0] == "write" {
			t.Fatalf("expected matching defaults not to be rewritten, got command %q", command.String())
		}
		if command.Name == "killall" {
			t.Fatalf("expected matching defaults not to restart processes, got command %q", command.String())
		}
	}
}

func TestRunMacOSDefaultsWritesDockNoBouncingWhenMissing(t *testing.T) {
	values := make(map[string]string, len(macOSDefaults))
	for _, setting := range macOSDefaults {
		if setting.Domain == "com.apple.dock" && setting.Key == "no-bouncing" {
			continue
		}
		values[setting.Domain+"\x00"+setting.Key] = setting.Value
	}
	runnerStub := &defaultsRunner{values: values}
	execCtx := ExecutionContext{Runner: runnerStub}

	check, err := precheckMacOSDefaults(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckMacOSDefaults returned error: %v", err)
	}
	if check.Satisfied {
		t.Fatal("expected macOS defaults precheck to detect missing Dock no-bouncing value")
	}

	if err := runMacOSDefaults(context.Background(), execCtx); err != nil {
		t.Fatalf("runMacOSDefaults returned error: %v", err)
	}

	if got := runnerStub.values["com.apple.dock\x00no-bouncing"]; got != "TRUE" {
		t.Fatalf("expected Dock no-bouncing value to be TRUE, got %q", got)
	}

	var wroteNoBouncing bool
	var restartedDock bool
	for _, command := range runnerStub.commands {
		switch command.String() {
		case "defaults write com.apple.dock no-bouncing -bool TRUE":
			wroteNoBouncing = true
		case "killall Dock":
			restartedDock = true
		}
	}
	if !wroteNoBouncing {
		t.Fatal("expected runMacOSDefaults to write Dock no-bouncing default")
	}
	if !restartedDock {
		t.Fatal("expected runMacOSDefaults to restart Dock after writing no-bouncing default")
	}
}

func TestRunNodeToolchainInstallUsesViteChoice(t *testing.T) {
	runnerStub := &recordingRunner{lookPathErr: errors.New("not found")}
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
	runnerStub := &recordingRunner{lookPathErr: errors.New("not found")}
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
	if err := os.WriteFile(filepath.Join(templatesDir, "ghostty.config"), []byte("ghostty\n"), 0o644); err != nil {
		t.Fatalf("write Ghostty template: %v", err)
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
			ShellApplyGhostty:   false,
			GitConfigMode:       GitConfigModeTemplate,
		},
	})
	if err != nil {
		t.Fatalf("runShellSetup returned error: %v", err)
	}

	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected no command execution, got %d commands", len(runnerStub.commands))
	}
	backups, err := filepath.Glob(filepath.Join(homeDir, ".zshrc.backup.*"))
	if err != nil {
		t.Fatalf("glob zshrc backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one timestamped zshrc backup, got %v", backups)
	}
	if content, err := os.ReadFile(filepath.Join(homeDir, ".zshrc")); err != nil {
		t.Fatalf("read resulting zshrc: %v", err)
	} else if string(content) != "new-zshrc\n" {
		t.Fatalf("unexpected zshrc contents: %q", string(content))
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".config", "starship.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no starship config when disabled, got err=%v", err)
	}
	if _, err := os.Stat(ghosttyConfigPath(homeDir)); !os.IsNotExist(err) {
		t.Fatalf("expected no Ghostty config when disabled, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".hushlogin")); err != nil {
		t.Fatalf("expected .hushlogin alongside zshrc template: %v", err)
	}
}

func TestGhosttyConfigPathUsesHomeConfigDirectory(t *testing.T) {
	homeDir := filepath.Join("Users", "alice")
	want := filepath.Join(homeDir, ".config", "ghostty", "config.ghostty")
	if got := ghosttyConfigPath(homeDir); got != want {
		t.Fatalf("ghosttyConfigPath() = %q, want %q", got, want)
	}
}

func TestRunShellSetupSecondRunSkipsInstalledToolsAndMatchingFiles(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(templatesDir, "ghostty.config"), []byte("ghostty\n"), 0o644); err != nil {
		t.Fatalf("write Ghostty template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("old-zshrc\n"), 0o644); err != nil {
		t.Fatalf("write existing zshrc: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".oh-my-zsh"), 0o755); err != nil {
		t.Fatalf("create existing oh-my-zsh dir: %v", err)
	}
	for _, plugin := range requiredOhMyZshPlugins {
		if err := os.MkdirAll(ohMyZshPluginPath(homeDir, plugin.Name), 0o755); err != nil {
			t.Fatalf("create existing plugin %s: %v", plugin.Name, err)
		}
	}

	runnerStub := &recordingRunner{}
	execCtx := ExecutionContext{
		Runner:   runnerStub,
		RepoRoot: repoRoot,
		HomeDir:  homeDir,
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			ShellApplyGhostty:   true,
			GitConfigMode:       GitConfigModeTemplate,
		},
	}

	if err := runShellSetup(context.Background(), execCtx); err != nil {
		t.Fatalf("first runShellSetup returned error: %v", err)
	}
	firstBackups := mustGlob(t, filepath.Join(homeDir, ".zshrc.backup.*"))
	if len(firstBackups) != 1 {
		t.Fatalf("expected one zshrc backup after first run, got %v", firstBackups)
	}
	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected installed oh-my-zsh to skip installer, got %d commands", len(runnerStub.commands))
	}

	if err := runShellSetup(context.Background(), execCtx); err != nil {
		t.Fatalf("second runShellSetup returned error: %v", err)
	}
	secondBackups := mustGlob(t, filepath.Join(homeDir, ".zshrc.backup.*"))
	if len(secondBackups) != 1 {
		t.Fatalf("expected second run not to create another backup, got %v", secondBackups)
	}
	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected second run not to install oh-my-zsh, got %d commands", len(runnerStub.commands))
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".hushlogin")); err != nil {
		t.Fatalf("expected .hushlogin after shell setup: %v", err)
	}
	if content, err := os.ReadFile(ghosttyConfigPath(homeDir)); err != nil {
		t.Fatalf("read Ghostty config: %v", err)
	} else if string(content) != "ghostty\n" {
		t.Fatalf("unexpected Ghostty config contents: %q", content)
	}

	check, err := precheckShellSetup(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckShellSetup returned error: %v", err)
	}
	if !check.Satisfied {
		t.Fatal("expected shell setup precheck to be satisfied after first run")
	}
}

func TestRunShellSetupInstallsMissingOhMyZshPlugins(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".oh-my-zsh"), 0o755); err != nil {
		t.Fatalf("create existing oh-my-zsh dir: %v", err)
	}

	execCtx := ExecutionContext{
		Runner:  &recordingRunner{},
		HomeDir: homeDir,
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			GitConfigMode:       GitConfigModeTemplate,
		},
	}

	check, err := precheckShellSetup(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckShellSetup returned error: %v", err)
	}
	if check.Satisfied {
		t.Fatal("expected missing shell plugins to leave precheck unsatisfied")
	}

	if err = runShellSetup(context.Background(), execCtx); err != nil {
		t.Fatalf("runShellSetup returned error: %v", err)
	}
	runnerStub := execCtx.Runner.(*recordingRunner)
	if len(runnerStub.commands) != len(requiredOhMyZshPlugins) {
		t.Fatalf("expected %d plugin clone commands, got %d", len(requiredOhMyZshPlugins), len(runnerStub.commands))
	}
	for index, plugin := range requiredOhMyZshPlugins {
		command := runnerStub.commands[index]
		if command.Name != "/usr/bin/git" {
			t.Fatalf("plugin %s command name = %q, want /usr/bin/git", plugin.Name, command.Name)
		}
		wantArgs := []string{"clone", "--depth", "1", plugin.URL, ohMyZshPluginPath(homeDir, plugin.Name)}
		if !slices.Equal(command.Args, wantArgs) {
			t.Fatalf("plugin %s command args = %v, want %v", plugin.Name, command.Args, wantArgs)
		}
	}
}

func TestRunNodeToolchainInstallSkipsInstalledNvmAndPnpm(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, ".nvm"), 0o755); err != nil {
		t.Fatalf("create existing nvm dir: %v", err)
	}

	runnerStub := &recordingRunner{}
	err := runNodeToolchainInstall(context.Background(), ExecutionContext{
		Runner:  runnerStub,
		HomeDir: homeDir,
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
	if len(runnerStub.commands) != 0 {
		t.Fatalf("expected no installer commands when nvm and pnpm are detected, got %d", len(runnerStub.commands))
	}

	check, err := precheckNodeToolchain(context.Background(), ExecutionContext{
		Runner:  runnerStub,
		HomeDir: homeDir,
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
		t.Fatalf("precheckNodeToolchain returned error: %v", err)
	}
	if !check.Satisfied {
		t.Fatal("expected node toolchain precheck to be satisfied")
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

func TestRunGitConfigOmitsBlankIdentityFields(t *testing.T) {
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
	if strings.Contains(body, "[user]") || strings.Contains(body, "name =") || strings.Contains(body, "email =") {
		t.Fatalf("expected blank git identity to be omitted, got %q", body)
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

func TestRunGitConfigSecondRunDoesNotRewriteMatchingFiles(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(homeDir, ".gitconfig"), []byte("[user]\n  name = Existing\n"), 0o644); err != nil {
		t.Fatalf("write existing gitconfig: %v", err)
	}

	execCtx := ExecutionContext{
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
	}
	if err := runGitConfig(context.Background(), execCtx); err != nil {
		t.Fatalf("first runGitConfig returned error: %v", err)
	}
	firstBackups := mustGlob(t, filepath.Join(homeDir, ".gitconfig.backup.*"))
	if len(firstBackups) != 1 {
		t.Fatalf("expected one gitconfig backup after first run, got %v", firstBackups)
	}

	if err := runGitConfig(context.Background(), execCtx); err != nil {
		t.Fatalf("second runGitConfig returned error: %v", err)
	}
	secondBackups := mustGlob(t, filepath.Join(homeDir, ".gitconfig.backup.*"))
	if len(secondBackups) != 1 {
		t.Fatalf("expected second run not to create another gitconfig backup, got %v", secondBackups)
	}

	check, err := precheckGitConfig(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckGitConfig returned error: %v", err)
	}
	if !check.Satisfied {
		t.Fatal("expected git config precheck to be satisfied after first run")
	}
}

func TestRunDockerConfigSecondRunDoesNotRewriteMatchingFile(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "docker-config.json"), []byte(`{"currentContext":"colima"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write docker config template: %v", err)
	}
	dockerDir := filepath.Join(homeDir, ".docker")
	if err := os.MkdirAll(dockerDir, 0o755); err != nil {
		t.Fatalf("create docker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dockerDir, "config.json"), []byte(`{"old":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("write existing docker config: %v", err)
	}

	execCtx := ExecutionContext{
		RepoRoot:   repoRoot,
		HomeDir:    homeDir,
		Decisions:  DefaultDecisions(),
		FileSystem: OSFileSystem{},
	}
	if err := runDockerConfig(context.Background(), execCtx); err != nil {
		t.Fatalf("first runDockerConfig returned error: %v", err)
	}
	firstBackups := mustGlob(t, filepath.Join(dockerDir, "config.json.backup.*"))
	if len(firstBackups) != 1 {
		t.Fatalf("expected one docker config backup after first run, got %v", firstBackups)
	}

	if err := runDockerConfig(context.Background(), execCtx); err != nil {
		t.Fatalf("second runDockerConfig returned error: %v", err)
	}
	secondBackups := mustGlob(t, filepath.Join(dockerDir, "config.json.backup.*"))
	if len(secondBackups) != 1 {
		t.Fatalf("expected second run not to create another docker config backup, got %v", secondBackups)
	}

	check, err := precheckDockerConfig(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("precheckDockerConfig returned error: %v", err)
	}
	if !check.Satisfied {
		t.Fatal("expected docker config precheck to be satisfied after first run")
	}
}

func TestDryRunSimulationsDoNotWriteUserConfig(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates dir: %v", err)
	}
	for name, payload := range map[string]string{
		"gitignore":      "*.tmp\n",
		"gitconfig":      "[core]\n  autocrlf = input\n",
		"zshrc":          "zshrc\n",
		"starship.toml":  "starship\n",
		"ghostty.config": "ghostty\n",
	} {
		if err := os.WriteFile(filepath.Join(templatesDir, name), []byte(payload), 0o644); err != nil {
			t.Fatalf("write template %s: %v", name, err)
		}
	}

	logger := &recordingEventLogger{}
	execCtx := ExecutionContext{
		DryRun:   true,
		Logger:   logger,
		RepoRoot: repoRoot,
		HomeDir:  homeDir,
		Decisions: DecisionSet{
			NodeToolchain:       NodeToolchainVitePlus,
			DockerRuntime:       DockerRuntimeColima,
			ShellInstallOhMyZsh: true,
			ShellApplyZshrc:     true,
			ShellApplyStarship:  true,
			ShellApplyGhostty:   true,
			GitConfigMode:       GitConfigModeTemplate,
			GitUserName:         "Ada",
			GitUserEmail:        "ada@example.com",
		},
	}
	if err := simulateShellSetup(context.Background(), execCtx); err != nil {
		t.Fatalf("simulateShellSetup returned error: %v", err)
	}
	if err := simulateGitConfig(context.Background(), execCtx); err != nil {
		t.Fatalf("simulateGitConfig returned error: %v", err)
	}

	for _, path := range []string{
		filepath.Join(homeDir, ".zshrc"),
		filepath.Join(homeDir, ".config", "starship.toml"),
		ghosttyConfigPath(homeDir),
		filepath.Join(homeDir, ".hushlogin"),
		filepath.Join(homeDir, ".gitignore"),
		filepath.Join(homeDir, ".gitconfig"),
		filepath.Join(homeDir, ".oh-my-zsh"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected dry-run simulation not to create %s, stat err=%v", path, err)
		}
	}
	if len(logger.events) == 0 {
		t.Fatal("expected dry-run simulation events")
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

func TestRunCommandUsesInteractiveRunnerForInteractiveCommand(t *testing.T) {
	command := runner.Command{Name: "brew", Args: []string{"bundle", "install"}, Interactive: true}
	captured := &scriptedRunner{}
	interactive := &scriptedInteractiveRunner{
		result: runner.Result{ExitCode: 0},
	}
	logger := &recordingEventLogger{}
	execCtx := ExecutionContext{
		Runner:            captured,
		InteractiveRunner: interactive,
		Logger:            logger,
		RunID:             "run-123",
		StageID:           "brew_bundle",
		Attempt:           1,
		Mode:              "normal",
	}

	if err := runCommand(context.Background(), execCtx, command); err != nil {
		t.Fatalf("runCommand returned error: %v", err)
	}

	if len(captured.commands) != 0 {
		t.Fatalf("expected captured runner not to run interactive command, got %d commands", len(captured.commands))
	}
	if len(interactive.commands) != 1 {
		t.Fatalf("expected one interactive command, got %d", len(interactive.commands))
	}
	if !interactive.commands[0].Interactive {
		t.Fatal("expected interactive command marker to be preserved")
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

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return matches
}
