package stages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/runner"
)

var brewEntryPattern = regexp.MustCompile(`^\s*(brew|cask)\s+"([^"]+)"`)

type Status string

const (
	StatusPending          Status = "pending"
	StatusRunning          Status = "running"
	StatusSuccess          Status = "success"
	StatusSkipped          Status = "skipped"
	StatusFailed           Status = "failed"
	StatusAlreadyDone      Status = "already_done"
	StatusSimulatedSuccess Status = "simulated_success"
)

const (
	DecisionEnvironment = "environment"
)

type CheckResult struct {
	Satisfied bool
	Message   string
}

type EventLogger interface {
	Log(event runner.Event) error
}

type ExecutionContext struct {
	DryRun                   bool
	Runner                   runner.CommandRunner
	Logger                   EventLogger
	RunID                    string
	Mode                     string
	StageID                  string
	Attempt                  int
	RunDir                   string
	RepoRoot                 string
	HomeDir                  string
	Environment              string
	SelectedBrewIDs          []string
	GeneratedBrewfilePath    string
	SetGeneratedBrewfilePath func(path string)
}

type CheckFunc func(context.Context, ExecutionContext) (CheckResult, error)
type RunFunc func(context.Context, ExecutionContext) error

type Stage struct {
	ID           string
	Title        string
	Description  string
	DecisionDeps []string
	CanSkip      bool
	Critical     bool
	LogTag       string
	Precheck     CheckFunc
	Run          RunFunc
	Simulate     RunFunc
}

type BrewEntry struct {
	Kind string
	ID   string
	Line string
}

func DefaultCatalog() []Stage {
	return []Stage{
		{
			ID:           "xcode_clt",
			Title:        "Xcode Command Line Tools",
			Description:  "Validate or install Xcode Command Line Tools",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     true,
			LogTag:       "xcode_clt",
			Precheck:     precheckXcodeCLT,
			Run:          runXcodeCLT,
			Simulate:     simulateXcodeCLT,
		},
		{
			ID:           "macos_defaults",
			Title:        "macOS Defaults",
			Description:  "Apply macOS defaults",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "macos_defaults",
			Precheck:     precheckNotSatisfied,
			Run:          runMacOSDefaults,
			Simulate:     simulateMacOSDefaults,
		},
		{
			ID:           "homebrew_install",
			Title:        "Homebrew Install",
			Description:  "Ensure Homebrew is installed",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     true,
			LogTag:       "homebrew_install",
			Precheck:     precheckHomebrew,
			Run:          runHomebrewInstall,
			Simulate:     simulateHomebrewInstall,
		},
		{
			ID:           "brew_bundle",
			Title:        "Brew Bundle",
			Description:  "Install selected packages and apps with brew bundle",
			DecisionDeps: []string{DecisionEnvironment},
			CanSkip:      true,
			Critical:     false,
			LogTag:       "brew_bundle",
			Precheck:     precheckNotSatisfied,
			Run:          runBrewBundle,
			Simulate:     simulateBrewBundle,
		},
		{
			ID:           "vite_plus_install",
			Title:        "Vite+ Install",
			Description:  "Set up Node toolchain",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "vite_plus_install",
			Precheck:     precheckNotSatisfied,
			Run:          runVitePlusInstall,
			Simulate:     simulateVitePlusInstall,
		},
		{
			ID:           "docker_config",
			Title:        "Docker Configuration",
			Description:  "Configure Docker runtime preferences",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "docker_config",
			Precheck:     precheckNotSatisfied,
			Run:          runDockerConfig,
			Simulate:     simulateDockerConfig,
		},
		{
			ID:           "shell_setup",
			Title:        "Shell Setup",
			Description:  "Configure shell environment",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "shell_setup",
			Precheck:     precheckNotSatisfied,
			Run:          runShellSetup,
			Simulate:     simulateShellSetup,
		},
		{
			ID:           "git_config",
			Title:        "Git Configuration",
			Description:  "Configure git identity and defaults",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "git_config",
			Precheck:     precheckNotSatisfied,
			Run:          runGitConfig,
			Simulate:     simulateGitConfig,
		},
		{
			ID:           "manual_app_store_apps",
			Title:        "Manual App Store Apps",
			Description:  "Display manual App Store install reminders",
			DecisionDeps: []string{DecisionEnvironment},
			CanSkip:      true,
			Critical:     false,
			LogTag:       "manual_app_store_apps",
			Precheck:     precheckNotSatisfied,
			Run:          runManualAppStoreApps,
			Simulate:     simulateManualAppStoreApps,
		},
	}
}

func IDs(catalog []Stage) []string {
	ids := make([]string, 0, len(catalog))
	for _, stage := range catalog {
		ids = append(ids, stage.ID)
	}
	return ids
}

func LoadBrewEntries(path string) ([]BrewEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

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
	return entries, nil
}

func GenerateBrewfile(execCtx ExecutionContext) (string, []string, error) {
	if strings.TrimSpace(execCtx.RunDir) == "" {
		return "", nil, errors.New("run directory is required to generate Brewfile")
	}
	templatePath, err := brewTemplatePath(execCtx)
	if err != nil {
		return "", nil, err
	}
	entries, err := LoadBrewEntries(templatePath)
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

	var builder strings.Builder
	builder.WriteString("# Generated by laptop-setup. Do not edit.\n")
	builder.WriteString(fmt.Sprintf("# Source: %s\n\n", templatePath))
	for _, entry := range selected {
		builder.WriteString(entry.Line)
		builder.WriteString("\n")
	}

	if err = os.MkdirAll(execCtx.RunDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create run directory: %w", err)
	}
	path := filepath.Join(execCtx.RunDir, "Brewfile.generated")
	if err = os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return "", nil, fmt.Errorf("write generated Brewfile: %w", err)
	}

	return path, selectedIDs, nil
}

func precheckNotSatisfied(context.Context, ExecutionContext) (CheckResult, error) {
	return CheckResult{Satisfied: false}, nil
}

func precheckXcodeCLT(ctx context.Context, execCtx ExecutionContext) (CheckResult, error) {
	if execCtx.Runner == nil {
		return CheckResult{}, errors.New("runner is required")
	}

	result, err := execCtx.Runner.Run(ctx, runner.Command{Name: "xcode-select", Args: []string{"-p"}})
	if err == nil {
		return CheckResult{Satisfied: true, Message: "Command Line Tools already installed"}, nil
	}
	if result.ExitCode >= 1 {
		return CheckResult{Satisfied: false}, nil
	}
	return CheckResult{}, err
}

func precheckHomebrew(context.Context, ExecutionContext) (CheckResult, error) {
	if _, err := exec.LookPath("brew"); err == nil {
		return CheckResult{Satisfied: true, Message: "Homebrew already installed"}, nil
	}
	return CheckResult{Satisfied: false}, nil
}

func runXcodeCLT(ctx context.Context, execCtx ExecutionContext) error {
	script := `set -e
touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
version=$(softwareupdate -l | grep "\*.*Command Line" | tail -n 1 | sed 's/^[^C]* //')
if [ -z "$version" ]; then
  echo "no matching Command Line Tools update found" >&2
  exit 1
fi
softwareupdate -i "$version" --verbose`
	return runCommand(ctx, execCtx, runner.Command{
		Name: "/bin/sh",
		Args: []string{"-c", script},
	})
}

func simulateXcodeCLT(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would check and install Xcode Command Line Tools via softwareupdate")
}

func runMacOSDefaults(ctx context.Context, execCtx ExecutionContext) error {
	commands := []runner.Command{
		{Name: "defaults", Args: []string{"write", "-g", "InitialKeyRepeat", "-int", "20"}},
		{Name: "defaults", Args: []string{"write", "-g", "KeyRepeat", "-int", "1"}},
		{Name: "defaults", Args: []string{"write", "-g", "AppleWindowTabbingMode", "-string", "always"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "autohide", "-bool", "true"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "tilesize", "-int", "60"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "show-recents", "-bool", "false"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "show-process-indicators", "-bool", "false"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "magnification", "-bool", "true"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "largesize", "-int", "70"}},
		{Name: "defaults", Args: []string{"write", "com.apple.dock", "windowtabbing", "-string", "always"}},
		{Name: "killall", Args: []string{"Dock"}},
		{Name: "defaults", Args: []string{"write", "com.apple.finder", "ShowPathbar", "-bool", "true"}},
		{Name: "defaults", Args: []string{"write", "com.apple.finder", "FXPreferredViewStyle", "-string", "clmv"}},
		{Name: "defaults", Args: []string{"write", "com.apple.finder", "_FXSortFoldersFirst", "-bool", "true"}},
		{Name: "defaults", Args: []string{"write", "com.apple.finder", "FXRemoveOldTrashItems", "-bool", "true"}},
		{Name: "defaults", Args: []string{"write", "com.apple.finder", "_FXSortFoldersFirstOnDesktop", "-bool", "true"}},
		{Name: "killall", Args: []string{"Finder"}},
		{Name: "defaults", Args: []string{"write", "com.apple.AppleMultitouchTrackpad", "FirstClickThreshold", "-int", "0"}},
		{Name: "defaults", Args: []string{"write", "com.apple.Siri", "StatusMenuVisible", "-bool", "false"}},
	}

	for _, command := range commands {
		if err := runCommand(ctx, execCtx, command); err != nil {
			if command.Name == "killall" {
				continue
			}
			return err
		}
	}
	return nil
}

func simulateMacOSDefaults(ctx context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would apply configured macOS defaults and restart Dock/Finder")
}

func runHomebrewInstall(ctx context.Context, execCtx ExecutionContext) error {
	if err := runCommand(ctx, execCtx, runner.Command{
		Name: "/bin/bash",
		Args: []string{"-c", "NONINTERACTIVE=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""},
	}); err != nil {
		return err
	}

	return ensureBrewShellenv(execCtx)
}

func simulateHomebrewInstall(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would run Homebrew installer and append brew shellenv to ~/.zprofile")
}

func runBrewBundle(ctx context.Context, execCtx ExecutionContext) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return logSimulation(execCtx, "brew not found, skipping brew bundle stage")
	}

	brewfilePath := execCtx.GeneratedBrewfilePath
	if strings.TrimSpace(brewfilePath) == "" {
		generatedPath, selectedIDs, err := GenerateBrewfile(execCtx)
		if err != nil {
			return err
		}
		brewfilePath = generatedPath
		if execCtx.SetGeneratedBrewfilePath != nil {
			execCtx.SetGeneratedBrewfilePath(generatedPath)
		}
		if len(execCtx.SelectedBrewIDs) == 0 && len(selectedIDs) > 0 {
			execCtx.SelectedBrewIDs = selectedIDs
		}
	}

	return runCommand(ctx, execCtx, runner.Command{
		Name: "brew",
		Args: []string{"bundle", "install", "--file", brewfilePath},
	})
}

func simulateBrewBundle(ctx context.Context, execCtx ExecutionContext) error {
	templatePath, err := brewTemplatePath(execCtx)
	if err != nil {
		return err
	}
	if err = logSimulation(execCtx, fmt.Sprintf("Would generate run-scoped Brewfile from %s", templatePath)); err != nil {
		return err
	}
	return logSimulation(execCtx, "Would run: brew bundle install --file <generated Brewfile>")
}

func runVitePlusInstall(ctx context.Context, execCtx ExecutionContext) error {
	return runCommand(ctx, execCtx, runner.Command{
		Name: "/bin/bash",
		Args: []string{"-c", "curl -fsSL https://vite.plus | bash"},
	})
}

func simulateVitePlusInstall(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would run Vite+ installer (curl https://vite.plus | bash)")
}

func runDockerConfig(_ context.Context, execCtx ExecutionContext) error {
	return copyFromTemplates(execCtx, "docker-config.json", filepath.Join(execCtx.HomeDir, ".docker", "config.json"))
}

func simulateDockerConfig(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would write templates/docker-config.json to ~/.docker/config.json")
}

func runShellSetup(ctx context.Context, execCtx ExecutionContext) error {
	if err := runCommand(ctx, execCtx, runner.Command{
		Name: "/bin/bash",
		Args: []string{"-c", "curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh | sh -s -- --unattended"},
	}); err != nil {
		return err
	}

	zshrcPath := filepath.Join(execCtx.HomeDir, ".zshrc")
	if _, err := os.Stat(zshrcPath); err == nil {
		if err = copyFile(zshrcPath, filepath.Join(execCtx.HomeDir, ".zshrc.bak")); err != nil {
			return err
		}
	}

	if err := copyFromTemplates(execCtx, "zshrc", zshrcPath); err != nil {
		return err
	}
	starshipPath := filepath.Join(execCtx.HomeDir, ".config", "starship.toml")
	if err := copyFromTemplates(execCtx, "starship.toml", starshipPath); err != nil {
		return err
	}
	return nil
}

func simulateShellSetup(_ context.Context, execCtx ExecutionContext) error {
	if err := logSimulation(execCtx, "Would install oh-my-zsh in unattended mode"); err != nil {
		return err
	}
	if err := logSimulation(execCtx, "Would back up ~/.zshrc to ~/.zshrc.bak when present"); err != nil {
		return err
	}
	return logSimulation(execCtx, "Would write templates/zshrc and templates/starship.toml to ~/.zshrc and ~/.config/starship.toml")
}

func runGitConfig(_ context.Context, execCtx ExecutionContext) error {
	if err := copyFromTemplates(execCtx, "gitignore", filepath.Join(execCtx.HomeDir, ".gitignore")); err != nil {
		return err
	}
	return copyFromTemplates(execCtx, "gitconfig", filepath.Join(execCtx.HomeDir, ".gitconfig"))
}

func simulateGitConfig(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would write templates/gitignore and templates/gitconfig to ~/.gitignore and ~/.gitconfig")
}

func runManualAppStoreApps(_ context.Context, execCtx ExecutionContext) error {
	items := []string{"Amphetamine", "Bear", "Bitwarden", "Things"}
	if strings.EqualFold(execCtx.Environment, "home") {
		items = append(items, "NordVPN")
	}
	return logStageMessage(execCtx, "Manual App Store apps: "+strings.Join(items, ", "))
}

func simulateManualAppStoreApps(_ context.Context, execCtx ExecutionContext) error {
	return runManualAppStoreApps(context.Background(), execCtx)
}

func runCommand(ctx context.Context, execCtx ExecutionContext, command runner.Command) error {
	if execCtx.Runner == nil {
		return errors.New("runner is required")
	}

	if execCtx.Logger != nil {
		if err := execCtx.Logger.Log(runner.Event{
			RunID:     execCtx.RunID,
			StageID:   execCtx.StageID,
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode,
			EventType: "command_started",
			Command:   command.String(),
		}); err != nil {
			return err
		}
	}

	result, err := execCtx.Runner.Run(ctx, command)
	if execCtx.Logger != nil {
		exitCode := result.ExitCode
		event := runner.Event{
			RunID:     execCtx.RunID,
			StageID:   execCtx.StageID,
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode,
			EventType: "command_completed",
			Command:   command.String(),
			ExitCode:  &exitCode,
		}
		if err != nil {
			event.Level = "error"
			event.Message = err.Error()
		} else {
			event.Message = "ok"
		}
		if logErr := execCtx.Logger.Log(event); logErr != nil {
			return logErr
		}
	}

	if err != nil {
		return fmt.Errorf("command failed (exit=%d): %s: %w", result.ExitCode, command.String(), err)
	}
	return nil
}

func logSimulation(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID,
		StageID:   execCtx.StageID,
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode,
		EventType: "simulation",
		Message:   message,
	})
}

func logStageMessage(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID,
		StageID:   execCtx.StageID,
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode,
		EventType: "stage_message",
		Message:   message,
	})
}

func ensureBrewShellenv(execCtx ExecutionContext) error {
	zprofilePath := filepath.Join(execCtx.HomeDir, ".zprofile")
	const sentinel = "brew shellenv"
	const shellenvLine = `eval "$(/opt/homebrew/bin/brew shellenv)"`
	const commentLine = "# Set PATH, MANPATH, etc., for Homebrew."

	content, err := os.ReadFile(zprofilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read .zprofile: %w", err)
	}
	if strings.Contains(string(content), sentinel) {
		return nil
	}

	if err = os.MkdirAll(filepath.Dir(zprofilePath), 0o755); err != nil {
		return fmt.Errorf("create .zprofile directory: %w", err)
	}

	file, err := os.OpenFile(zprofilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open .zprofile: %w", err)
	}
	defer file.Close()

	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		if _, err = file.WriteString("\n"); err != nil {
			return fmt.Errorf("write newline to .zprofile: %w", err)
		}
	}
	if _, err = file.WriteString(commentLine + "\n" + shellenvLine + "\n"); err != nil {
		return fmt.Errorf("append brew shellenv to .zprofile: %w", err)
	}
	return nil
}

func copyFromTemplates(execCtx ExecutionContext, sourceName, destination string) error {
	sourcePath := filepath.Join(execCtx.RepoRoot, "templates", sourceName)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	return copyFile(sourcePath, destination)
}

func copyFile(sourcePath, destinationPath string) error {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if err = os.WriteFile(destinationPath, payload, 0o644); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}

func brewTemplatePath(execCtx ExecutionContext) (string, error) {
	templateName, err := BrewTemplateName(execCtx.Environment)
	if err != nil {
		return "", err
	}
	return filepath.Join(execCtx.RepoRoot, "templates", templateName), nil
}

func BrewTemplateName(environment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "home":
		return "Brewfile.home", nil
	case "work":
		return "Brewfile.work", nil
	default:
		return "", fmt.Errorf("unsupported environment %q (valid: home, work)", environment)
	}
}

func ValidateEnvironment(environment string) error {
	_, err := BrewTemplateName(environment)
	return err
}

func ResolveSelectedBrewIDs(repoRoot, environment string) ([]string, error) {
	templateName, err := BrewTemplateName(environment)
	if err != nil {
		return nil, err
	}
	entries, err := LoadBrewEntries(filepath.Join(repoRoot, "templates", templateName))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return slices.Compact(ids), nil
}
