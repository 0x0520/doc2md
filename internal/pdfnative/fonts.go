package pdfnative

import "github.com/ledongthuc/pdf"

// fontMetrics holds glyph advance widths in 1/1000 em: per CID for Type0 fonts, per
// character code (the Widths array) for simple fonts.
type fontMetrics struct {
	type0     bool
	cidW      map[int]float64
	dw        float64
	widths    []float64
	firstChar int
	missing   float64
}

// width returns the advance of a character code, falling back to a per-character
// estimate when the font has no usable entry.
func (m *fontMetrics) width(code int, s string) float64 {
	if m == nil {
		return estimateEm(s)
	}
	if m.type0 {
		if w, ok := m.cidW[code]; ok && w != 0 {
			return w
		}
		if m.dw > 0 {
			return m.dw
		}
		return estimateEm(s)
	}
	if idx := code - m.firstChar; idx >= 0 && idx < len(m.widths) {
		if w := m.widths[idx]; w != 0 {
			return w
		}
	}
	if m.missing > 0 {
		return m.missing
	}
	return estimateEm(s)
}

// buildMetrics reads /W and /DW of the descendant font for Type0 fonts, otherwise
// /Widths, /FirstChar and /MissingWidth.
func buildMetrics(font pdf.Font) *fontMetrics {
	v := font.V
	if v.Key("Subtype").Name() == "Type0" {
		d := v.Key("DescendantFonts").Index(0)
		return &fontMetrics{
			type0: true,
			cidW:  parseCIDWidths(d.Key("W")),
			dw:    d.Key("DW").Float64(),
		}
	}
	m := &fontMetrics{firstChar: int(v.Key("FirstChar").Int64())}
	w := v.Key("Widths")
	for i := 0; i < w.Len(); i++ {
		m.widths = append(m.widths, w.Index(i).Float64())
	}
	m.missing = v.Key("FontDescriptor").Key("MissingWidth").Float64()
	return m
}

// parseCIDWidths parses a /W array, which mixes "c [w1 w2 ...]" and "cFirst cLast w"
// entries (PDF 32000-1 §9.7.4.3).
func parseCIDWidths(w pdf.Value) map[int]float64 {
	out := map[int]float64{}
	if w.Kind() != pdf.Array {
		return out
	}
	for i := 0; i < w.Len(); {
		c := int(w.Index(i).Int64())
		i++
		if i >= w.Len() {
			break
		}
		nx := w.Index(i)
		if nx.Kind() == pdf.Array {
			for j := 0; j < nx.Len(); j++ {
				out[c+j] = nx.Index(j).Float64()
			}
			i++
			continue
		}
		cLast := int(nx.Int64())
		i++
		if i >= w.Len() {
			break
		}
		wv := w.Index(i).Float64()
		i++
		// The span cap keeps a corrupt range from allocating a huge map.
		for cc := c; cc <= cLast && cc-c < 1<<16; cc++ {
			out[cc] = wv
		}
	}
	return out
}

// estimateEm approximates a character's width in 1/1000 em when the font gives none.
func estimateEm(s string) float64 {
	return estimateWidth(s, 1000)
}
