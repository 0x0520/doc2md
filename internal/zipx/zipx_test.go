package zipx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func archive(t *testing.T, parts ...[2]string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p[0])
		if err != nil {
			t.Fatalf("create %s: %v", p[0], err)
		}
		if _, err := w.Write([]byte(p[1])); err != nil {
			t.Fatalf("write %s: %v", p[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return zr
}

func TestFindByExactName(t *testing.T) {
	zr := archive(t,
		[2]string{"content.xml", "right"},
		[2]string{"content.xml.rels", "wrong"},
	)
	f := Find(zr, "content.xml")
	if f == nil {
		t.Fatal("part not found")
	}
	b, err := ReadEntry(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "right" {
		t.Errorf("wrong part read: %q", b)
	}
	if Find(zr, "missing.xml") != nil {
		t.Error("a missing part must report itself as missing, not resolve to a neighbour")
	}
}

func TestReadEntryTruncatesAtCap(t *testing.T) {
	orig := readCap
	t.Cleanup(func() { readCap = orig })
	readCap = 1024

	zr := archive(t, [2]string{"big.xml", strings.Repeat("A", 8192)})
	b, err := ReadEntry(Find(zr, "big.xml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) != 1024 {
		t.Errorf("read %d bytes, want it truncated at the cap (1024)", len(b))
	}
}

func TestReadEntryKeepsWholeSmallPart(t *testing.T) {
	body := strings.Repeat("The sample is checked. ", 100)
	zr := archive(t, [2]string{"content.xml", body})
	b, err := ReadEntry(Find(zr, "content.xml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != body {
		t.Errorf("part came back altered: %d bytes, want %d", len(b), len(body))
	}
}
