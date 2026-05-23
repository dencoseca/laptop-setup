package stages

import "testing"

func TestNormalizeDecisionsAppliesDefaultsAndValidation(t *testing.T) {
	normalized := NormalizeDecisions(map[string]any{
		DecisionNodeToolchain:       "invalid",
		DecisionDockerRuntime:       "invalid",
		DecisionShellInstallOhMyZsh: false,
		DecisionShellApplyZshrc:     "wrong-type",
		DecisionGitConfigMode:       GitConfigModeCustom,
		DecisionGitUserName:         "  Alice  ",
		DecisionGitUserEmail:        " alice@example.com ",
	})

	if normalized[DecisionNodeToolchain] != NodeToolchainVitePlus {
		t.Fatalf("expected default node toolchain, got %v", normalized[DecisionNodeToolchain])
	}
	if normalized[DecisionDockerRuntime] != DockerRuntimeColima {
		t.Fatalf("expected default docker runtime, got %v", normalized[DecisionDockerRuntime])
	}
	if normalized[DecisionShellInstallOhMyZsh] != false {
		t.Fatalf("expected shell install override to persist, got %v", normalized[DecisionShellInstallOhMyZsh])
	}
	if normalized[DecisionShellApplyZshrc] != true {
		t.Fatalf("expected invalid bool to fallback true, got %v", normalized[DecisionShellApplyZshrc])
	}
	if normalized[DecisionGitConfigMode] != GitConfigModeCustom {
		t.Fatalf("expected custom git mode, got %v", normalized[DecisionGitConfigMode])
	}
	if normalized[DecisionGitUserName] != "Alice" {
		t.Fatalf("expected trimmed git user name, got %v", normalized[DecisionGitUserName])
	}
	if normalized[DecisionGitUserEmail] != "alice@example.com" {
		t.Fatalf("expected trimmed git user email, got %v", normalized[DecisionGitUserEmail])
	}
}
