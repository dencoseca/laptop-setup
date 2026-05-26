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
	"time"

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
	InteractiveRunner        runner.InteractiveRunner
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
			Precheck:     precheckMacOSDefaults,
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
			Precheck:     precheckBrewBundle,
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
			Precheck:     precheckNodeToolchain,
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
			Precheck:     precheckDockerConfig,
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
			Precheck: precheckShellSetup,
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
			Precheck:     precheckGitConfig,
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

type macOSDefault struct {
	Domain  string
	Key     string
	Type    string
	Value   string
	Restart string
}

var macOSDefaults = []macOSDefault{
	{Domain: "-g", Key: "InitialKeyRepeat", Type: "-int", Value: "20"},
	{Domain: "-g", Key: "KeyRepeat", Type: "-int", Value: "1"},
	{Domain: "-g", Key: "AppleWindowTabbingMode", Type: "-string", Value: "always"},
	{Domain: "com.apple.dock", Key: "autohide", Type: "-bool", Value: "true", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "tilesize", Type: "-int", Value: "60", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "show-recents", Type: "-bool", Value: "false", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "show-process-indicators", Type: "-bool", Value: "false", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "magnification", Type: "-bool", Value: "true", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "largesize", Type: "-int", Value: "70", Restart: "Dock"},
	{Domain: "com.apple.dock", Key: "windowtabbing", Type: "-string", Value: "always", Restart: "Dock"},
	{Domain: "com.apple.finder", Key: "ShowPathbar", Type: "-bool", Value: "true", Restart: "Finder"},
	{Domain: "com.apple.finder", Key: "FXPreferredViewStyle", Type: "-string", Value: "clmv", Restart: "Finder"},
	{Domain: "com.apple.finder", Key: "_FXSortFoldersFirst", Type: "-bool", Value: "true", Restart: "Finder"},
	{Domain: "com.apple.finder", Key: "FXRemoveOldTrashItems", Type: "-bool", Value: "true", Restart: "Finder"},
	{Domain: "com.apple.finder", Key: "_FXSortFoldersFirstOnDesktop", Type: "-bool", Value: "true", Restart: "Finder"},
	{Domain: "com.apple.AppleMultitouchTrackpad", Key: "FirstClickThreshold", Type: "-int", Value: "0"},
	{Domain: "com.apple.Siri", Key: "StatusMenuVisible", Type: "-bool", Value: "false"},
}

func precheckMacOSDefaults(ctx context.Context, execCtx ExecutionContext) (CheckResult, error) {
	if execCtx.Runner == nil {
		return CheckResult{}, errors.New("runner is required")
	}
	for _, setting := range macOSDefaults {
		satisfied, err := macOSDefaultSatisfied(ctx, execCtx, setting)
		if err != nil {
			return CheckResult{}, err
		}
		if !satisfied {
			return CheckResult{Satisfied: false}, nil
		}
	}
	return CheckResult{Satisfied: true, Message: "macOS defaults already match requested values"}, nil
}

func precheckBrewBundle(ctx context.Context, execCtx ExecutionContext) (CheckResult, error) {
	if err := execCtx.packageManager().HomebrewAvailable(ctx); err != nil {
		return CheckResult{Satisfied: false}, nil
	}
	if execCtx.Runner == nil {
		return CheckResult{}, errors.New("runner is required")
	}

	brewfilePath := execCtx.GeneratedBrewfilePath
	if strings.TrimSpace(brewfilePath) == "" {
		if execCtx.DryRun {
			return CheckResult{Satisfied: false}, nil
		}
		generatedPath, selectedIDs, err := GenerateBrewfile(execCtx)
		if err != nil {
			return CheckResult{}, err
		}
		brewfilePath = generatedPath
		if execCtx.SetGeneratedBrewfilePath != nil {
			execCtx.SetGeneratedBrewfilePath(generatedPath)
		}
		if len(execCtx.SelectedBrewIDs) == 0 && len(selectedIDs) > 0 {
			execCtx.SelectedBrewIDs = selectedIDs
		}
	}

	result, err := execCtx.Runner.Run(ctx, runner.Command{
		Name: "brew",
		Args: []string{"bundle", "check", "--file", brewfilePath},
	})
	if err == nil {
		return CheckResult{Satisfied: true, Message: "Homebrew bundle already satisfied"}, nil
	}
	if result.ExitCode >= 1 {
		return CheckResult{Satisfied: false}, nil
	}
	return CheckResult{}, err
}

func precheckNodeToolchain(ctx context.Context, execCtx ExecutionContext) (CheckResult, error) {
	switch NodeToolchainFromDecisions(execCtx.Decisions) {
	case NodeToolchainNvmPnpm:
		nvmInstalled, err := nvmInstalled(execCtx)
		if err != nil {
			return CheckResult{}, err
		}
		pnpmInstalled, err := commandOrFileInstalled(ctx, execCtx, "pnpm", filepath.Join(execCtx.HomeDir, ".local", "share", "pnpm", "pnpm"))
		if err != nil {
			return CheckResult{}, err
		}
		if nvmInstalled && pnpmInstalled {
			return CheckResult{Satisfied: true, Message: "nvm and pnpm already installed"}, nil
		}
	default:
		viteInstalled, err := commandInstalled(ctx, execCtx, "vite")
		if err != nil {
			return CheckResult{}, err
		}
		if viteInstalled {
			return CheckResult{Satisfied: true, Message: "Vite toolchain already installed"}, nil
		}
	}
	return CheckResult{Satisfied: false}, nil
}

func precheckDockerConfig(_ context.Context, execCtx ExecutionContext) (CheckResult, error) {
	runtime := DockerRuntimeFromDecisions(execCtx.Decisions)
	if runtime != DockerRuntimeColima {
		return CheckResult{Satisfied: true, Message: fmt.Sprintf("No docker runtime handler for %q", runtime)}, nil
	}
	same, err := destinationMatchesTemplate(execCtx, "docker-config.json", filepath.Join(execCtx.HomeDir, ".docker", "config.json"))
	if err != nil {
		return CheckResult{}, err
	}
	if same {
		return CheckResult{Satisfied: true, Message: "Docker config already matches template"}, nil
	}
	return CheckResult{Satisfied: false}, nil
}

func precheckShellSetup(_ context.Context, execCtx ExecutionContext) (CheckResult, error) {
	if ShellInstallOhMyZsh(execCtx.Decisions) {
		installed, err := ohMyZshInstalled(execCtx)
		if err != nil {
			return CheckResult{}, err
		}
		if !installed {
			return CheckResult{Satisfied: false}, nil
		}
	}
	if ShellApplyZshrcTemplate(execCtx.Decisions) {
		same, err := destinationMatchesTemplate(execCtx, "zshrc", filepath.Join(execCtx.HomeDir, ".zshrc"))
		if err != nil {
			return CheckResult{}, err
		}
		if !same {
			return CheckResult{Satisfied: false}, nil
		}
	}
	if ShellApplyStarshipTemplate(execCtx.Decisions) {
		same, err := destinationMatchesTemplate(execCtx, "starship.toml", filepath.Join(execCtx.HomeDir, ".config", "starship.toml"))
		if err != nil {
			return CheckResult{}, err
		}
		if !same {
			return CheckResult{Satisfied: false}, nil
		}
	}
	return CheckResult{Satisfied: true, Message: "Shell setup already matches requested state"}, nil
}

func precheckGitConfig(_ context.Context, execCtx ExecutionContext) (CheckResult, error) {
	sameIgnore, err := destinationMatchesTemplate(execCtx, "gitignore", filepath.Join(execCtx.HomeDir, ".gitignore"))
	if err != nil {
		return CheckResult{}, err
	}
	if !sameIgnore {
		return CheckResult{Satisfied: false}, nil
	}

	gitConfigPath := filepath.Join(execCtx.HomeDir, ".gitconfig")
	expected, err := gitConfigContent(execCtx)
	if err != nil {
		return CheckResult{}, err
	}
	sameConfig, err := destinationMatchesContent(execCtx.fileSystem(), gitConfigPath, expected)
	if err != nil {
		return CheckResult{}, err
	}
	if sameConfig {
		return CheckResult{Satisfied: true, Message: "Git config already matches requested identity and defaults"}, nil
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
		Name:        "/bin/sh",
		Args:        []string{"-c", script},
		Interactive: true,
		Prompt:      "Installing Xcode Command Line Tools may ask macOS for administrator authorization.",
	})
}

func simulateXcodeCLT(_ context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would check and install Xcode Command Line Tools via softwareupdate")
}

func runMacOSDefaults(ctx context.Context, execCtx ExecutionContext) error {
	restarts := make(map[string]struct{})
	for _, setting := range macOSDefaults {
		satisfied, err := macOSDefaultSatisfied(ctx, execCtx, setting)
		if err != nil {
			return err
		}
		if satisfied {
			continue
		}
		if err = runCommand(ctx, execCtx, runner.Command{
			Name: "defaults",
			Args: []string{"write", setting.Domain, setting.Key, setting.Type, setting.Value},
		}); err != nil {
			return err
		}
		if setting.Restart != "" {
			restarts[setting.Restart] = struct{}{}
		}
	}

	for _, process := range []string{"Dock", "Finder"} {
		if _, ok := restarts[process]; !ok {
			continue
		}
		if err := runCommand(ctx, execCtx, runner.Command{Name: "killall", Args: []string{process}}); err != nil {
			continue
		}
	}
	return nil
}

func simulateMacOSDefaults(ctx context.Context, execCtx ExecutionContext) error {
	return logSimulation(execCtx, "Would apply configured macOS defaults and restart Dock/Finder")
}

func runHomebrewInstall(ctx context.Context, execCtx ExecutionContext) error {
	if err := execCtx.packageManager().HomebrewAvailable(ctx); err == nil {
		return ensureBrewShellenv(execCtx)
	}
	if err := runCommand(ctx, execCtx, runner.Command{
		Name:        "/bin/bash",
		Args:        []string{"-c", "NONINTERACTIVE=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""},
		Interactive: true,
		Prompt:      "Installing Homebrew may ask for your macOS administrator password.",
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
		installed, err := nvmInstalled(execCtx)
		if err != nil {
			return err
		}
		if installed {
			if err = logStageMessage(execCtx, "nvm already installed; skipping installer"); err != nil {
				return err
			}
		} else {
			if err = runCommand(ctx, execCtx, runner.Command{
				Name: "/bin/bash",
				Args: []string{"-c", "curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash"},
			}); err != nil {
				return err
			}
		}

		installed, err = commandOrFileInstalled(ctx, execCtx, "pnpm", filepath.Join(execCtx.HomeDir, ".local", "share", "pnpm", "pnpm"))
		if err != nil {
			return err
		}
		if installed {
			return logStageMessage(execCtx, "pnpm already installed; skipping installer")
		}
		return runCommand(ctx, execCtx, runner.Command{
			Name: "/bin/bash",
			Args: []string{"-c", "curl -fsSL https://get.pnpm.io/install.sh | sh -"},
		})
	default:
		installed, err := commandInstalled(ctx, execCtx, "vite")
		if err != nil {
			return err
		}
		if installed {
			return logStageMessage(execCtx, "Vite toolchain already installed; skipping installer")
		}
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

	if installOhMyZsh {
		installed, err := ohMyZshInstalled(execCtx)
		if err != nil {
			return err
		}
		if installed {
			if err = logStageMessage(execCtx, "oh-my-zsh already installed; skipping installer"); err != nil {
				return err
			}
		} else {
			if err = runCommand(ctx, execCtx, runner.Command{
				Name: "/bin/bash",
				Args: []string{"-c", "curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh | sh -s -- --unattended"},
			}); err != nil {
				return err
			}
		}
	} else if err := logStageMessage(execCtx, "Skipping oh-my-zsh install by decision"); err != nil {
		return err
	}

	zshrcPath := filepath.Join(execCtx.HomeDir, ".zshrc")
	if applyZshrc {
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
		if err := logSimulation(execCtx, "Would back up existing ~/.zshrc with a timestamped backup when content differs"); err != nil {
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
	if name == "" && email == "" {
		return logSimulation(execCtx, "Would write templates/gitconfig without git user identity")
	}
	return logSimulation(execCtx, "Would write templates/gitconfig and set provided git identity fields")
}

func writeGitConfigFromTemplate(execCtx ExecutionContext, gitConfigPath string) error {
	configBody, err := gitConfigContent(execCtx)
	if err != nil {
		return err
	}
	if _, err = writeFileSafely(execCtx.fileSystem(), gitConfigPath, configBody, privateFilePerm); err != nil {
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

	run := execCtx.Runner.Run
	if command.Interactive && execCtx.InteractiveRunner != nil {
		run = execCtx.InteractiveRunner.RunInteractive
	}
	result, err := run(ctx, command)
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

	if err = fsys.MkdirAll(filepath.Dir(zprofilePath), privateDirPerm); err != nil {
		return fmt.Errorf("create .zprofile directory: %w", err)
	}

	var builder strings.Builder
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		builder.WriteString("\n")
	}
	builder.WriteString(commentLine + "\n" + shellenvLine + "\n")
	if err = fsys.AppendFile(zprofilePath, []byte(builder.String()), privateFilePerm); err != nil {
		return fmt.Errorf("append brew shellenv to .zprofile: %w", err)
	}
	return nil
}

func macOSDefaultSatisfied(ctx context.Context, execCtx ExecutionContext, setting macOSDefault) (bool, error) {
	if execCtx.Runner == nil {
		return false, errors.New("runner is required")
	}
	result, err := execCtx.Runner.Run(ctx, runner.Command{
		Name: "defaults",
		Args: []string{"read", setting.Domain, setting.Key},
	})
	if err != nil {
		if result.ExitCode >= 1 {
			return false, nil
		}
		return false, err
	}
	return defaultValueMatches(setting.Type, strings.TrimSpace(result.Stdout), setting.Value), nil
}

func defaultValueMatches(valueType string, current string, expected string) bool {
	switch valueType {
	case "-bool":
		return boolDefaultValue(current) == boolDefaultValue(expected)
	default:
		return current == expected
	}
}

func boolDefaultValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func commandInstalled(ctx context.Context, execCtx ExecutionContext, name string) (bool, error) {
	if execCtx.Runner == nil {
		return false, nil
	}
	if _, err := execCtx.Runner.LookPath(ctx, name); err != nil {
		return false, nil
	}
	return true, nil
}

func commandOrFileInstalled(ctx context.Context, execCtx ExecutionContext, commandName string, filePath string) (bool, error) {
	installed, err := commandInstalled(ctx, execCtx, commandName)
	if err != nil || installed {
		return installed, err
	}
	info, err := execCtx.fileSystem().Stat(filePath)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func nvmInstalled(execCtx ExecutionContext) (bool, error) {
	return directoryExists(execCtx.fileSystem(), filepath.Join(execCtx.HomeDir, ".nvm"))
}

func ohMyZshInstalled(execCtx ExecutionContext) (bool, error) {
	return directoryExists(execCtx.fileSystem(), filepath.Join(execCtx.HomeDir, ".oh-my-zsh"))
}

func directoryExists(fsys FileSystem, path string) (bool, error) {
	info, err := fsys.Stat(path)
	if err == nil {
		return info.IsDir(), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func destinationMatchesTemplate(execCtx ExecutionContext, sourceName, destination string) (bool, error) {
	payload, _, err := execCtx.templateStore().Read(sourceName)
	if err != nil {
		return false, err
	}
	return destinationMatchesContent(execCtx.fileSystem(), destination, payload)
}

func destinationMatchesContent(fsys FileSystem, destination string, expected []byte) (bool, error) {
	content, err := fsys.ReadFile(destination)
	if err == nil {
		return string(content) == string(expected), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func gitConfigContent(execCtx ExecutionContext) ([]byte, error) {
	name, email := GitIdentityFromDecisions(execCtx.Decisions)
	if err := validateGitIdentity(name, email); err != nil {
		return nil, err
	}
	content, _, err := execCtx.templateStore().Read("gitconfig")
	if err != nil {
		return nil, err
	}
	configBody := strings.TrimRight(string(content), "\n") + "\n"
	if name != "" || email != "" {
		var builder strings.Builder
		builder.WriteString(configBody)
		builder.WriteString("\n[user]\n")
		if name != "" {
			builder.WriteString("  name = " + name + "\n")
		}
		if email != "" {
			builder.WriteString("  email = " + email + "\n")
		}
		configBody = builder.String()
	}
	return []byte(configBody), nil
}

type safeWriteResult struct {
	Changed    bool
	BackupPath string
}

func writeFileSafely(fsys FileSystem, destination string, payload []byte, perm fs.FileMode) (safeWriteResult, error) {
	if strings.TrimSpace(destination) == "" {
		return safeWriteResult{}, errors.New("destination path is required")
	}
	if err := fsys.MkdirAll(filepath.Dir(destination), privateDirPerm); err != nil {
		return safeWriteResult{}, fmt.Errorf("create destination directory: %w", err)
	}

	writePerm := perm
	info, statErr := fsys.Stat(destination)
	if statErr == nil {
		current, err := fsys.ReadFile(destination)
		if err != nil {
			return safeWriteResult{}, fmt.Errorf("read existing destination: %w", err)
		}
		if string(current) == string(payload) {
			return safeWriteResult{Changed: false}, nil
		}
		writePerm = info.Mode().Perm()
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return safeWriteResult{}, fmt.Errorf("stat destination: %w", statErr)
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	var backupPath string
	if statErr == nil {
		backupPath = destination + ".backup." + timestamp
		if err := copyFileFS(fsys, destination, backupPath, info.Mode().Perm()); err != nil {
			return safeWriteResult{}, fmt.Errorf("create timestamped backup: %w", err)
		}
	}

	tempPath := filepath.Join(filepath.Dir(destination), "."+filepath.Base(destination)+".tmp."+timestamp)
	if err := fsys.WriteFile(tempPath, payload, writePerm); err != nil {
		return safeWriteResult{}, fmt.Errorf("write temporary file: %w", err)
	}
	if err := fsys.Rename(tempPath, destination); err != nil {
		_ = fsys.Remove(tempPath)
		return safeWriteResult{}, fmt.Errorf("replace destination atomically: %w", err)
	}

	return safeWriteResult{Changed: true, BackupPath: backupPath}, nil
}

func copyFromTemplates(execCtx ExecutionContext, sourceName, destination string) error {
	return execCtx.templateStore().Copy(sourceName, destination)
}

func copyFile(sourcePath, destinationPath string) error {
	return copyFileFS(OSFileSystem{}, sourcePath, destinationPath, privateFilePerm)
}

func copyFileFS(fsys FileSystem, sourcePath, destinationPath string, perm fs.FileMode) error {
	payload, err := fsys.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if err = fsys.WriteFile(destinationPath, payload, perm); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}

func validateGitIdentity(name, email string) error {
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
