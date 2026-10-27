package pdfnative

import (
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractTokens(p pdf.Page) ([]pdf.Text, []imgPlace) {
	contents := p.V.Key("Contents")
	if contents.Kind() == pdf.Null {
		return nil, nil
	}

	g := gstate{ctm: identM, tm: identM, tlm: identM, th: 1}
	metricsCache := map[string]*fontMetrics{}
	decoderCache := map[string]map[int]string{}
	var enc pdf.TextEncoding
	var stack []gstate
	var out []pdf.Text
	var places []imgPlace

	showText := func(raw string) {
		if g.metrics == nil {
			return
		}
		step := 1
		if g.metrics.type0 {
			step = 2
		}
		for k := 0; k+step <= len(raw); k += step {
			code := int(raw[k])
			if step == 2 {
				code = int(raw[k])<<8 | int(raw[k+1])
			}
			seg := raw[k : k+step]
			txt := seg
			if s, ok := g.decode[code]; ok && s != "" {
				txt = s
			} else if enc != nil {
				txt = enc.Decode(seg)
			}
			txt = expandLigatures(txt)
			w0 := g.metrics.width(code, txt)

			trm := textScale(g.tfs, g.th, g.trise).mul(g.tm).mul(g.ctm)
			out = append(out, pdf.Text{
				Font:     g.baseFont,
				FontSize: trm[0][0],
				X:        trm[2][0],
				Y:        trm[2][1],
				W:        w0 / 1000 * trm[0][0],
				S:        txt,
			})

			tx := w0 / 1000 * g.tfs
			if step == 1 && code == 32 {
				tx += g.tw
			}
			tx = (tx + g.tc) * g.th
			g.tm = translateM(tx, 0).mul(g.tm)
		}
	}

	pdf.Interpret(contents, func(stk *pdf.Stack, op string) {
		n := stk.Len()
		args := make([]pdf.Value, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		switch op {
		case "cm":
			if len(args) == 6 {
				g.ctm = matrixFromArgs(args).mul(g.ctm)
			}
		case "q":
			stack = append(stack, g)
		case "Q":
			if len(stack) > 0 {
				g = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "BT":
			g.tm, g.tlm = identM, identM
		case "Td":
			if len(args) == 2 {
				g.tlm = translateM(args[0].Float64(), args[1].Float64()).mul(g.tlm)
				g.tm = g.tlm
			}
		case "TD":
			if len(args) == 2 {
				g.tl = -args[1].Float64()
				g.tlm = translateM(args[0].Float64(), args[1].Float64()).mul(g.tlm)
				g.tm = g.tlm
			}
		case "Tm":
			if len(args) == 6 {
				g.tm = matrixFromArgs(args)
				g.tlm = g.tm
			}
		case "T*":
			g.tlm = translateM(0, -g.tl).mul(g.tlm)
			g.tm = g.tlm
		case "Tc":
			if len(args) == 1 {
				g.tc = args[0].Float64()
			}
		case "Tw":
			if len(args) == 1 {
				g.tw = args[0].Float64()
			}
		case "Tz":
			if len(args) == 1 {
				g.th = args[0].Float64() / 100
			}
		case "TL":
			if len(args) == 1 {
				g.tl = args[0].Float64()
			}
		case "Ts":
			if len(args) == 1 {
				g.trise = args[0].Float64()
			}
		case "Tf":
			if len(args) == 2 {
				name := args[0].Name()
				font := p.Font(name)
				g.tf = font
				g.tfs = args[1].Float64()
				g.baseFont = stripSubsetPrefix(font.BaseFont())
				enc = font.Encoder()
				m, ok := metricsCache[name]
				if !ok {
					m = buildMetrics(font)
					metricsCache[name] = m
				}
				g.metrics = m
				dec, ok := decoderCache[name]
				if !ok {
					dec = buildDecoder(font)
					decoderCache[name] = dec
				}
				g.decode = dec
			}
		case "\"":
			if len(args) == 3 {
				g.tw = args[0].Float64()
				g.tc = args[1].Float64()
				g.tlm = translateM(0, -g.tl).mul(g.tlm)
				g.tm = g.tlm
				showText(args[2].RawString())
			}
		case "'":
			if len(args) == 1 {
				g.tlm = translateM(0, -g.tl).mul(g.tlm)
				g.tm = g.tlm
				showText(args[0].RawString())
			}
		case "Tj":
			if len(args) == 1 {
				showText(args[0].RawString())
			}
		case "TJ":
			if len(args) == 1 {
				v := args[0]
				for i := 0; i < v.Len(); i++ {
					x := v.Index(i)
					if x.Kind() == pdf.String {
						showText(x.RawString())
						continue
					}
					tx := -x.Float64() / 1000 * g.tfs * g.th
					g.tm = translateM(tx, 0).mul(g.tm)
				}
			}
		case "Do":
			if len(args) == 1 {
				places = append(places, imgPlace{
					name:   args[0].Name(),
					topY:   g.ctm[2][1] + absF(g.ctm[1][1]),
					height: absF(g.ctm[1][1]),
				})
			}
		}
	})
	return out, places
}

type imgPlace struct {
	name   string
	topY   float64
	height float64
}

type gstate struct {
	ctm, tm, tlm        matrix3
	tf                  pdf.Font
	metrics             *fontMetrics
	decode              map[int]string
	baseFont            string
	tfs, tc, tw, th, tl float64
	trise               float64
}

func stripSubsetPrefix(name string) string {
	if i := strings.Index(name, "+"); i >= 0 {
		return name[i+1:]
	}
	return name
}

var ligatures = strings.NewReplacer(
	"ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl",
	"ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "ft", "ﬆ", "st",
)

func expandLigatures(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return ligatures.Replace(s)
		}
	}
	return s
}

type matrix3 [3][3]float64

var identM = matrix3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func (a matrix3) mul(b matrix3) matrix3 {
	var r matrix3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return r
}

func translateM(tx, ty float64) matrix3 {
	return matrix3{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
}

func textScale(tfs, th, trise float64) matrix3 {
	return matrix3{{tfs * th, 0, 0}, {0, tfs, 0}, {0, trise, 1}}
}

func matrixFromArgs(args []pdf.Value) matrix3 {
	m := matrix3{}
	for i := 0; i < 6; i++ {
		m[i/2][i%2] = args[i].Float64()
	}
	m[2][2] = 1
	return m
}
