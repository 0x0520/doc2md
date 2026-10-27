// Package sheets converts spreadsheets (.xlsx, .xlsm, legacy .xls including Excel
// 2003 XML, and CSV) to Markdown tables, one section per sheet.
package sheets

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/0x0520/doc2md/internal/docimg"
	"github.com/0x0520/doc2md/internal/mdrender"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// ErrUnsupported is returned for a filename extension this package does not read.
var ErrUnsupported = errors.New("not a supported spreadsheet format")

// maxCellsPerSheet bounds the cells read from one sheet.
const maxCellsPerSheet = 4 << 20

// ToMarkdown converts a spreadsheet to Markdown. The filename's extension picks the
// reader, because CSV and workbooks cannot be told apart reliably by content.
// Figures are only extracted from .xlsx and .xlsm.
func ToMarkdown(filename string, data []byte) (string, []docimg.Image, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx", ".xlsm":
		return xlsxToMarkdown(data)
	case ".xls":
		// Excel 2003 "XML Spreadsheet" files are saved with an .xls extension.
		if isSpreadsheetML(data) {
			md, err := spreadsheetMLToMarkdown(data)
			return md, nil, err
		}
		md, err := xlsToMarkdown(data)
		return md, nil, err
	case ".csv":
		md, err := csvToMarkdown(data)
		return md, nil, err
	default:
		return "", nil, ErrUnsupported
	}
}

// sheet is one worksheet as raw rows. breaks maps a row index to a section title
// that splits the table there; figures are image markers appended after the table.
type sheet struct {
	name    string
	rows    [][]string
	figures []string
	breaks  map[int]string
}

// render joins the sheets into one document. Sheets with no content are skipped,
// and a name heading is only added when headings is set (not for CSV).
func render(sheets []sheet, headings bool) (string, error) {
	var blocks []mdrender.Block
	for _, s := range sheets {
		body := renderSheetBody(s)
		if len(body) == 0 {
			continue
		}
		if headings && s.name != "" {
			blocks = append(blocks, mdrender.Block{Kind: mdrender.Heading, Level: 2, Text: s.name})
		}
		blocks = append(blocks, body...)
	}
	if len(blocks) == 0 {
		return "", errors.New("no tabular content found")
	}
	return mdrender.Document(blocks), nil
}

func renderSheetBody(s sheet) []mdrender.Block {
	var blocks []mdrender.Block
	var acc [][]string
	flush := func() {
		grid := mdrender.NormalizeGrid(acc)
		if len(grid) > 0 {
			blocks = append(blocks, mdrender.Block{Kind: mdrender.Table_, Text: mdrender.Table(grid)})
		}
		acc = nil
	}
	for i, row := range s.rows {
		if text, ok := s.breaks[i]; ok {
			flush()
			blocks = append(blocks, mdrender.Block{Kind: mdrender.Heading, Level: 3, Text: text})
			continue
		}
		acc = append(acc, row)
	}
	flush()
	for _, m := range s.figures {
		blocks = append(blocks, mdrender.Block{Kind: mdrender.Paragraph, Text: m})
	}
	return blocks
}

func xlsxToMarkdown(data []byte) (string, []docimg.Image, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	var (
		sheets []sheet
		images []docimg.Image
	)
	for i, name := range f.GetSheetList() {
		rows, err := xlsxSheetRows(f, name, maxCellsPerSheet)
		if err != nil {
			continue
		}
		pics := sheetPictures(f, name, i+1, len(images))
		markers := make([]string, 0, len(pics))
		for _, pic := range pics {
			markers = append(markers, docimg.MarkerFor(pic.Seq))
		}
		images = append(images, pics...)
		sheets = append(sheets, sheet{name: name, rows: rows, figures: markers})
	}
	md, err := render(sheets, true)
	if err != nil {
		return "", nil, err
	}
	return md, images, nil
}

// xlsxSheetRows streams a sheet's rows, stopping at maxCells.
func xlsxSheetRows(f *excelize.File, name string, maxCells int) ([][]string, error) {
	it, err := f.Rows(name)
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var rows [][]string
	cur, filled, cells := 0, 0, 0
	for it.Next() {
		cur++
		cols, err := it.Columns()
		if err != nil {
			break
		}
		if len(cols) == 0 {
			continue
		}
		// gap is the number of empty rows skipped since the last stored one; it keeps row
		// positions and counts toward the cap, so a sparse sheet cannot allocate unbounded memory.
		gap := cur - filled - 1
		if cells+gap+len(cols) > maxCells {
			break
		}
		rows = append(rows, make([][]string, gap)...)
		rows = append(rows, cols)
		cells += gap + len(cols)
		filled = cur
	}
	return rows, nil
}

// xlsToMarkdown converts a legacy BIFF8 workbook (Excel 97 to 2003).
func xlsToMarkdown(data []byte) (string, error) {
	sheets, err := readBIFF8(data)
	if err != nil {
		return "", fmt.Errorf("open xls: %w", err)
	}
	return render(sheets, true)
}

func csvToMarkdown(data []byte) (string, error) {
	data = decodeText(data)
	r := csv.NewReader(bytes.NewReader(data))
	// Rows may have different lengths, and stray quotes are tolerated.
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.Comma = sniffDelimiter(data)
	var rows [][]string
	for {
		// A malformed record is skipped rather than failing the whole file.
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		rows = append(rows, rec)
	}
	return render([]sheet{{rows: rows}}, false)
}

// decodeText returns data as UTF-8. Excel writes CSV as UTF-8 with a BOM, as
// UTF-16 with a BOM ("Unicode text"), or in the ANSI code page; text that is not
// valid UTF-8 is taken as Windows-1252, the code page of most such files.
func decodeText(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		out, err := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if err == nil {
			return out
		}
	case !utf8.Valid(data):
		if out, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
			return out
		}
	}
	return data
}

// sniffDelimiter picks the separator (comma, semicolon, tab or pipe) that occurs
// most often outside quotes on the first record; ties and none default to comma.
func sniffDelimiter(data []byte) rune {
	counts := map[byte]int{}
	quoted := false
	for _, c := range data {
		if c == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		if c == '\n' {
			break
		}
		counts[c]++
	}
	best, bestN := ',', 0
	for _, d := range []byte{',', ';', '\t', '|'} {
		if counts[d] > bestN {
			best, bestN = rune(d), counts[d]
		}
	}
	return best
}

// sheetPictures returns the pictures on a sheet, numbered after alreadyTaken so
// figure sequence numbers stay unique across the workbook.
func sheetPictures(f *excelize.File, name string, sheetNum, alreadyTaken int) []docimg.Image {
	cells, err := f.GetPictureCells(name)
	if err != nil {
		return nil
	}
	var (
		out   []docimg.Image
		total int
	)
	for _, cell := range cells {
		pics, perr := f.GetPictures(name, cell)
		if perr != nil {
			continue
		}
		for _, pic := range pics {
			if total >= docimg.MaxTotalBytes {
				return out
			}
			ext := docimg.Ext(pic.Extension)
			if ext == "" || len(pic.File) == 0 {
				continue
			}
			total += len(pic.File)
			out = append(out, docimg.Image{
				Seq:   alreadyTaken + len(out) + 1,
				Page:  sheetNum,
				Bytes: pic.File,
				Ext:   ext,
			})
		}
	}
	return out
}
