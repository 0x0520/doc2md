package sheets

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCSV_SemicolonDelimited(t *testing.T) {
	md, _, err := ToMarkdown("data.csv", []byte("ID;Name\n1;Alpha\n2;Beta\n"))
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	want := "| ID | Name |\n|---|---|\n| 1 | Alpha |\n| 2 | Beta |\n"
	if md != want {
		t.Errorf("csv mismatch:\n got: %q\nwant: %q", md, want)
	}
}

func TestCSV_PipeDelimited(t *testing.T) {
	md, _, err := ToMarkdown("export.csv", []byte("Key|Summary|Status\nABC-1|Do the thing|Open\n"))
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	want := "| Key | Summary | Status |\n|---|---|---|\n| ABC-1 | Do the thing | Open |\n"
	if md != want {
		t.Errorf("pipe-csv mismatch:\n got: %q\nwant: %q", md, want)
	}
}

func TestSpreadsheetML_IndexAndMergeAndNewline(t *testing.T) {
	xml := `<?xml version="1.0"?>
<?mso-application progid="Excel.Sheet"?>
<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">
<Worksheet ss:Name="S1"><Table>
<Row>
  <Cell><Data ss:Type="String">A</Data></Cell>
  <Cell><Data ss:Type="String">B</Data></Cell>
  <Cell><Data ss:Type="String">C</Data></Cell>
</Row>
<Row>
  <Cell><Data ss:Type="String">first
line</Data></Cell>
  <Cell ss:Index="3"><Data ss:Type="String">third</Data></Cell>
</Row>
</Table></Worksheet>
</Workbook>`
	md, _, err := ToMarkdown("legacy.xls", []byte(xml))
	if err != nil {
		t.Fatalf("xmlss: %v", err)
	}
	if !strings.Contains(md, "# S1") {
		t.Errorf("missing sheet heading:\n%s", md)
	}
	if !strings.Contains(md, "| A | B | C |") {
		t.Errorf("missing header row:\n%s", md)
	}
	if !strings.Contains(md, "| first line |  | third |") {
		t.Errorf("index/newline handling wrong:\n%s", md)
	}
}

func TestSpreadsheetML_HostileIndexBounded(t *testing.T) {
	xml := `<?xml version="1.0"?>
<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">
<Worksheet ss:Name="S1"><Table>
<Row>
  <Cell><Data ss:Type="String">A</Data></Cell>
  <Cell ss:Index="2000000000"><Data ss:Type="String">evil</Data></Cell>
</Row>
<Row>
  <Cell ss:MergeAcross="2000000000"><Data ss:Type="String">B</Data></Cell>
</Row>
</Table></Worksheet>
</Workbook>`
	md, _, err := ToMarkdown("evil.xls", []byte(xml))
	if err != nil {
		t.Fatalf("xmlss: %v", err)
	}
	if !strings.Contains(md, "| A |") {
		t.Errorf("valid cell A dropped:\n%s", md)
	}
	if strings.Contains(md, "evil") {
		t.Errorf("out-of-range cell should have been skipped:\n%s", md)
	}
}

func TestXLSX_ViaExcelize(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	f.SetCellValue("Sheet1", "A1", "H1")
	f.SetCellValue("Sheet1", "B1", "H2")
	f.SetCellValue("Sheet1", "A2", "x")
	f.SetCellValue("Sheet1", "B2", "y")
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	md, _, err := ToMarkdown("book.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("xlsx: %v", err)
	}
	if !strings.Contains(md, "# Sheet1") || !strings.Contains(md, "| H1 | H2 |\n|---|---|\n| x | y |") {
		t.Errorf("xlsx output unexpected:\n%s", md)
	}
}

func TestUnsupportedExt(t *testing.T) {
	if _, _, err := ToMarkdown("file.pdf", []byte("x")); err == nil {
		t.Error("expected ErrUnsupported for .pdf")
	}
}

func xlsxFixture(t *testing.T, build func(*excelize.File)) (*excelize.File, []byte) {
	t.Helper()
	src := excelize.NewFile()
	defer src.Close()
	build(src)
	buf, err := src.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	data := buf.Bytes()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f, data
}

func TestXLSX_FormattedEmptyRowsAreNotMaterialised(t *testing.T) {
	f, data := xlsxFixture(t, func(f *excelize.File) {
		f.SetCellValue("Sheet1", "A1", "H1")
		f.SetCellValue("Sheet1", "A2", "x")
		style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
		if err != nil {
			t.Fatalf("style: %v", err)
		}
		if err := f.SetCellStyle("Sheet1", "A3", "A5000", style); err != nil {
			t.Fatalf("set style: %v", err)
		}
	})

	rows, err := xlsxSheetRows(f, "Sheet1", maxCellsPerSheet)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2 — formatted empty rows were materialised", len(rows))
	}

	md, _, err := ToMarkdown("book.xlsx", data)
	if err != nil {
		t.Fatalf("xlsx: %v", err)
	}
	if !strings.Contains(md, "| H1 |\n|---|\n| x |") {
		t.Errorf("unexpected output:\n%s", md)
	}
}

func TestXLSX_GapRowsKeepRowNumbersAligned(t *testing.T) {
	f, _ := xlsxFixture(t, func(f *excelize.File) {
		f.SetCellValue("Sheet1", "A1", "top")
		f.SetCellValue("Sheet1", "A3", "bottom")
	})

	rows, err := xlsxSheetRows(f, "Sheet1", maxCellsPerSheet)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if len(rows[1]) != 0 {
		t.Errorf("row 2 should be the blank gap row, got %q", rows[1])
	}
	if rows[2][0] != "bottom" {
		t.Errorf("row 3 lost its value: %q", rows[2])
	}
}

func TestXLSX_CapStopsReading(t *testing.T) {
	f, _ := xlsxFixture(t, func(f *excelize.File) {
		for r := 1; r <= 4; r++ {
			f.SetSheetRow("Sheet1", fmt.Sprintf("A%d", r), &[]string{"a", "b"})
		}
	})

	rows, err := xlsxSheetRows(f, "Sheet1", 5)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2 under a 5-cell cap", len(rows))
	}
}

func TestXLSX_GapRowsCountAgainstCap(t *testing.T) {
	f, _ := xlsxFixture(t, func(f *excelize.File) {
		f.SetCellValue("Sheet1", "A1", "top")
		f.SetCellValue("Sheet1", "A50", "far below")
	})

	rows, err := xlsxSheetRows(f, "Sheet1", 5)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("got %d rows, want 1 — the 48-row gap should have hit the cap", len(rows))
	}
}
