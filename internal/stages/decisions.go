package stages

import "strings"

const (
	DecisionSelectedStageIDs = "selected_stage_ids"

	DecisionNodeToolchain = "dev.node_toolchain"
	NodeToolchainVitePlus = "vite_plus"
	NodeToolchainNvmPnpm  = "pnpm_nvm"

	DecisionDockerRuntime = "dev.docker_runtime"
	DockerRuntimeColima   = "colima"

	DecisionShellInstallOhMyZsh = "shell.install_oh_my_zsh"
	DecisionShellApplyZshrc     = "shell.apply_zshrc_template"
	DecisionShellApplyStarship  = "shell.apply_starship_template"

	DecisionGitConfigMode = "git.config_mode"
	GitConfigModeTemplate = "template"
	GitConfigModeExisting = "existing"
	GitConfigModeCustom   = "custom"

	DecisionGitUserName  = "git.user_name"
	DecisionGitUserEmail = "git.user_email"
)

func DefaultDecisions() map[string]any {
	return map[string]any{
		DecisionNodeToolchain:       NodeToolchainVitePlus,
		DecisionDockerRuntime:       DockerRuntimeColima,
		DecisionShellInstallOhMyZsh: true,
		DecisionShellApplyZshrc:     true,
		DecisionShellApplyStarship:  true,
		DecisionGitConfigMode:       GitConfigModeTemplate,
	}
}

func NormalizeDecisions(decisions map[string]any) map[string]any {
	normalized := DefaultDecisions()
	for key, value := range decisions {
		normalized[key] = value
	}

	normalized[DecisionNodeToolchain] = NodeToolchainFromDecisions(normalized)
	normalized[DecisionDockerRuntime] = DockerRuntimeFromDecisions(normalized)
	normalized[DecisionShellInstallOhMyZsh] = ShellInstallOhMyZsh(normalized)
	normalized[DecisionShellApplyZshrc] = ShellApplyZshrcTemplate(normalized)
	normalized[DecisionShellApplyStarship] = ShellApplyStarshipTemplate(normalized)
	normalized[DecisionGitConfigMode] = GitConfigModeFromDecisions(normalized)
	normalized[DecisionGitUserName], normalized[DecisionGitUserEmail] = GitIdentityFromDecisions(normalized)

	return normalized
}

func NodeToolchainFromDecisions(decisions map[string]any) string {
	switch strings.TrimSpace(stringValue(decisions, DecisionNodeToolchain)) {
	case NodeToolchainNvmPnpm:
		return NodeToolchainNvmPnpm
	default:
		return NodeToolchainVitePlus
	}
}

func DockerRuntimeFromDecisions(decisions map[string]any) string {
	switch strings.TrimSpace(stringValue(decisions, DecisionDockerRuntime)) {
	case DockerRuntimeColima:
		return DockerRuntimeColima
	default:
		return DockerRuntimeColima
	}
}

func ShellInstallOhMyZsh(decisions map[string]any) bool {
	return boolValue(decisions, DecisionShellInstallOhMyZsh, true)
}

func ShellApplyZshrcTemplate(decisions map[string]any) bool {
	return boolValue(decisions, DecisionShellApplyZshrc, true)
}

func ShellApplyStarshipTemplate(decisions map[string]any) bool {
	return boolValue(decisions, DecisionShellApplyStarship, true)
}

func GitConfigModeFromDecisions(decisions map[string]any) string {
	switch strings.TrimSpace(stringValue(decisions, DecisionGitConfigMode)) {
	case GitConfigModeExisting:
		return GitConfigModeExisting
	case GitConfigModeCustom:
		return GitConfigModeCustom
	default:
		return GitConfigModeTemplate
	}
}

func GitIdentityFromDecisions(decisions map[string]any) (string, string) {
	return strings.TrimSpace(stringValue(decisions, DecisionGitUserName)),
		strings.TrimSpace(stringValue(decisions, DecisionGitUserEmail))
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback
	}
	value, ok := raw.(bool)
	if !ok {
		return fallback
	}
	return value
}
