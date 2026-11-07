// Package webui embeds the built frontend so a single binary can serve the
// whole application without nginx or a static directory.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the embedded frontend, or ok=false when the build did not copy
// web/dist into this directory (local development runs Vite instead).
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
