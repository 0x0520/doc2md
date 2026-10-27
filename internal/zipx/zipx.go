// Package zipx holds the size guards shared by the zip-based readers (OOXML,
// OpenDocument, EPUB), which all parse untrusted archives.
package zipx

import (
	"archive/zip"
	"io"
)

// MaxPartSize caps how much a single zip entry may decompress to. The size in a
// zip header is attacker-controlled, so reads are bounded instead of pre-allocated.
const MaxPartSize = 1 << 28

// MaxTotalSize caps the cumulative decompressed size a reader takes from one
// archive, since per-part limits do not bound an archive of thousands of parts.
const MaxTotalSize = 512 << 20

// readCap is the limit ReadEntry applies; a variable only so tests can lower it.
var readCap int64 = MaxPartSize

// ReadEntry reads one zip entry, silently truncating it at MaxPartSize.
func ReadEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, readCap))
}

// Find returns the entry with exactly this name, or nil. Entry order in a zip is
// unspecified, so parts are located by name.
func Find(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
