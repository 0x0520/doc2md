package sheets

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

type biffWriter struct{ bytes.Buffer }

func (w *biffWriter) rec(typ uint16, parts ...any) {
	var body bytes.Buffer
	for _, p := range parts {
		_ = binary.Write(&body, binary.LittleEndian, p)
	}
	_ = binary.Write(&w.Buffer, binary.LittleEndian, typ)
	_ = binary.Write(&w.Buffer, binary.LittleEndian, uint16(body.Len()))
	w.Write(body.Bytes())
}

func xlString(s string) []byte {
	b := []byte{byte(len(s)), 0, 0}
	binary.LittleEndian.PutUint16(b, uint16(len(s)))
	return append(b, s...)
}

func cellHead(row, col, xf uint16) []byte {
	b := make([]byte, 6)
	binary.LittleEndian.PutUint16(b, row)
	binary.LittleEndian.PutUint16(b[2:], col)
	binary.LittleEndian.PutUint16(b[4:], xf)
	return b
}

func rk(v int32, div100 bool) uint32 {
	r := uint32(v)<<2 | 0x02
	if div100 {
		r |= 0x01
	}
	return r
}

// biffFixture builds a workbook stream with one sheet exercising every cell record
// the reader handles. XF 0 is General, 1 a date, 2 two decimals, 3 a percentage.
func biffFixture(t testing.TB) []byte {
	t.Helper()
	var w biffWriter
	w.rec(recBOF, uint16(0x0600), uint16(0x0005), uint32(0), uint32(0), uint32(0))
	w.rec(recDateMode, uint16(0))
	w.rec(recFormat, uint16(164), xlString("yyyy\\-mm\\-dd"))
	for _, fmtID := range []uint16{0, 164, 2, 10} {
		w.rec(recXF, uint16(0), fmtID, make([]byte, 16))
	}
	boundAt := w.Len() + 4
	w.rec(recBoundSheet, uint32(0), uint8(0), uint8(0), uint8(6), uint8(0), []byte("Ledger"))

	// "Alpha" whole in SST; "Split string" begins compressed and continues as UTF-16.
	w.rec(recSST, uint32(3), uint32(2), xlString("Alpha"), uint16(12), uint8(0), []byte("Split "))
	cont := []byte{0x01}
	for _, r := range "string" {
		cont = binary.LittleEndian.AppendUint16(cont, uint16(r))
	}
	w.rec(recContinue, cont)
	w.rec(recEOF)

	sheetAt := w.Len()
	w.rec(recBOF, uint16(0x0600), uint16(0x0010), uint32(0), uint32(0), uint32(0))
	w.rec(recLabelSST, cellHead(0, 0, 0), uint32(0))
	w.rec(recLabelSST, cellHead(0, 1, 0), uint32(1))
	w.rec(recNumber, cellHead(1, 0, 1), math.Float64bits(46023))
	w.rec(recRK, cellHead(1, 1, 2), rk(1999, true))
	w.rec(recMulRK, cellHead(2, 0, 0)[:4], uint16(0), rk(7, false), uint16(3), rk(125, true), uint16(1))
	formula := func(row, col, xf uint16, result []byte) {
		w.rec(recFormula, cellHead(row, col, xf), result, uint16(0), uint32(0), uint16(0))
	}
	formula(3, 0, 2, binary.LittleEndian.AppendUint64(nil, math.Float64bits(2878.555)))
	formula(3, 1, 0, []byte{0, 0, 0, 0, 0, 0, 0xFF, 0xFF})
	w.rec(recString, xlString("from formula"))
	formula(3, 2, 0, []byte{1, 0, 1, 0, 0, 0, 0xFF, 0xFF})
	w.rec(recBoolErr, cellHead(4, 0, 0), uint8(0x07), uint8(1))
	w.rec(recLabel, cellHead(4, 1, 0), xlString("inline label"))
	w.rec(recEOF)

	out := w.Bytes()
	binary.LittleEndian.PutUint32(out[boundAt:], uint32(sheetAt))
	return out
}

func TestParseWorkbookReadsEveryCellKind(t *testing.T) {
	sheets, err := parseWorkbook(biffFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 1 || sheets[0].name != "Ledger" {
		t.Fatalf("sheets = %+v", sheets)
	}
	want := [][]string{
		{"Alpha", "Split string"},
		{"2026-01-01", "19.99"},
		{"7", "125.00%"},
		{"2878.56", "from formula", "TRUE"},
		{"#DIV/0!", "inline label"},
	}
	got := sheets[0].rows
	if len(got) != len(want) {
		t.Fatalf("rows = %q", got)
	}
	for i := range want {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseWorkbookRejectsOtherBIFFVersions(t *testing.T) {
	var w biffWriter
	w.rec(recBOF, uint16(0x0500), uint16(0x0005))
	if _, err := parseWorkbook(w.Bytes()); err != errNotBIFF8 {
		t.Fatalf("err = %v, want errNotBIFF8", err)
	}
}

func TestParseWorkbookSurvivesTruncation(t *testing.T) {
	full := biffFixture(t)
	for n := 0; n < len(full); n += 7 {
		_, _ = parseWorkbook(full[:n])
	}
}

func TestFormatNumber(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		code string
		want string
	}{
		{46023, "yyyy-mm-dd", "2026-01-01"},
		{46023.5, "dd.mm.yyyy hh:mm", "2026-01-01 12:00:00"},
		{0.75, "h:mm", "18:00:00"},
		{1.5, "[h]:mm:ss", "36:00:00"},
		{0.125, "0.0%", "12.5%"},
		{1234.5, "#,##0.00", "1234.50"},
		{1234.5, `"EUR" #,##0`, "1235"},
		{3, "General", "3"},
		{0.1 + 0.2, "", "0.3"},
		{-5, "#,##0;[Red]-#,##0", "-5"},
		{12345, "0.00E+00", "1.23E+04"},
		{42, `0 "days"`, "42"},
	} {
		if got := formatNumber(tc.v, tc.code, false); got != tc.want {
			t.Errorf("formatNumber(%v, %q) = %q, want %q", tc.v, tc.code, got, tc.want)
		}
	}
	if got := formatNumber(0, "yyyy-mm-dd", true); got != "1904-01-01" {
		t.Errorf("1904 date system: got %q", got)
	}
}

func FuzzParseWorkbook(f *testing.F) {
	f.Add(biffFixture(f))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseWorkbook(data)
	})
}
