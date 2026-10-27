package zipx

import (
	"archive/zip"
	"io"
)

const MaxPartSize = 1 << 28

const MaxTotalSize = 512 << 20

var readCap int64 = MaxPartSize

func ReadEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, readCap))
}

func Find(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
