package pdfnative

import (
	"encoding/binary"
	"testing"
)

func buildTestFont() []byte {
	be := binary.BigEndian
	u16 := func(b []byte, v int) []byte { return be.AppendUint16(b, uint16(v)) }
	u32 := func(b []byte, v int) []byte { return be.AppendUint32(b, uint32(v)) }

	var cmap []byte
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 1)
	cmap = u16(cmap, 3)
	cmap = u16(cmap, 1)
	cmap = u32(cmap, 12)
	cmap = u16(cmap, 4)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 6)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0x69)
	cmap = u16(cmap, 0x74)
	cmap = u16(cmap, 0xFFFF)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0x69)
	cmap = u16(cmap, 0x74)
	cmap = u16(cmap, 0xFFFF)
	cmap = u16(cmap, (11-0x69)&0xFFFF)
	cmap = u16(cmap, (10-0x74)&0xFFFF)
	cmap = u16(cmap, 1)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0)
	cmap = u16(cmap, 0)

	var gsub []byte
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 0)
	gsub = u16(gsub, 0)
	gsub = u16(gsub, 0)
	gsub = u16(gsub, 10)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 4)
	gsub = u16(gsub, 4)
	gsub = u16(gsub, 0)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 8)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 8)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 14)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 10)
	gsub = u16(gsub, 1)
	gsub = u16(gsub, 4)
	gsub = u16(gsub, 415)
	gsub = u16(gsub, 2)
	gsub = u16(gsub, 11)

	dataStart := 12 + 2*16
	cmapOff := dataStart
	gsubOff := dataStart + len(cmap)

	var out []byte
	out = u32(out, 0x00010000)
	out = u16(out, 2)
	out = u16(out, 0)
	out = u16(out, 0)
	out = u16(out, 0)
	out = append(out, "cmap"...)
	out = u32(out, 0)
	out = u32(out, cmapOff)
	out = u32(out, len(cmap))
	out = append(out, "GSUB"...)
	out = u32(out, 0)
	out = u32(out, gsubOff)
	out = u32(out, len(gsub))
	out = append(out, cmap...)
	out = append(out, gsub...)
	return out
}

func TestEmbeddedLigatures(t *testing.T) {
	got := embeddedLigatures(buildTestFont())
	if got[415] != "ti" {
		t.Errorf("embeddedLigatures[415] = %q, want %q (full map: %v)", got[415], "ti", got)
	}
}

func TestEmbeddedLigatures_NotAnSfnt(t *testing.T) {
	if got := embeddedLigatures([]byte("OTTO not really a font")); got != nil {
		t.Errorf("expected nil for non-sfnt bytes, got %v", got)
	}
	if got := embeddedLigatures(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
