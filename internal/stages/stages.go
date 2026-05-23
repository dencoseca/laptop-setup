package stages

import "context"

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

type CheckResult struct {
	Satisfied bool
	Message   string
}

type ExecutionContext struct {
	DryRun bool
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

func DefaultCatalog() []Stage {
	return []Stage{
		newNoopStage("xcode_clt", "Xcode Command Line Tools", "Validate or install Xcode Command Line Tools"),
		newNoopStage("macos_defaults", "macOS Defaults", "Apply macOS defaults"),
		newNoopStage("homebrew_install", "Homebrew Install", "Ensure Homebrew is installed"),
		newNoopStage("brew_bundle", "Brew Bundle", "Install selected packages and apps with brew bundle"),
		newNoopStage("vite_plus_install", "Vite+ Install", "Set up Node toolchain"),
		newNoopStage("docker_config", "Docker Configuration", "Configure Docker runtime preferences"),
		newNoopStage("shell_setup", "Shell Setup", "Configure shell environment"),
		newNoopStage("git_config", "Git Configuration", "Configure git identity and defaults"),
		newNoopStage("manual_app_store_apps", "Manual App Store Apps", "Display manual App Store install reminders"),
	}
}

func IDs(catalog []Stage) []string {
	ids := make([]string, 0, len(catalog))
	for _, stage := range catalog {
		ids = append(ids, stage.ID)
	}
	return ids
}

func newNoopStage(id, title, description string) Stage {
	return Stage{
		ID:          id,
		Title:       title,
		Description: description,
		CanSkip:     true,
		Critical:    false,
		LogTag:      id,
		Precheck: func(context.Context, ExecutionContext) (CheckResult, error) {
			return CheckResult{Satisfied: false}, nil
		},
		Run: func(context.Context, ExecutionContext) error {
			return nil
		},
		Simulate: func(context.Context, ExecutionContext) error {
			return nil
		},
	}
}
