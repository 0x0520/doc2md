package pdfnative

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	synthPageW = 595.0
	synthPageH = 842.0
)

type textRun struct {
	text string
	x    float64
	yTop float64
	size int
	bold bool
}

type synthPage struct {
	runs []textRun
}

func buildPDF(t testing.TB, pages ...synthPage) []byte {
	t.Helper()

	jsonPages := make(map[string]any, len(pages))
	for i, p := range pages {
		texts := make([]any, 0, len(p.runs))
		for _, r := range p.runs {
			font := "Helvetica"
			if r.bold {
				font = "Helvetica-Bold"
			}
			texts = append(texts, map[string]any{
				"value": r.text,
				"pos":   []float64{r.x, synthPageH - r.yTop},
				"font":  map[string]any{"name": font, "size": r.size},
			})
		}
		jsonPages[strconv.Itoa(i+1)] = map[string]any{
			"content": map[string]any{"text": texts},
		}
	}

	desc, err := json.Marshal(map[string]any{"paper": "A4", "pages": jsonPages})
	if err != nil {
		t.Fatalf("marshal page description: %v", err)
	}

	var out bytes.Buffer
	if err := api.Create(nil, bytes.NewReader(desc), &out, nil); err != nil {
		t.Fatalf("pdfcpu create: %v\ndescription: %s", err, desc)
	}
	return out.Bytes()
}

func TestSynthPDFRoundTrip(t *testing.T) {
	data := buildPDF(t, synthPage{runs: []textRun{
		{text: "Objectives", x: 72, yTop: 120, size: 18, bold: true},
		{text: "Intake has so far been done by phone or at the café.", x: 72, yTop: 150, size: 10},
	}})

	md, err := Extract(data)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	t.Logf("markdown:\n%s", md)

	if !bytes.Contains([]byte(md), []byte("Objectives")) {
		t.Errorf("heading text missing from rendering:\n%s", md)
	}
	if !bytes.Contains([]byte(md), []byte("café")) {
		t.Errorf("body text missing from rendering:\n%s", md)
	}
}
