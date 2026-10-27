package doc2md

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

var fuzzExts = []string{
	".pdf", ".docx", ".pptx", ".xlsx", ".xls", ".csv", ".odt", ".ods", ".odp",
	".epub", ".rtf", ".html", ".xml", ".adoc", ".md", "",
}

func minimalPDF(text string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	stream := fmt.Sprintf("BT /F1 18 Tf 72 720 Td (%s) Tj ET BT /F1 11 Tf 72 690 Td (Body text follows here.) Tj ET", text)
	objs[3] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func FuzzConvert(f *testing.F) {
	seeds := map[string][]byte{
		".pdf":  minimalPDF("Heading"),
		".docx": docxWithPicture(f),
		".pptx": pptxWithTwoPictures(f),
		".xlsx": xlsxWithPicture(f),
		".odt":  odtWithPicture(f),
		".epub": epubWithPicture(f),
		".html": htmlWithDataURI(f),
		".rtf":  rtfWithPicture(f),
		".csv":  []byte("a;b\n1;2\n"),
		".xml":  []byte("<root><item><a>1</a></item><item><a>2</a></item></root>"),
		".adoc": []byte("= Title\n\n== Section\n\n|===\n|a |b\n|1 |2\n|===\n"),
	}
	for i, ext := range fuzzExts {
		if data, ok := seeds[ext]; ok {
			f.Add(uint8(i), data)
		}
	}
	f.Fuzz(func(t *testing.T, pick uint8, data []byte) {
		ext := fuzzExts[int(pick)%len(fuzzExts)]
		start := time.Now()
		_, _, _ = Convert("fuzz"+ext, data)
		_, _, _ = ConvertVerbatim("fuzz"+ext, data)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("%q, %d bytes: took %v", ext, len(data), d)
		}
	})
}
