package pdfnative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestExtractRasterImagesWithoutWritableConfigDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)
	t.Setenv("AppData", blocker)
	t.Setenv("HOME", blocker)

	got := extractRasterImages([]byte("%PDF-1.7\nnot really a pdf"))

	if model.ConfigPath != "disable" {
		t.Errorf("pdfcpu model.ConfigPath = %q, want %q", model.ConfigPath, "disable")
	}
	if len(got) != 0 {
		t.Errorf("pages with images from malformed input = %d, want 0", len(got))
	}
}
