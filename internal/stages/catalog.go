package stages

import (
	"context"

	"github.com/dencoseca/laptop-setup/internal/domain/setup"
	"github.com/dencoseca/laptop-setup/internal/runner"
)

type StageID = setup.StageID
type Status = setup.StageStatus

const (
	StageXcodeCLT           StageID = "xcode_clt"
	StageMacOSDefaults      StageID = "macos_defaults"
	StageHomebrewInstall    StageID = "homebrew_install"
	StageBrewBundle         StageID = "brew_bundle"
	StageNodeToolchain      StageID = "node_toolchain"
	StageDockerConfig       StageID = "docker_config"
	StageShellSetup         StageID = "shell_setup"
	StageGitConfig          StageID = "git_config"
	StageManualAppStoreApps StageID = "manual_app_store_apps"
)

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

	brewShellenvSentinel    = "brew shellenv"
	brewShellenvLine        = `eval "$(/opt/homebrew/bin/brew shellenv)"`
	brewShellenvCommentLine = "# Set PATH, MANPATH, etc., for Homebrew."
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
	Precheck     CheckFunc
	Run          RunFunc
	Simulate     RunFunc
}

func ManualAppStoreApps() []string {
	return append([]string(nil), manualAppStoreApps...)
}

func DefaultCatalog() []Stage {
	return []Stage{
		{
			ID:           StageXcodeCLT,
			Title:        "Xcode Command Line Tools",
			Description:  "Validate or install Xcode Command Line Tools",
			DecisionDeps: nil,
			CanSkip:      false,
			Critical:     true,
			Precheck:     precheckXcodeCLT,
			Run:          runXcodeCLT,
			Simulate:     simulateXcodeCLT,
		},
		{
			ID:           StageMacOSDefaults,
			Title:        "macOS Defaults",
			Description:  "Apply macOS defaults",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			Precheck:     precheckMacOSDefaults,
			Run:          runMacOSDefaults,
			Simulate:     simulateMacOSDefaults,
		},
		{
			ID:           StageHomebrewInstall,
			Title:        "Homebrew Install",
			Description:  "Ensure Homebrew is installed",
			DecisionDeps: nil,
			CanSkip:      false,
			Critical:     true,
			Precheck:     precheckHomebrew,
			Run:          runHomebrewInstall,
			Simulate:     simulateHomebrewInstall,
		},
		{
			ID:           StageBrewBundle,
			Title:        "Brew Bundle",
			Description:  "Install selected packages and apps with brew bundle",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
			Precheck:     precheckBrewBundle,
			Run:          runBrewBundle,
			Simulate:     simulateBrewBundle,
		},
		{
			ID:           StageNodeToolchain,
			Title:        "Node Toolchain",
			Description:  "Set up Node toolchain",
			DecisionDeps: []string{DecisionNodeToolchain},
			CanSkip:      true,
			Critical:     false,
			Precheck:     precheckNodeToolchain,
			Run:          runNodeToolchainInstall,
			Simulate:     simulateNodeToolchainInstall,
		},
		{
			ID:           StageDockerConfig,
			Title:        "Docker Configuration",
			Description:  "Configure Docker runtime preferences",
			DecisionDeps: []string{DecisionDockerRuntime},
			CanSkip:      true,
			Critical:     false,
			Precheck:     precheckDockerConfig,
			Run:          runDockerConfig,
			Simulate:     simulateDockerConfig,
		},
		{
			ID:          StageShellSetup,
			Title:       "Shell Setup",
			Description: "Configure shell environment",
			DecisionDeps: []string{
				DecisionShellInstallOhMyZsh,
				DecisionShellApplyZshrc,
				DecisionShellApplyStarship,
				DecisionShellApplyGhostty,
			},
			CanSkip:  true,
			Critical: false,
			Precheck: precheckShellSetup,
			Run:      runShellSetup,
			Simulate: simulateShellSetup,
		},
		{
			ID:           StageGitConfig,
			Title:        "Git Configuration",
			Description:  "Configure git identity and defaults",
			DecisionDeps: []string{DecisionGitConfigMode, DecisionGitUserName, DecisionGitUserEmail},
			CanSkip:      true,
			Critical:     false,
			Precheck:     precheckGitConfig,
			Run:          runGitConfig,
			Simulate:     simulateGitConfig,
		},
		{
			ID:           StageManualAppStoreApps,
			Title:        "Manual App Store Apps",
			Description:  "Display manual App Store install reminders",
			DecisionDeps: nil,
			CanSkip:      true,
			Critical:     false,
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
