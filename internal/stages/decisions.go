package stages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/domain/setup"
)

const (
	DecisionSelectedStageIDs = "selected_stage_ids"

	DecisionNodeToolchain = "dev.node_toolchain"

	DecisionDockerRuntime = "dev.docker_runtime"

	DecisionShellInstallOhMyZsh = "shell.install_oh_my_zsh"
	DecisionShellApplyZshrc     = "shell.apply_zshrc_template"
	DecisionShellApplyStarship  = "shell.apply_starship_template"

	DecisionGitConfigMode = "git.config_mode"
	DecisionGitUserName   = "git.user_name"
	DecisionGitUserEmail  = "git.user_email"
)

type NodeToolchain string

const (
	NodeToolchainVitePlus NodeToolchain = "vite_plus"
	NodeToolchainNvmPnpm  NodeToolchain = "pnpm_nvm"
)

func (toolchain NodeToolchain) Validate() error {
	switch toolchain {
	case NodeToolchainVitePlus, NodeToolchainNvmPnpm:
		return nil
	default:
		return fmt.Errorf("unknown value %q", toolchain)
	}
}

type DockerRuntime string

const (
	DockerRuntimeColima DockerRuntime = "colima"
)

func (runtime DockerRuntime) Validate() error {
	switch runtime {
	case DockerRuntimeColima:
		return nil
	default:
		return fmt.Errorf("unknown value %q", runtime)
	}
}

type GitConfigMode string

const (
	GitConfigModeTemplate GitConfigMode = "template"
	GitConfigModeCustom   GitConfigMode = "custom"
)

func (mode GitConfigMode) Validate() error {
	switch mode {
	case GitConfigModeTemplate, GitConfigModeCustom:
		return nil
	default:
		return fmt.Errorf("unknown value %q", mode)
	}
}

type DecisionSet struct {
	SelectedStageIDs    []setup.StageID
	NodeToolchain       NodeToolchain
	DockerRuntime       DockerRuntime
	ShellInstallOhMyZsh bool
	ShellApplyZshrc     bool
	ShellApplyStarship  bool
	GitConfigMode       GitConfigMode
	GitUserName         string
	GitUserEmail        string
}

func DefaultDecisions() DecisionSet {
	return DecisionSet{
		NodeToolchain:       NodeToolchainVitePlus,
		DockerRuntime:       DockerRuntimeColima,
		ShellInstallOhMyZsh: true,
		ShellApplyZshrc:     true,
		ShellApplyStarship:  true,
		GitConfigMode:       GitConfigModeTemplate,
	}
}

func DecisionSetFromMap(values map[string]any) (DecisionSet, error) {
	decisions := DefaultDecisions()
	for key, value := range values {
		switch key {
		case DecisionSelectedStageIDs:
			stageIDs, err := parseStageIDs(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.SelectedStageIDs = stageIDs
		case DecisionNodeToolchain:
			toolchain, err := parseNodeToolchain(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.NodeToolchain = toolchain
		case DecisionDockerRuntime:
			runtime, err := parseDockerRuntime(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.DockerRuntime = runtime
		case DecisionShellInstallOhMyZsh:
			enabled, err := parseBool(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.ShellInstallOhMyZsh = enabled
		case DecisionShellApplyZshrc:
			enabled, err := parseBool(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.ShellApplyZshrc = enabled
		case DecisionShellApplyStarship:
			enabled, err := parseBool(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.ShellApplyStarship = enabled
		case DecisionGitConfigMode:
			mode, err := parseGitConfigMode(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.GitConfigMode = mode
		case DecisionGitUserName:
			name, err := parseTrimmedString(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.GitUserName = name
		case DecisionGitUserEmail:
			email, err := parseTrimmedString(value)
			if err != nil {
				return DecisionSet{}, fmt.Errorf("%s: %w", key, err)
			}
			decisions.GitUserEmail = email
		default:
			return DecisionSet{}, fmt.Errorf("%s: unknown decision key", key)
		}
	}
	if err := decisions.Validate(); err != nil {
		return DecisionSet{}, err
	}
	return decisions, nil
}

func (decisions DecisionSet) ToMap() map[string]any {
	stageIDs := make([]string, 0, len(decisions.SelectedStageIDs))
	for _, stageID := range decisions.SelectedStageIDs {
		stageIDs = append(stageIDs, stageID.String())
	}
	return map[string]any{
		DecisionSelectedStageIDs:    stageIDs,
		DecisionNodeToolchain:       string(decisions.NodeToolchain),
		DecisionDockerRuntime:       string(decisions.DockerRuntime),
		DecisionShellInstallOhMyZsh: decisions.ShellInstallOhMyZsh,
		DecisionShellApplyZshrc:     decisions.ShellApplyZshrc,
		DecisionShellApplyStarship:  decisions.ShellApplyStarship,
		DecisionGitConfigMode:       string(decisions.GitConfigMode),
		DecisionGitUserName:         decisions.GitUserName,
		DecisionGitUserEmail:        decisions.GitUserEmail,
	}
}

func (decisions DecisionSet) Validate() error {
	if err := decisions.NodeToolchain.Validate(); err != nil {
		return fmt.Errorf("%s: %w", DecisionNodeToolchain, err)
	}
	if err := decisions.DockerRuntime.Validate(); err != nil {
		return fmt.Errorf("%s: %w", DecisionDockerRuntime, err)
	}
	if err := decisions.GitConfigMode.Validate(); err != nil {
		return fmt.Errorf("%s: %w", DecisionGitConfigMode, err)
	}
	for index, stageID := range decisions.SelectedStageIDs {
		if err := stageID.Validate(); err != nil {
			return fmt.Errorf("%s[%d]: %w", DecisionSelectedStageIDs, index, err)
		}
	}
	if decisions.GitConfigMode == GitConfigModeCustom {
		if strings.TrimSpace(decisions.GitUserName) == "" {
			return fmt.Errorf("%s: is required for custom git identity mode", DecisionGitUserName)
		}
		if strings.TrimSpace(decisions.GitUserEmail) == "" {
			return fmt.Errorf("%s: is required for custom git identity mode", DecisionGitUserEmail)
		}
	}
	return nil
}

func (decisions DecisionSet) IsZero() bool {
	return len(decisions.SelectedStageIDs) == 0 &&
		decisions.NodeToolchain == "" &&
		decisions.DockerRuntime == "" &&
		!decisions.ShellInstallOhMyZsh &&
		!decisions.ShellApplyZshrc &&
		!decisions.ShellApplyStarship &&
		decisions.GitConfigMode == "" &&
		decisions.GitUserName == "" &&
		decisions.GitUserEmail == ""
}

func (decisions DecisionSet) MarshalJSON() ([]byte, error) {
	return json.Marshal(decisions.ToMap())
}

func (decisions *DecisionSet) UnmarshalJSON(payload []byte) error {
	if len(payload) == 0 || string(payload) == "null" {
		*decisions = DefaultDecisions()
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	parsed, err := DecisionSetFromMap(raw)
	if err != nil {
		return err
	}
	*decisions = parsed
	return nil
}

func (decisions DecisionSet) WithSelectedStageIDs(stageIDs []setup.StageID) DecisionSet {
	decisions.SelectedStageIDs = append([]setup.StageID(nil), stageIDs...)
	return decisions
}

func NodeToolchainFromDecisions(decisions DecisionSet) NodeToolchain {
	return decisions.NodeToolchain
}

func DockerRuntimeFromDecisions(decisions DecisionSet) DockerRuntime {
	return decisions.DockerRuntime
}

func ShellInstallOhMyZsh(decisions DecisionSet) bool {
	return decisions.ShellInstallOhMyZsh
}

func ShellApplyZshrcTemplate(decisions DecisionSet) bool {
	return decisions.ShellApplyZshrc
}

func ShellApplyStarshipTemplate(decisions DecisionSet) bool {
	return decisions.ShellApplyStarship
}

func GitConfigModeFromDecisions(decisions DecisionSet) GitConfigMode {
	return decisions.GitConfigMode
}

func GitIdentityFromDecisions(decisions DecisionSet) (string, string) {
	return strings.TrimSpace(decisions.GitUserName), strings.TrimSpace(decisions.GitUserEmail)
}

func parseStageIDs(value any) ([]setup.StageID, error) {
	switch ids := value.(type) {
	case []setup.StageID:
		return append([]setup.StageID(nil), ids...), nil
	case []string:
		out := make([]setup.StageID, 0, len(ids))
		for _, id := range ids {
			out = append(out, setup.StageID(id))
		}
		return out, nil
	case []any:
		out := make([]setup.StageID, 0, len(ids))
		for index, id := range ids {
			value, ok := id.(string)
			if !ok {
				return nil, fmt.Errorf("[%d]: must be a string", index)
			}
			out = append(out, setup.StageID(value))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array of stage ids")
	}
}

func parseNodeToolchain(value any) (NodeToolchain, error) {
	raw, err := parseTrimmedString(value)
	if err != nil {
		return "", err
	}
	toolchain := NodeToolchain(raw)
	return toolchain, toolchain.Validate()
}

func parseDockerRuntime(value any) (DockerRuntime, error) {
	raw, err := parseTrimmedString(value)
	if err != nil {
		return "", err
	}
	runtime := DockerRuntime(raw)
	return runtime, runtime.Validate()
}

func parseGitConfigMode(value any) (GitConfigMode, error) {
	raw, err := parseTrimmedString(value)
	if err != nil {
		return "", err
	}
	mode := GitConfigMode(raw)
	return mode, mode.Validate()
}

func parseTrimmedString(value any) (string, error) {
	raw, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must be a string")
	}
	return strings.TrimSpace(raw), nil
}

func parseBool(value any) (bool, error) {
	raw, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("must be a boolean")
	}
	return raw, nil
}
