package odf

import (
	"strings"

	"github.com/nullwerke/doc2md/internal/mdrender"
)

// linkTarget returns href if it is a link worth rendering: not empty, not an
// address within the document (those carry editor-internal names) and not an
// active target.
func linkTarget(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || mdrender.ActiveScheme(href) {
		return ""
	}
	return href
}
