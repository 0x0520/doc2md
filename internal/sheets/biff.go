package sheets

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

// BIFF8 record types used here, see [MS-XLS] 2.3.
const (
	recFormula    = 0x0006
	recEOF        = 0x000A
	recDateMode   = 0x0022
	recContinue   = 0x003C
	recBoundSheet = 0x0085
	recMulRK      = 0x00BD
	recXF         = 0x00E0
	recSST        = 0x00FC
	recLabelSST   = 0x00FD
	recNumber     = 0x0203
	recLabel      = 0x0204
	recBoolErr    = 0x0205
	recString     = 0x0207
	recRK         = 0x027E
	recFormat     = 0x041E
	recBOF        = 0x0809
)

var errNotBIFF8 = errors.New("not a BIFF8 workbook")

type biffSheet struct {
	name   string
	offset uint32
}

type biffBook struct {
	sheets   []biffSheet
	sst      []string
	formats  map[uint16]string
	xfFmt    []uint16
	date1904 bool
}

// readBIFF8 reads every worksheet of a legacy .xls (Excel 97 and later) into
// rows of displayed cell values.
func readBIFF8(data []byte) ([]sheet, error) {
	stream, err := workbookStream(data)
	if err != nil {
		return nil, err
	}
	return parseWorkbook(stream)
}

func workbookStream(data []byte) ([]byte, error) {
	doc, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	for entry, err := doc.Next(); err == nil; entry, err = doc.Next() {
		if entry.Name != "Workbook" {
			continue
		}
		return io.ReadAll(io.LimitReader(entry, 1<<28))
	}
	return nil, errNotBIFF8
}

type biffRecord struct {
	typ  uint16
	data []byte
	pos  int
}

func readRecords(stream []byte, from int) func() (biffRecord, bool) {
	pos := from
	return func() (biffRecord, bool) {
		if pos < 0 || pos+4 > len(stream) {
			return biffRecord{}, false
		}
		typ := binary.LittleEndian.Uint16(stream[pos:])
		n := int(binary.LittleEndian.Uint16(stream[pos+2:]))
		start := pos + 4
		if start+n > len(stream) {
			return biffRecord{}, false
		}
		rec := biffRecord{typ: typ, data: stream[start : start+n], pos: pos}
		pos = start + n
		return rec, true
	}
}

func parseWorkbook(stream []byte) ([]sheet, error) {
	book := &biffBook{formats: map[uint16]string{}}
	next := readRecords(stream, 0)

	first, ok := next()
	if !ok || first.typ != recBOF || len(first.data) < 4 || binary.LittleEndian.Uint16(first.data) != 0x0600 {
		return nil, errNotBIFF8
	}

	var pendingSST [][]byte
	for {
		rec, ok := next()
		if !ok || rec.typ == recEOF {
			break
		}
		switch rec.typ {
		case recBoundSheet:
			if len(rec.data) >= 8 && rec.data[5] == 0 {
				name, _ := shortString(rec.data[6:])
				book.sheets = append(book.sheets, biffSheet{name: name, offset: binary.LittleEndian.Uint32(rec.data)})
			}
		case recDateMode:
			book.date1904 = len(rec.data) >= 2 && binary.LittleEndian.Uint16(rec.data) == 1
		case recFormat:
			if len(rec.data) >= 5 {
				code, _ := unicodeString(rec.data[2:], 2)
				book.formats[binary.LittleEndian.Uint16(rec.data)] = code
			}
		case recXF:
			if len(rec.data) >= 4 {
				book.xfFmt = append(book.xfFmt, binary.LittleEndian.Uint16(rec.data[2:]))
			}
		case recSST:
			pendingSST = [][]byte{rec.data}
		case recContinue:
			if pendingSST != nil {
				pendingSST = append(pendingSST, rec.data)
			}
		default:
			if pendingSST != nil {
				book.sst = parseSST(pendingSST)
				pendingSST = nil
			}
		}
	}
	if pendingSST != nil {
		book.sst = parseSST(pendingSST)
	}

	var out []sheet
	for _, s := range book.sheets {
		budget := maxCellsPerSheet
		rows := book.readSheet(stream, int(s.offset), &budget)
		out = append(out, sheet{name: s.name, rows: rows})
	}
	return out, nil
}

func (b *biffBook) readSheet(stream []byte, offset int, budget *int) [][]string {
	next := readRecords(stream, offset)
	first, ok := next()
	if !ok || first.typ != recBOF {
		return nil
	}

	var rows [][]string
	set := func(row, col uint16, v string) {
		if v == "" || *budget <= 0 {
			return
		}
		*budget--
		for len(rows) <= int(row) {
			rows = append(rows, nil)
		}
		r := rows[row]
		for len(r) <= int(col) {
			r = append(r, "")
		}
		r[col] = v
		rows[row] = r
	}

	var formulaRow, formulaCol uint16
	formulaPending := false
	for {
		rec, ok := next()
		if !ok || rec.typ == recEOF {
			break
		}
		d := rec.data
		if len(d) < 6 && rec.typ != recString {
			continue
		}
		switch rec.typ {
		case recLabelSST:
			if len(d) >= 10 {
				if i := int(binary.LittleEndian.Uint32(d[6:])); i < len(b.sst) {
					set(u16(d, 0), u16(d, 2), b.sst[i])
				}
			}
		case recLabel:
			s, _ := unicodeString(d[6:], 2)
			set(u16(d, 0), u16(d, 2), s)
		case recNumber:
			if len(d) >= 14 {
				v := math.Float64frombits(binary.LittleEndian.Uint64(d[6:]))
				set(u16(d, 0), u16(d, 2), b.format(v, u16(d, 4)))
			}
		case recRK:
			if len(d) >= 10 {
				set(u16(d, 0), u16(d, 2), b.format(rkValue(binary.LittleEndian.Uint32(d[6:])), u16(d, 4)))
			}
		case recMulRK:
			row, col := u16(d, 0), u16(d, 2)
			for p := 4; p+6 <= len(d)-2; p += 6 {
				set(row, col, b.format(rkValue(binary.LittleEndian.Uint32(d[p+2:])), u16(d, p)))
				col++
			}
		case recBoolErr:
			if len(d) >= 8 {
				set(u16(d, 0), u16(d, 2), boolErr(d[6], d[7]))
			}
		case recFormula:
			if len(d) < 14 {
				continue
			}
			row, col, xf := u16(d, 0), u16(d, 2), u16(d, 4)
			res := d[6:14]
			if res[6] != 0xFF || res[7] != 0xFF {
				set(row, col, b.format(math.Float64frombits(binary.LittleEndian.Uint64(res)), xf))
				continue
			}
			switch res[0] {
			case 0:
				formulaRow, formulaCol, formulaPending = row, col, true
			case 1:
				set(row, col, boolErr(res[2], 0))
			case 2:
				set(row, col, boolErr(res[2], 1))
			}
		case recString:
			// A string formula result follows its FORMULA record.
			if formulaPending {
				s, _ := unicodeString(d, 2)
				set(formulaRow, formulaCol, s)
				formulaPending = false
			}
		}
	}
	return rows
}

func u16(b []byte, at int) uint16 { return binary.LittleEndian.Uint16(b[at:]) }

func rkValue(rk uint32) float64 {
	var v float64
	if rk&0x02 != 0 {
		v = float64(int32(rk) >> 2)
	} else {
		v = math.Float64frombits(uint64(rk&0xFFFFFFFC) << 32)
	}
	if rk&0x01 != 0 {
		v /= 100
	}
	return v
}

func boolErr(v, isErr byte) string {
	if isErr == 0 {
		if v != 0 {
			return "TRUE"
		}
		return "FALSE"
	}
	switch v {
	case 0x00:
		return "#NULL!"
	case 0x07:
		return "#DIV/0!"
	case 0x0F:
		return "#VALUE!"
	case 0x17:
		return "#REF!"
	case 0x1D:
		return "#NAME?"
	case 0x24:
		return "#NUM!"
	case 0x2A:
		return "#N/A"
	}
	return "#ERROR"
}

func shortString(b []byte) (string, int) {
	if len(b) < 2 {
		return "", len(b)
	}
	return decodeChars(b[2:], int(b[0]), b[1]&0x01 != 0)
}

// unicodeString reads an XLUnicodeString whose length field is lenSize bytes wide.
func unicodeString(b []byte, lenSize int) (string, int) {
	if len(b) < lenSize+1 {
		return "", len(b)
	}
	n := int(b[0])
	if lenSize == 2 {
		n = int(binary.LittleEndian.Uint16(b))
	}
	s, used := decodeChars(b[lenSize+1:], n, b[lenSize]&0x01 != 0)
	return s, lenSize + 1 + used
}

func decodeChars(b []byte, n int, wide bool) (string, int) {
	if wide {
		if 2*n > len(b) {
			n = len(b) / 2
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		return string(utf16.Decode(u)), 2 * n
	}
	if n > len(b) {
		n = len(b)
	}
	r := make([]rune, n)
	for i, c := range b[:n] {
		r[i] = rune(c)
	}
	return string(r), n
}

// sstReader walks the SST record and its CONTINUE records. A string may be split
// across a record boundary; the continuation then starts with a fresh flags byte.
type sstReader struct {
	segs [][]byte
	seg  int
	pos  int
}

func (r *sstReader) remaining() int {
	if r.seg >= len(r.segs) {
		return 0
	}
	return len(r.segs[r.seg]) - r.pos
}

func (r *sstReader) advance() bool {
	for r.remaining() <= 0 {
		r.seg++
		r.pos = 0
		if r.seg >= len(r.segs) {
			return false
		}
	}
	return true
}

func (r *sstReader) bytes(n int) ([]byte, bool) {
	var out []byte
	for n > 0 {
		if !r.advance() {
			return out, false
		}
		k := min(n, r.remaining())
		out = append(out, r.segs[r.seg][r.pos:r.pos+k]...)
		r.pos += k
		n -= k
	}
	return out, true
}

func (r *sstReader) u16() (uint16, bool) {
	b, ok := r.bytes(2)
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b), true
}

func (r *sstReader) str() (string, bool) {
	cch, ok := r.u16()
	if !ok {
		return "", false
	}
	flags, ok := r.bytes(1)
	if !ok {
		return "", false
	}
	wide := flags[0]&0x01 != 0
	var runs, ext int
	if flags[0]&0x08 != 0 {
		n, ok := r.u16()
		if !ok {
			return "", false
		}
		runs = int(n)
	}
	if flags[0]&0x04 != 0 {
		b, ok := r.bytes(4)
		if !ok {
			return "", false
		}
		ext = int(int32(binary.LittleEndian.Uint32(b)))
	}

	var sb strings.Builder
	left := int(cch)
	for left > 0 {
		if !r.advance() {
			return sb.String(), false
		}
		width := 1
		if wide {
			width = 2
		}
		k := min(left, r.remaining()/width)
		if k == 0 {
			return sb.String(), false
		}
		s, _ := decodeChars(r.segs[r.seg][r.pos:], k, wide)
		sb.WriteString(s)
		r.pos += k * width
		left -= k
		if left > 0 {
			if r.remaining() > 0 || !r.advance() {
				return sb.String(), false
			}
			f, _ := r.bytes(1)
			wide = f[0]&0x01 != 0
		}
	}
	if _, ok := r.bytes(4*runs + max(ext, 0)); !ok && (runs > 0 || ext > 0) {
		return sb.String(), false
	}
	return sb.String(), true
}

func parseSST(segs [][]byte) []string {
	if len(segs) == 0 || len(segs[0]) < 8 {
		return nil
	}
	unique := int(binary.LittleEndian.Uint32(segs[0][4:]))
	r := &sstReader{segs: segs, pos: 8}
	out := make([]string, 0, min(unique, 1<<20))
	for len(out) < unique {
		s, ok := r.str()
		if !ok {
			break
		}
		out = append(out, s)
	}
	return out
}

// builtinFormats are the number formats BIFF8 does not store, see [MS-XLS] 2.4.126.
var builtinFormats = map[uint16]string{
	1: "0", 2: "0.00", 3: "#,##0", 4: "#,##0.00", 9: "0%", 10: "0.00%", 11: "0.00E+00",
	14: "yyyy-mm-dd", 15: "d-mmm-yy", 16: "d-mmm", 17: "mmm-yy",
	18: "h:mm AM/PM", 19: "h:mm:ss AM/PM", 20: "h:mm", 21: "h:mm:ss", 22: "yyyy-mm-dd h:mm",
	37: "#,##0", 38: "#,##0", 39: "#,##0.00", 40: "#,##0.00",
	45: "mm:ss", 46: "[h]:mm:ss", 47: "mm:ss.0", 48: "##0.0E+0", 49: "@",
}

func (b *biffBook) format(v float64, xf uint16) string {
	code := ""
	if int(xf) < len(b.xfFmt) {
		id := b.xfFmt[xf]
		if c, ok := b.formats[id]; ok {
			code = c
		} else {
			code = builtinFormats[id]
		}
	}
	return formatNumber(v, code, b.date1904)
}

// formatNumber renders v the way its number format classifies it: as an ISO date
// or time, a percentage, a fixed number of decimals, or the shortest exact form.
// It does not reproduce locale-specific separators.
func formatNumber(v float64, code string, date1904 bool) string {
	section := code
	if i := strings.IndexByte(section, ';'); i >= 0 {
		section = section[:i]
	}
	plain := stripLiterals(section)
	lower := strings.ToLower(plain)

	if strings.ContainsAny(lower, "ymdhs") && !strings.Contains(lower, "general") {
		return formatSerial(v, lower, date1904)
	}
	if strings.Contains(plain, "%") {
		return fixed(v*100, decimals(plain)) + "%"
	}
	if strings.ContainsAny(plain, "Ee") && strings.Contains(plain, "0") {
		return strconv.FormatFloat(v, 'E', decimals(plain), 64)
	}
	if strings.Contains(plain, "0") || strings.Contains(plain, "#") {
		s := fixed(v, decimals(plain))
		if strings.Contains(plain, ",") {
			s = groupThousands(s)
		}
		return s
	}
	return strconv.FormatFloat(roundSignificant(v), 'f', -1, 64)
}

// stripLiterals removes quoted text, escaped characters and bracketed colour or
// condition sections, so only the format tokens remain.
func stripLiterals(code string) string {
	var sb strings.Builder
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '"':
			for i++; i < len(code) && code[i] != '"'; i++ {
			}
		case '\\', '_', '*':
			i++
		case '[':
			j := strings.IndexByte(code[i:], ']')
			if j < 0 {
				return sb.String()
			}
			if tok := strings.ToLower(code[i+1 : i+j]); tok == "h" || tok == "hh" || tok == "m" || tok == "mm" || tok == "s" || tok == "ss" {
				sb.WriteString(tok)
			}
			i += j
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func decimals(plain string) int {
	i := strings.IndexByte(plain, '.')
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range plain[i+1:] {
		if c != '0' && c != '#' {
			break
		}
		n++
	}
	return n
}

func roundSignificant(v float64) float64 {
	if v == 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return v
	}
	s := strconv.FormatFloat(v, 'g', 15, 64)
	r, _ := strconv.ParseFloat(s, 64)
	return r
}

func formatSerial(v float64, lowerCode string, date1904 bool) string {
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if date1904 {
		epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	t := epoch.Add(time.Duration(math.Round(v*86400)) * time.Second)
	hasDate := strings.ContainsAny(lowerCode, "yd") || (strings.Contains(lowerCode, "m") && !strings.ContainsAny(lowerCode, "hs"))
	hasTime := strings.ContainsAny(lowerCode, "hs")
	switch {
	case hasDate && hasTime:
		return t.Format("2006-01-02 15:04:05")
	case hasTime:
		if v >= 1 {
			h := int(v * 24)
			return fmt.Sprintf("%d:%02d:%02d", h, t.Minute(), t.Second())
		}
		return t.Format("15:04:05")
	default:
		return t.Format("2006-01-02")
	}
}

// fixed rounds half away from zero, as Excel displays it, after trimming the
// binary noise below 15 significant digits (2878.555 shows as 2878.56).
func fixed(v float64, decimals int) string {
	p := math.Pow10(decimals)
	return strconv.FormatFloat(math.Round(roundSignificant(v)*p)/p, 'f', decimals, 64)
}

// groupThousands inserts "," every three integer digits, as excelize renders
// "#,##0" formats for .xlsx, so both workbook formats read the same.
func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + b.String() + frac
}
