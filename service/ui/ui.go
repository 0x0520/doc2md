package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html app.css app.js
var files embed.FS

func Handler() http.Handler {
	return http.FileServer(http.FS(files))
}
