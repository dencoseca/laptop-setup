package laptopsetup

import (
	"io/fs"
	"strings"
	"testing"
)

func TestTemplateFSEmbedsRequiredTemplates(t *testing.T) {
	templateFS := TemplateFS()
	for _, name := range []string{
		"Brewfile",
		"docker-config.json",
		"gitconfig",
		"gitignore",
		"ghostty.config",
		"starship.toml",
		"zshrc",
	} {
		payload, err := fs.ReadFile(templateFS, name)
		if err != nil {
			t.Fatalf("expected embedded template %s: %v", name, err)
		}
		if len(payload) == 0 {
			t.Fatalf("expected embedded template %s to be non-empty", name)
		}
	}
}

func TestEmbeddedTerminalSetupMatchesGhosttyStack(t *testing.T) {
	templateFS := TemplateFS()

	brewfile := mustReadTemplate(t, templateFS, "Brewfile")
	for _, entry := range []string{`brew "bat"`, `brew "fzf"`, `cask "ghostty"`} {
		if !strings.Contains(brewfile, entry) {
			t.Fatalf("expected Brewfile to contain %q", entry)
		}
	}
	zshrc := mustReadTemplate(t, templateFS, "zshrc")
	for _, plugin := range []string{"fzf", "zsh-autosuggestions", "zsh-syntax-highlighting"} {
		if !strings.Contains(zshrc, plugin) {
			t.Fatalf("expected zshrc to enable %s", plugin)
		}
	}

	starship := mustReadTemplate(t, templateFS, "starship.toml")
	if strings.Contains(starship, "[character]") {
		t.Fatal("expected Starship template to use the default character configuration")
	}

	ghostty := mustReadTemplate(t, templateFS, "ghostty.config")
	for _, setting := range []string{
		"font-family = JetBrains Mono",
		"font-size = 13",
		"adjust-cell-height = 4%",
		"window-width = 120",
		"macos-titlebar-style = tabs",
	} {
		if !strings.Contains(ghostty, setting) {
			t.Fatalf("expected Ghostty config to contain %q", setting)
		}
	}
}

func TestEmbeddedZshrcGuardsOptionalDependencies(t *testing.T) {
	zshrc := mustReadTemplate(t, TemplateFS(), "zshrc")

	tests := []struct {
		name  string
		block string
	}{
		{
			name: "missing Oh My Zsh",
			block: strings.Join([]string{
				`if [[ -r "$ZSH/oh-my-zsh.sh" ]]; then`,
				`  source "$ZSH/oh-my-zsh.sh"`,
				"fi",
			}, "\n"),
		},
		{
			name: "missing Starship",
			block: strings.Join([]string{
				"if command -v starship >/dev/null 2>&1; then",
				`  eval "$(starship init zsh)"`,
				"fi",
			}, "\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(zshrc, test.block) {
				t.Fatalf("expected zshrc to contain dependency guard:\n%s", test.block)
			}
		})
	}
}

func mustReadTemplate(t *testing.T, templateFS fs.FS, name string) string {
	t.Helper()
	payload, err := fs.ReadFile(templateFS, name)
	if err != nil {
		t.Fatalf("read embedded template %s: %v", name, err)
	}
	return string(payload)
}
