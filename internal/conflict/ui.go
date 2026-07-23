package conflict

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/dist
var uiFiles embed.FS

func uiFileServer() http.Handler {
	dist, err := fs.Sub(uiFiles, "static/dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(dist))
}
