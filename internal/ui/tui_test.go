package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func TestPrepareExecutionSetupPersistsPhaseDecisions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	catalog := []stages.Stage{
		{ID: "vite_plus_install"},
		{ID: "docker_config"},
		{ID: "shell_setup"},
		{ID: "git_config"},
	}
	m := model{
		ctx:     context.Background(),
		store:   store,
		catalog: catalog,
		config:  Config{},
		devOptions: []toggleOption{
			{ID: "vite_plus_install", Title: "Vite+ Install", Selected: true},
			{ID: "docker_config", Title: "Docker Configuration", Selected: true},
			{ID: "shell_setup", Title: "Shell Setup", Selected: true},
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
			{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: false},
			{ID: stages.DecisionShellApplyZshrc, Title: "Write zshrc", Selected: true},
			{ID: stages.DecisionShellApplyStarship, Title: "Write starship", Selected: false},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
			{ID: stages.GitConfigModeExisting, Title: "existing"},
			{ID: stages.GitConfigModeCustom, Title: "custom"},
		},
		nodeSelection:    1,
		dockerSelection:  0,
		gitModeSelection: 2,
	}
	m.gitNameInput = textinput.New()
	m.gitEmailInput = textinput.New()
	m.gitNameInput.SetValue("Alice")
	m.gitEmailInput.SetValue("alice@example.com")

	setup, err := m.prepareExecutionSetup()
	if err != nil {
		t.Fatalf("prepareExecutionSetup returned error: %v", err)
	}
	defer setup.humanLog.Close()
	defer setup.eventsLog.Close()

	decisions := setup.runState.Decisions
	if got := stages.NodeToolchainFromDecisions(decisions); got != stages.NodeToolchainNvmPnpm {
		t.Fatalf("node toolchain mismatch: got=%s", got)
	}
	if got := stages.DockerRuntimeFromDecisions(decisions); got != stages.DockerRuntimeColima {
		t.Fatalf("docker runtime mismatch: got=%s", got)
	}
	if stages.ShellInstallOhMyZsh(decisions) {
		t.Fatalf("expected oh-my-zsh decision=false")
	}
	if !stages.ShellApplyZshrcTemplate(decisions) {
		t.Fatalf("expected zshrc decision=true")
	}
	if stages.ShellApplyStarshipTemplate(decisions) {
		t.Fatalf("expected starship decision=false")
	}
	if got := stages.GitConfigModeFromDecisions(decisions); got != stages.GitConfigModeCustom {
		t.Fatalf("git mode mismatch: got=%s", got)
	}
	name, email := stages.GitIdentityFromDecisions(decisions)
	if name != "Alice" || email != "alice@example.com" {
		t.Fatalf("git identity mismatch: got=%q <%s>", name, email)
	}
}

func TestParseGitIdentity(t *testing.T) {
	name, email := parseGitIdentity(`
[core]
  autocrlf = input
[user]
  name = Ada Lovelace
  email = ada@example.com
`)
	if name != "Ada Lovelace" {
		t.Fatalf("name mismatch: %q", name)
	}
	if email != "ada@example.com" {
		t.Fatalf("email mismatch: %q", email)
	}
}
