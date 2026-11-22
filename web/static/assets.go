package static

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed dist/*
var files embed.FS

func Assets() (fs.FS, error) {
	built, err := fs.Sub(files, "dist")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"app.css", "htmx.min.js", "seller-upload.js"} {
		info, err := fs.Stat(built, name)
		if err != nil || info.Size() == 0 {
			return nil, fmt.Errorf("missing generated asset %s, run make build", name)
		}
	}
	return built, nil
}
