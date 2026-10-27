package pdfnative

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type PageBreak struct {
	Offset int
	Page   int
}

func Extract(data []byte) (string, error) {
	md, _, err := Render(data)
	return md, err
}

func Render(data []byte) (string, []Image, error) {
	md, images, _, err := RenderWithPages(data)
	return md, images, err
}

func RenderWithPages(data []byte) (string, []Image, []PageBreak, error) {
	return renderPDF(data, false)
}

func RenderVerbatim(data []byte) (string, []Image, []PageBreak, error) {
	return renderPDF(data, true)
}

func renderPDF(data []byte, verbatim bool) (string, []Image, []PageBreak, error) {
	md, images, breaks, err := renderPDFOnce(data, verbatim)
	if err == nil {
		return md, images, breaks, nil
	}
	repaired, rerr := rewritePDF(data)
	if rerr != nil {
		return "", nil, nil, err
	}
	md, images, breaks, err2 := renderPDFOnce(repaired, verbatim)
	if err2 != nil {
		return "", nil, nil, err
	}
	return md, images, breaks, nil
}

func rewritePDF(data []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("rewrite pdf: %v", r)
		}
	}()
	disableConfigDir.Do(api.DisableConfigDir)
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	var dec bytes.Buffer
	if err := api.Decrypt(bytes.NewReader(data), &dec, conf); err == nil {
		return dec.Bytes(), nil
	}
	conf = model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	var buf bytes.Buffer
	if err := api.Optimize(bytes.NewReader(data), &buf, conf); err != nil {
		return nil, fmt.Errorf("rewrite pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func pageTokens(p pdf.Page) (toks []pdf.Text, places []imgPlace, height float64, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	toks, places = extractTokens(p)
	return toks, places, pageHeight(p), true
}

func renderPDFOnce(data []byte, verbatim bool) (md string, images []Image, breaks []PageBreak, err error) {
	defer func() {
		if r := recover(); r != nil {
			md, images, breaks, err = "", nil, nil, fmt.Errorf("read pdf: %v", r)
		}
	}()
	return renderPDFBody(data, verbatim)
}

func renderPDFBody(data []byte, verbatim bool) (string, []Image, []PageBreak, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, nil, fmt.Errorf("open pdf: %w", err)
	}

	var pages []page
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		toks, places, height, ok := pageTokens(p)
		if !ok {
			continue
		}
		pages = append(pages, page{
			rows:   pageRows(toks),
			height: height,
			num:    i,
			places: places,
		})
	}
	if len(pages) == 0 {
		return "", nil, nil, fmt.Errorf("no pages with content")
	}

	if !verbatim {
		pages = dropCoverPage(pages)
		pages = dropTOCPages(pages)
		dropRunningHeadFoot(pages)
	}
	for pi := range pages {
		pages[pi].rows = splitColumnRows(pages[pi].rows)
		pages[pi].lines = assembleRows(pages[pi].rows, verbatim)
		for li := range pages[pi].lines {
			pages[pi].lines[li].page = pages[pi].num
		}
	}
	images := placeImages(pages, extractRasterImages(data), r.NumPage(), verbatim)

	body := bodyFontSize(pages)
	blocks := groupBlocks(pages, body, verbatim)

	md, breaks := renderWithBreaks(blocks)
	if strings.TrimSpace(md) == "" {
		return "", nil, nil, fmt.Errorf("no text could be extracted")
	}
	return md, images, breaks, nil
}

func pageHeight(p pdf.Page) float64 {
	mb := inherited(p.V, "MediaBox")
	if mb.Kind() != pdf.Array || mb.Len() < 4 {
		return 0
	}
	return mb.Index(3).Float64() - mb.Index(1).Float64()
}

func inherited(v pdf.Value, key string) pdf.Value {
	for ; !v.IsNull(); v = v.Key("Parent") {
		if r := v.Key(key); !r.IsNull() {
			return r
		}
	}
	return pdf.Value{}
}
