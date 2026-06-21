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

func TestZshrcDetailsBannerIsOptIn(t *testing.T) {
	payload, err := fs.ReadFile(TemplateFS(), "zshrc")
	if err != nil {
		t.Fatalf("read zshrc template: %v", err)
	}

	content := string(payload)
	if !strings.Contains(content, "LAPTOP_SETUP_SHOW_SHELL_DETAILS") {
		t.Fatal("expected zshrc details banner to be guarded by opt-in environment variable")
	}
	if strings.HasSuffix(strings.TrimSpace(content), "sweet_sweet_details") {
		t.Fatal("expected zshrc details banner not to run unconditionally at startup")
	}
}
