// Package ui embeds the static web UI of the doc2md service: a page for uploading
// documents and viewing the converted Markdown. There is no build step.
package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html app.css app.js
var files embed.FS

// Handler serves the embedded UI files at "/".
func Handler() http.Handler {
	return http.FileServer(http.FS(files))
}
