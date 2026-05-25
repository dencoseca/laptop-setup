package stages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/domain/setup"
	"github.com/dencoseca/laptop-setup/internal/runner"
)

var brewEntryPattern = regexp.MustCompile(`^\s*(brew|cask)\s+"([^"]+)"`)

type StageID = setup.StageID
type Status = setup.StageStatus

const (
	StatusPending          Status = setup.StageStatusPending
	StatusRunning          Status = setup.StageStatusRunning
	StatusSuccess          Status = setup.StageStatusSuccess
	StatusSkipped          Status = setup.StageStatusSkipped
	StatusFailed           Status = setup.StageStatusFailed
	StatusAlreadyDone      Status = setup.StageStatusAlreadyDone
	StatusSimulatedSuccess Status = setup.StageStatusSimulatedSuccess
)

const (
	defaultBrewTemplate = "Brewfile"
)

var manualAppStoreApps = []string{
	"Amphetamine",
	"Bear",
	"Bitwarden",
	"Things",
	"NordVPN",
}

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
	RunID                    setup.RunID
	Mode                     setup.Mode
	StageID                  StageID
	Attempt                  int
	RunDir                   string
	RepoRoot                 string
	HomeDir                  string
	FileSystem               FileSystem
	TemplateStore            TemplateStore
	PackageManager           PackageManager
	Decisions                DecisionSet
	SelectedBrewIDs          []string
	GeneratedBrewfilePath    string
	SetGeneratedBrewfilePath func(path string)
}

type CheckFunc func(context.Context, ExecutionContext) (CheckResult, error)
type RunFunc func(context.Context, ExecutionContext) error

type Stage struct {
	ID           StageID
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

func ManualAppStoreApps() []string {
	return append([]string(nil), manualAppStoreApps...)
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
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "brew_bundle",
			Precheck:     precheckNotSatisfied,
			Run:          runBrewBundle,
			Simulate:     simulateBrewBundle,
		},
		{
			ID:           "node_toolchain",
			Title:        "Node Toolchain",
			Description:  "Set up Node toolchain",
			DecisionDeps: []string{DecisionNodeToolchain},
			CanSkip:      true,
			Critical:     false,
			LogTag:       "node_toolchain",
			Precheck:     precheckNotSatisfied,
			Run:          runNodeToolchainInstall,
			Simulate:     simulateNodeToolchainInstall,
		},
		{
			ID:           "docker_config",
			Title:        "Docker Configuration",
			Description:  "Configure Docker runtime preferences",
			DecisionDeps: []string{DecisionDockerRuntime},
			CanSkip:      true,
			Critical:     false,
			LogTag:       "docker_config",
			Precheck:     precheckNotSatisfied,
			Run:          runDockerConfig,
			Simulate:     simulateDockerConfig,
		},
		{
			ID:          "shell_setup",
			Title:       "Shell Setup",
			Description: "Configure shell environment",
			DecisionDeps: []string{
				DecisionShellInstallOhMyZsh,
				DecisionShellApplyZshrc,
				DecisionShellApplyStarship,
			},
			CanSkip:  true,
			Critical: false,
			LogTag:   "shell_setup",
			Precheck: precheckNotSatisfied,
			Run:      runShellSetup,
			Simulate: simulateShellSetup,
		},
		{
			ID:           "git_config",
			Title:        "Git Configuration",
			Description:  "Configure git identity and defaults",
			DecisionDeps: []string{DecisionGitConfigMode, DecisionGitUserName, DecisionGitUserEmail},
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
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			LogTag:       "manual_app_store_apps",
			Precheck:     precheckNotSatisfied,
			Run:          runManualAppStoreApps,
			Simulate:     simulateManualAppStoreApps,
		},
	}
}

func IDs(catalog []Stage) []StageID {
	ids := make([]StageID, 0, len(catalog))
	for _, stage := range catalog {
		ids = append(ids, stage.ID)
	}
	return ids
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

func precheckHomebrew(ctx context.Context, execCtx ExecutionContext) (CheckResult, error) {
	if err := execCtx.packageManager().HomebrewAvailable(ctx); err == nil {
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
	packageManager := execCtx.packageManager()
	if err := packageManager.HomebrewAvailable(ctx); err != nil {
		return err
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

	return packageManager.RunBrewBundle(ctx, execCtx, brewfilePath)
}

func simulateBrewBundle(ctx context.Context, execCtx ExecutionContext) error {
	_, templatePath, err := execCtx.templateStore().LoadBrewEntries(defaultBrewTemplate)
	if err != nil {
		return err
	}
	if err = logSimulation(execCtx, fmt.Sprintf("Would generate run-scoped Brewfile from %s", templatePath)); err != nil {
		return err
	}
	return logSimulation(execCtx, "Would run: brew bundle install --file <generated Brewfile>")
}

func runNodeToolchainInstall(ctx context.Context, execCtx ExecutionContext) error {
	switch NodeToolchainFromDecisions(execCtx.Decisions) {
	case NodeToolchainNvmPnpm:
		if err := runCommand(ctx, execCtx, runner.Command{
			Name: "/bin/bash",
			Args: []string{"-c", "curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash"},
		}); err != nil {
			return err
		}
		return runCommand(ctx, execCtx, runner.Command{
			Name: "/bin/bash",
			Args: []string{"-c", "curl -fsSL https://get.pnpm.io/install.sh | sh -"},
		})
	default:
		return runCommand(ctx, execCtx, runner.Command{
			Name: "/bin/bash",
			Args: []string{"-c", "curl -fsSL https://vite.plus | bash"},
		})
	}
}

func simulateNodeToolchainInstall(_ context.Context, execCtx ExecutionContext) error {
	switch NodeToolchainFromDecisions(execCtx.Decisions) {
	case NodeToolchainNvmPnpm:
		if err := logSimulation(execCtx, "Would install nvm (curl raw.githubusercontent.com/nvm-sh/.../install.sh | bash)"); err != nil {
			return err
		}
		return logSimulation(execCtx, "Would install pnpm (curl https://get.pnpm.io/install.sh | sh -)")
	default:
		return logSimulation(execCtx, "Would run Vite+ installer (curl https://vite.plus | bash)")
	}
}

func runDockerConfig(_ context.Context, execCtx ExecutionContext) error {
	runtime := DockerRuntimeFromDecisions(execCtx.Decisions)
	if runtime != DockerRuntimeColima {
		return logStageMessage(execCtx, fmt.Sprintf("No docker runtime handler for %q; skipping config", runtime))
	}
	return copyFromTemplates(execCtx, "docker-config.json", filepath.Join(execCtx.HomeDir, ".docker", "config.json"))
}

func simulateDockerConfig(_ context.Context, execCtx ExecutionContext) error {
	runtime := DockerRuntimeFromDecisions(execCtx.Decisions)
	if runtime != DockerRuntimeColima {
		return logSimulation(execCtx, fmt.Sprintf("Would skip docker config for unsupported runtime %q", runtime))
	}
	return logSimulation(execCtx, "Would write templates/docker-config.json to ~/.docker/config.json (runtime: colima)")
}

func runShellSetup(ctx context.Context, execCtx ExecutionContext) error {
	installOhMyZsh := ShellInstallOhMyZsh(execCtx.Decisions)
	applyZshrc := ShellApplyZshrcTemplate(execCtx.Decisions)
	applyStarship := ShellApplyStarshipTemplate(execCtx.Decisions)
	fsys := execCtx.fileSystem()

	if installOhMyZsh {
		if err := runCommand(ctx, execCtx, runner.Command{
			Name: "/bin/bash",
			Args: []string{"-c", "curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh | sh -s -- --unattended"},
		}); err != nil {
			return err
		}
	} else if err := logStageMessage(execCtx, "Skipping oh-my-zsh install by decision"); err != nil {
		return err
	}

	zshrcPath := filepath.Join(execCtx.HomeDir, ".zshrc")
	if applyZshrc {
		if _, err := fsys.Stat(zshrcPath); err == nil {
			if err = copyFileFS(fsys, zshrcPath, filepath.Join(execCtx.HomeDir, ".zshrc.bak")); err != nil {
				return err
			}
		}

		if err := copyFromTemplates(execCtx, "zshrc", zshrcPath); err != nil {
			return err
		}
	} else if err := logStageMessage(execCtx, "Skipping ~/.zshrc template write by decision"); err != nil {
		return err
	}

	if applyStarship {
		starshipPath := filepath.Join(execCtx.HomeDir, ".config", "starship.toml")
		if err := copyFromTemplates(execCtx, "starship.toml", starshipPath); err != nil {
			return err
		}
	} else if err := logStageMessage(execCtx, "Skipping starship config write by decision"); err != nil {
		return err
	}

	return nil
}

func simulateShellSetup(_ context.Context, execCtx ExecutionContext) error {
	if ShellInstallOhMyZsh(execCtx.Decisions) {
		if err := logSimulation(execCtx, "Would install oh-my-zsh in unattended mode"); err != nil {
			return err
		}
	} else if err := logSimulation(execCtx, "Would skip oh-my-zsh install by decision"); err != nil {
		return err
	}

	if ShellApplyZshrcTemplate(execCtx.Decisions) {
		if err := logSimulation(execCtx, "Would back up ~/.zshrc to ~/.zshrc.bak when present"); err != nil {
			return err
		}
		if err := logSimulation(execCtx, "Would write templates/zshrc to ~/.zshrc"); err != nil {
			return err
		}
	} else if err := logSimulation(execCtx, "Would skip ~/.zshrc template write by decision"); err != nil {
		return err
	}

	if ShellApplyStarshipTemplate(execCtx.Decisions) {
		return logSimulation(execCtx, "Would write templates/starship.toml to ~/.config/starship.toml")
	}
	return logSimulation(execCtx, "Would skip starship config write by decision")
}

func runGitConfig(_ context.Context, execCtx ExecutionContext) error {
	if err := copyFromTemplates(execCtx, "gitignore", filepath.Join(execCtx.HomeDir, ".gitignore")); err != nil {
		return err
	}

	gitConfigPath := filepath.Join(execCtx.HomeDir, ".gitconfig")
	return writeGitConfigFromTemplate(execCtx, gitConfigPath)
}

func simulateGitConfig(_ context.Context, execCtx ExecutionContext) error {
	if err := logSimulation(execCtx, "Would write templates/gitignore to ~/.gitignore"); err != nil {
		return err
	}
	name, email := GitIdentityFromDecisions(execCtx.Decisions)
	if err := validateGitIdentity(name, email); err != nil {
		return err
	}
	return logSimulation(execCtx, fmt.Sprintf("Would write templates/gitconfig and set git identity to %q <%s>", name, email))
}

func writeGitConfigFromTemplate(execCtx ExecutionContext, gitConfigPath string) error {
	name, email := GitIdentityFromDecisions(execCtx.Decisions)
	if err := validateGitIdentity(name, email); err != nil {
		return err
	}
	if err := copyFromTemplates(execCtx, "gitconfig", gitConfigPath); err != nil {
		return err
	}

	fsys := execCtx.fileSystem()
	content, err := fsys.ReadFile(gitConfigPath)
	if err != nil {
		return fmt.Errorf("read gitconfig: %w", err)
	}
	configBody := strings.TrimRight(string(content), "\n") + "\n\n[user]\n  name = " + name + "\n  email = " + email + "\n"
	if err = fsys.WriteFile(gitConfigPath, []byte(configBody), 0o644); err != nil {
		return fmt.Errorf("write gitconfig identity: %w", err)
	}
	return nil
}

func runManualAppStoreApps(_ context.Context, execCtx ExecutionContext) error {
	items := ManualAppStoreApps()
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
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: runner.EventTypeCommandStarted,
			Command:   command.String(),
		}); err != nil {
			return err
		}
	}

	result, err := execCtx.Runner.Run(ctx, command)
	if execCtx.Logger != nil {
		if logErr := logCommandOutput(execCtx, runner.EventTypeCommandStdout, result.Stdout); logErr != nil {
			return logErr
		}
		if logErr := logCommandOutput(execCtx, runner.EventTypeCommandStderr, result.Stderr); logErr != nil {
			return logErr
		}

		exitCode := result.ExitCode
		event := runner.Event{
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: runner.EventTypeCommandCompleted,
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
		var commandErr *runner.CommandError
		if errors.As(err, &commandErr) {
			return commandErr
		}
		return &runner.CommandError{
			Command:  command,
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			Err:      err,
		}
	}
	return nil
}

func logCommandOutput(execCtx ExecutionContext, eventType runner.EventType, output string) error {
	if execCtx.Logger == nil || output == "" {
		return nil
	}
	for _, line := range splitOutputLines(output) {
		message := line
		if message == "" {
			message = "<blank>"
		}
		event := runner.Event{
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: eventType,
			Message:   message,
		}
		if eventType == runner.EventTypeCommandStderr {
			event.Level = "warn"
		}
		if err := execCtx.Logger.Log(event); err != nil {
			return err
		}
	}
	return nil
}

func splitOutputLines(output string) []string {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		return nil
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func logSimulation(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID.String(),
		StageID:   execCtx.StageID.String(),
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode.String(),
		EventType: runner.EventTypeSimulation,
		Message:   message,
	})
}

func logStageMessage(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID.String(),
		StageID:   execCtx.StageID.String(),
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode.String(),
		EventType: runner.EventTypeStageMessage,
		Message:   message,
	})
}

func ensureBrewShellenv(execCtx ExecutionContext) error {
	zprofilePath := filepath.Join(execCtx.HomeDir, ".zprofile")
	const sentinel = "brew shellenv"
	const shellenvLine = `eval "$(/opt/homebrew/bin/brew shellenv)"`
	const commentLine = "# Set PATH, MANPATH, etc., for Homebrew."

	fsys := execCtx.fileSystem()
	content, err := fsys.ReadFile(zprofilePath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read .zprofile: %w", err)
	}
	if strings.Contains(string(content), sentinel) {
		return nil
	}

	if err = fsys.MkdirAll(filepath.Dir(zprofilePath), 0o755); err != nil {
		return fmt.Errorf("create .zprofile directory: %w", err)
	}

	var builder strings.Builder
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		builder.WriteString("\n")
	}
	builder.WriteString(commentLine + "\n" + shellenvLine + "\n")
	if err = fsys.AppendFile(zprofilePath, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("append brew shellenv to .zprofile: %w", err)
	}
	return nil
}

func copyFromTemplates(execCtx ExecutionContext, sourceName, destination string) error {
	return execCtx.templateStore().Copy(sourceName, destination)
}

func copyFile(sourcePath, destinationPath string) error {
	return copyFileFS(OSFileSystem{}, sourcePath, destinationPath)
}

func copyFileFS(fsys FileSystem, sourcePath, destinationPath string) error {
	payload, err := fsys.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if err = fsys.WriteFile(destinationPath, payload, 0o644); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}

func validateGitIdentity(name, email string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("git user name is required for custom git identity mode")
	}
	if strings.TrimSpace(email) == "" {
		return errors.New("git user email is required for custom git identity mode")
	}
	if strings.ContainsAny(name, "\r\n") {
		return errors.New("git user name cannot contain newlines")
	}
	if strings.ContainsAny(email, "\r\n") {
		return errors.New("git user email cannot contain newlines")
	}
	return nil
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
