package copilotburn

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var embeddedWebFS embed.FS

// WebHandler returns an http.Handler that serves embedded frontend web assets at the root path.
func WebHandler() http.Handler {
	sub, err := fs.Sub(embeddedWebFS, "web")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
