package pdfnative

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestRenderReadsPermissionOnlyEncryptedPDF(t *testing.T) {
	plain := buildPDF(t, synthPage{runs: []textRun{
		{text: "Minutes of the meeting", x: 72, yTop: 120, size: 10},
	}})
	disableConfigDir.Do(api.DisableConfigDir)
	conf := model.NewAESConfiguration("", "ownerpass", 128)
	var enc bytes.Buffer
	if err := api.Encrypt(bytes.NewReader(plain), &enc, conf); err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}

	md, err := Extract(enc.Bytes())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(md, "Minutes") {
		t.Errorf("text of the encrypted page missing:\n%s", md)
	}
}

func TestRenderSkipsPageWithMalformedContentStream(t *testing.T) {
	data := rawPDF(
		"BT /F1 12 Tf 72 720 Td (First page is readable) Tj ET",
		"BT /F1 12 Tf 72 720 Td <6 g ET",
		"BT /F1 12 Tf 72 720 Td (Third page likewise) Tj ET",
	)

	md, err := Extract(data)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, want := range []string{"First page is readable", "Third page likewise"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q missing; one broken page must not lose the others:\n%s", want, md)
		}
	}
}

func rawPDF(contents ...string) []byte {
	n := len(contents)
	var objs []string
	kids := make([]string, n)
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	for i, c := range contents {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c),
		)
	}

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
