package laptopsetup

import (
	"embed"
	"io/fs"
)

//go:embed templates/*
var embeddedFiles embed.FS

func TemplateFS() fs.FS {
	templateFS, err := fs.Sub(embeddedFiles, "templates")
	if err != nil {
		panic(err)
	}
	return templateFS
}
