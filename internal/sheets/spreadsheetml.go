package sheets

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// maxCols is Excel's column limit; it bounds sparse Index and MergeAcross values.
const maxCols = 16384

// isSpreadsheetML reports whether data looks like Excel 2003 XML rather than BIFF.
func isSpreadsheetML(data []byte) bool {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	s := strings.TrimSpace(string(head))
	return strings.HasPrefix(s, "<?xml") || strings.HasPrefix(s, "<Workbook") ||
		bytes.Contains(head, []byte("urn:schemas-microsoft-com:office:spreadsheet"))
}

// boldStyles returns the ids of styles with a bold font, used to find section-title rows.
func boldStyles(data []byte) map[string]bool {
	var wb struct {
		Styles struct {
			Style []struct {
				ID   string `xml:"ID,attr"`
				Font struct {
					Bold string `xml:"Bold,attr"`
				} `xml:"Font"`
			} `xml:"Style"`
		} `xml:"Styles"`
	}
	if xml.Unmarshal(data, &wb) != nil {
		return nil
	}
	bold := make(map[string]bool)
	for _, st := range wb.Styles.Style {
		if st.Font.Bold != "" && st.Font.Bold != "0" {
			bold[st.ID] = true
		}
	}
	return bold
}

// spreadsheetMLToMarkdown reads Excel 2003 XML. The cell Index attribute is
// 1-based and lets a row skip columns; MergeAcross widens a cell over following columns.
func spreadsheetMLToMarkdown(data []byte) (string, error) {
	bold := boldStyles(data)

	var wb struct {
		Worksheets []struct {
			Name  string `xml:"Name,attr"`
			Table struct {
				Rows []struct {
					Index string `xml:"Index,attr"`
					Cells []struct {
						Index       string `xml:"Index,attr"`
						StyleID     string `xml:"StyleID,attr"`
						MergeAcross string `xml:"MergeAcross,attr"`
						Data        struct {
							Value string `xml:",chardata"`
						} `xml:"Data"`
					} `xml:"Cell"`
				} `xml:"Row"`
			} `xml:"Table"`
		} `xml:"Worksheet"`
	}
	if err := xml.Unmarshal(data, &wb); err != nil {
		return "", fmt.Errorf("parse SpreadsheetML: %w", err)
	}

	var sheets []sheet
	for _, ws := range wb.Worksheets {
		var rows [][]string
		breaks := map[int]string{}
		cells := 0
		for _, r := range ws.Table.Rows {
			if cells >= maxCellsPerSheet {
				break
			}
			// Row Index is 1-based and skips empty rows; the gap counts against the cap.
			if n, err := strconv.Atoi(r.Index); err == nil && n-1 > len(rows) {
				gap := n - 1 - len(rows)
				if gap > maxCellsPerSheet-cells {
					break
				}
				rows = append(rows, make([][]string, gap)...)
				cells += gap
			}
			var row []string
			nonEmpty, lastStyle, lastValue := 0, "", ""
			col := 0
			for _, c := range r.Cells {
				if c.Index != "" {
					if n, err := strconv.Atoi(c.Index); err == nil && n >= 1 {
						col = n - 1
					}
				}
				if col >= maxCols {
					continue
				}
				for len(row) <= col {
					row = append(row, "")
				}
				value := strings.TrimSpace(c.Data.Value)
				row[col] = value
				if value != "" {
					nonEmpty++
					lastStyle, lastValue = c.StyleID, value
				}
				span := 0
				if c.MergeAcross != "" {
					if n, err := strconv.Atoi(c.MergeAcross); err == nil && n > 0 {
						span = n
					}
				}
				if span > maxCols {
					span = maxCols
				}
				col += span + 1
			}
			cells += len(row)
			// A row with a single bold value is a section title: it splits the table and becomes a heading.
			if nonEmpty == 1 && bold[lastStyle] {
				breaks[len(rows)] = lastValue
			}
			rows = append(rows, row)
		}
		sheets = append(sheets, sheet{name: ws.Name, rows: rows, breaks: breaks})
	}
	return render(sheets, true)
}
