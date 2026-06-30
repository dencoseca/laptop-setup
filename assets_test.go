package laptopsetup

import (
	"io/fs"
	"testing"
)

func TestTemplateFSEmbedsRequiredTemplates(t *testing.T) {
	templateFS := TemplateFS()
	for _, name := range []string{
		"Brewfile",
		"docker-config.json",
		"gitconfig",
		"gitignore",
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
