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

func TestTemplatesIncludeWatchPortHelpers(t *testing.T) {
	templateFS := TemplateFS()

	zshrc, err := fs.ReadFile(templateFS, "zshrc")
	if err != nil {
		t.Fatalf("read zshrc template: %v", err)
	}

	for _, want := range []string{
		`wp() {`,
		`watch -n 1 "lsof -nP -iTCP:\"$1\" -sTCP:LISTEN"`,
		`alias wap="watch -n 1 'lsof -nP -iTCP -sTCP:LISTEN'"`,
	} {
		if !strings.Contains(string(zshrc), want) {
			t.Fatalf("expected zshrc template to contain %q", want)
		}
	}

	brewfile, err := fs.ReadFile(templateFS, "Brewfile")
	if err != nil {
		t.Fatalf("read Brewfile template: %v", err)
	}
	if !strings.Contains(string(brewfile), `brew "watch"`+"\n") {
		t.Fatal(`expected Brewfile template to install watch`)
	}
}
