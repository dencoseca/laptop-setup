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
	if !strings.Contains(starship, `format = '$symbol'`) || !strings.Contains(starship, `"\u200b"`) {
		t.Fatal("expected Starship template to keep the command on the next line at column zero")
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

func mustReadTemplate(t *testing.T, templateFS fs.FS, name string) string {
	t.Helper()
	payload, err := fs.ReadFile(templateFS, name)
	if err != nil {
		t.Fatalf("read embedded template %s: %v", name, err)
	}
	return string(payload)
}
