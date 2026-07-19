package stages

import (
	"strings"
	"testing"

	"github.com/dencoseca/laptop-setup/internal/domain/setup"
)

func TestDecisionSetFromMap(t *testing.T) {
	cases := []struct {
		name    string
		values  map[string]any
		want    DecisionSet
		wantErr string
	}{
		{
			name: "defaults missing values",
			values: map[string]any{
				DecisionSelectedStageIDs: []any{"xcode_clt", "brew_bundle"},
			},
			want: DefaultDecisions().WithSelectedStageIDs([]setup.StageID{"xcode_clt", "brew_bundle"}),
		},
		{
			name: "valid overrides",
			values: map[string]any{
				DecisionNodeToolchain:       string(NodeToolchainNvmPnpm),
				DecisionDockerRuntime:       string(DockerRuntimeColima),
				DecisionShellInstallOhMyZsh: false,
				DecisionShellApplyZshrc:     true,
				DecisionShellApplyStarship:  false,
				DecisionShellApplyGhostty:   false,
				DecisionGitConfigMode:       string(GitConfigModeTemplate),
				DecisionGitUserName:         "  Alice  ",
				DecisionGitUserEmail:        " alice@example.com ",
			},
			want: DecisionSet{
				NodeToolchain:       NodeToolchainNvmPnpm,
				DockerRuntime:       DockerRuntimeColima,
				ShellInstallOhMyZsh: false,
				ShellApplyZshrc:     true,
				ShellApplyStarship:  false,
				ShellApplyGhostty:   false,
				GitConfigMode:       GitConfigModeTemplate,
				GitUserName:         "Alice",
				GitUserEmail:        "alice@example.com",
			},
		},
		{
			name: "invalid enum rejected",
			values: map[string]any{
				DecisionNodeToolchain: "invalid",
			},
			wantErr: DecisionNodeToolchain,
		},
		{
			name: "invalid bool type rejected",
			values: map[string]any{
				DecisionShellApplyZshrc: "wrong-type",
			},
			wantErr: DecisionShellApplyZshrc,
		},
		{
			name: "blank custom git identity is accepted",
			values: map[string]any{
				DecisionGitConfigMode: string(GitConfigModeCustom),
			},
			want: DecisionSet{
				NodeToolchain:       NodeToolchainVitePlus,
				DockerRuntime:       DockerRuntimeColima,
				ShellInstallOhMyZsh: true,
				ShellApplyZshrc:     true,
				ShellApplyStarship:  true,
				ShellApplyGhostty:   true,
				GitConfigMode:       GitConfigModeCustom,
			},
		},
		{
			name: "git identity newline rejected",
			values: map[string]any{
				DecisionGitUserName: "Alice\nBob",
			},
			wantErr: DecisionGitUserName,
		},
		{
			name: "unknown key rejected",
			values: map[string]any{
				"unknown": true,
			},
			wantErr: "unknown decision key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecisionSetFromMap(tc.values)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecisionSetFromMap returned error: %v", err)
			}
			if got.NodeToolchain != tc.want.NodeToolchain ||
				got.DockerRuntime != tc.want.DockerRuntime ||
				got.ShellInstallOhMyZsh != tc.want.ShellInstallOhMyZsh ||
				got.ShellApplyZshrc != tc.want.ShellApplyZshrc ||
				got.ShellApplyStarship != tc.want.ShellApplyStarship ||
				got.ShellApplyGhostty != tc.want.ShellApplyGhostty ||
				got.GitConfigMode != tc.want.GitConfigMode ||
				got.GitUserName != tc.want.GitUserName ||
				got.GitUserEmail != tc.want.GitUserEmail {
				t.Fatalf("decision mismatch: got=%+v want=%+v", got, tc.want)
			}
		})
	}
}

func TestDecisionSetToMapPreservesJSONKeys(t *testing.T) {
	decisions := DefaultDecisions().WithSelectedStageIDs([]setup.StageID{"xcode_clt"})
	decisions.NodeToolchain = NodeToolchainNvmPnpm

	out := decisions.ToMap()
	if out[DecisionNodeToolchain] != string(NodeToolchainNvmPnpm) {
		t.Fatalf("node toolchain mismatch: %v", out[DecisionNodeToolchain])
	}
	if _, ok := out[DecisionSelectedStageIDs]; !ok {
		t.Fatalf("expected selected stage ids key in map: %+v", out)
	}
	if out[DecisionShellApplyGhostty] != true {
		t.Fatalf("expected Ghostty template decision in map: %+v", out)
	}
}
