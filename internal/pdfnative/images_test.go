package pdfnative

import (
	"os"
	"testing"
)

// TestPDFCPUUsesNoConfigDir points every config location at an empty directory: the
// pdfcpu passes (image extraction, the repair rewrite) must neither need nor create
// a configuration there.
func TestPDFCPUUsesNoConfigDir(t *testing.T) {
	// Built first: the fixture generator is pdfcpu with its default configuration.
	data := buildPDF(t, synthPage{runs: []textRun{{text: "Minutes of the meeting", x: 72, yTop: 120, size: 10}}})
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	t.Setenv("HOME", dir)

	if got := extractRasterImages([]byte("%PDF-1.7\nnot really a pdf")); len(got) != 0 {
		t.Errorf("pages with images from malformed input = %d, want 0", len(got))
	}
	_ = extractRasterImages(data)
	if _, err := rewritePDF(data); err != nil {
		t.Errorf("rewrite: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("pdfcpu wrote %d entries into the config location, want none", len(entries))
	}
}
