package pdfnative

import "unicode"

func u16(b []byte, o int) int {
	if o < 0 || o+2 > len(b) {
		return 0
	}
	return int(b[o])<<8 | int(b[o+1])
}

func u32(b []byte, o int) int {
	if o < 0 || o+4 > len(b) {
		return 0
	}
	return int(b[o])<<24 | int(b[o+1])<<16 | int(b[o+2])<<8 | int(b[o+3])
}

func sfntTables(b []byte) map[string][]byte {
	out := map[string][]byte{}
	if len(b) < 12 {
		return out
	}
	if string(b[0:4]) == "ttcf" {
		return out
	}
	n := u16(b, 4)
	for i := 0; i < n; i++ {
		rec := 12 + i*16
		if rec+16 > len(b) {
			break
		}
		off := u32(b, rec+8)
		ln := u32(b, rec+12)
		if off < 12 || ln <= 0 || off > len(b) {
			continue
		}
		end := off + ln
		if end > len(b) {
			end = len(b)
		}
		out[string(b[rec:rec+4])] = b[off:end]
	}
	return out
}

func cmapGIDToUnicode(cmap []byte) map[int]rune {
	out := map[int]rune{}
	n := u16(cmap, 2)
	bestOff, bestScore := -1, -1
	for i := 0; i < n; i++ {
		rec := 4 + i*8
		if rec+8 > len(cmap) {
			break
		}
		plat, enc, off := u16(cmap, rec), u16(cmap, rec+2), u32(cmap, rec+4)
		score := 1
		switch {
		case plat == 3 && enc == 10:
			score = 5
		case plat == 3 && enc == 1:
			score = 4
		case plat == 0:
			score = 3
		case plat == 3 && enc == 0:
			score = 2
		}
		if score > bestScore && off > 0 && off < len(cmap) {
			bestScore, bestOff = score, off
		}
	}
	if bestOff < 0 {
		return out
	}
	sub := cmap[bestOff:]
	put := func(ch, gid int) {
		if gid == 0 || ch < 0 {
			return
		}
		if _, ok := out[gid]; !ok {
			out[gid] = rune(ch)
		}
	}
	switch u16(sub, 0) {
	case 0:
		for ch := 0; ch < 256 && 6+ch < len(sub); ch++ {
			put(ch, int(sub[6+ch]))
		}
	case 6:
		first, cnt := u16(sub, 6), u16(sub, 8)
		for i := 0; i < cnt; i++ {
			put(first+i, u16(sub, 10+i*2))
		}
	case 4:
		seg := u16(sub, 6) / 2
		endO := 14
		startO := endO + seg*2 + 2
		deltaO := startO + seg*2
		rangeO := deltaO + seg*2
		for s := 0; s < seg; s++ {
			end, start := u16(sub, endO+s*2), u16(sub, startO+s*2)
			delta, ro := u16(sub, deltaO+s*2), u16(sub, rangeO+s*2)
			if start > end {
				continue
			}
			for ch := start; ch <= end && ch != 0xFFFF; ch++ {
				gid := 0
				if ro == 0 {
					gid = (ch + delta) & 0xFFFF
				} else if g := u16(sub, rangeO+s*2+ro+(ch-start)*2); g != 0 {
					gid = (g + delta) & 0xFFFF
				}
				put(ch, gid)
			}
		}
	case 12:
		ng := u32(sub, 12)
		for gi := 0; gi < ng; gi++ {
			base := 16 + gi*12
			sc, ec, sg := u32(sub, base), u32(sub, base+4), u32(sub, base+8)
			if sc > ec || ec-sc > 1<<20 {
				continue
			}
			for ch := sc; ch <= ec; ch++ {
				put(ch, sg+(ch-sc))
			}
		}
	}
	return out
}

func coverageGlyphs(cov []byte) []int {
	switch u16(cov, 0) {
	case 1:
		cnt := u16(cov, 2)
		out := make([]int, 0, cnt)
		for i := 0; i < cnt; i++ {
			out = append(out, u16(cov, 4+i*2))
		}
		return out
	case 2:
		rc := u16(cov, 2)
		maxIdx := -1
		type rng struct{ start, end, idx int }
		rs := make([]rng, 0, rc)
		for i := 0; i < rc; i++ {
			b := 4 + i*6
			st, en, ci := u16(cov, b), u16(cov, b+2), u16(cov, b+4)
			if st > en {
				continue
			}
			rs = append(rs, rng{st, en, ci})
			if ci+(en-st) > maxIdx {
				maxIdx = ci + (en - st)
			}
		}
		if maxIdx < 0 || maxIdx > 1<<20 {
			return nil
		}
		out := make([]int, maxIdx+1)
		for _, r := range rs {
			for g := r.start; g <= r.end; g++ {
				if i := r.idx + (g - r.start); i >= 0 && i < len(out) {
					out[i] = g
				}
			}
		}
		return out
	}
	return nil
}

func gsubLigatures(gsub []byte) map[int][]int {
	out := map[int][]int{}
	llOff := u16(gsub, 8)
	if llOff <= 0 || llOff >= len(gsub) {
		return out
	}
	ll := gsub[llOff:]
	for i, lc := 0, u16(ll, 0); i < lc; i++ {
		lo := u16(ll, 2+i*2)
		if lo <= 0 || lo >= len(ll) {
			continue
		}
		lk := ll[lo:]
		lt := u16(lk, 0)
		for s, stc := 0, u16(lk, 4); s < stc; s++ {
			so := u16(lk, 6+s*2)
			if so <= 0 || so >= len(lk) {
				continue
			}
			sub := lk[so:]
			t := lt
			if lt == 7 {
				t = u16(sub, 2)
				eo := u32(sub, 4)
				if eo <= 0 || eo >= len(sub) {
					continue
				}
				sub = sub[eo:]
			}
			if t == 4 {
				parseLigSubst(sub, out)
			}
		}
	}
	return out
}

func parseLigSubst(sub []byte, out map[int][]int) {
	if u16(sub, 0) != 1 {
		return
	}
	covOff := u16(sub, 2)
	if covOff <= 0 || covOff >= len(sub) {
		return
	}
	firsts := coverageGlyphs(sub[covOff:])
	for i, setCount := 0, u16(sub, 4); i < setCount && i < len(firsts); i++ {
		lsOff := u16(sub, 6+i*2)
		if lsOff <= 0 || lsOff >= len(sub) {
			continue
		}
		ls := sub[lsOff:]
		for j, ligCount := 0, u16(ls, 0); j < ligCount; j++ {
			ligOff := u16(ls, 2+j*2)
			if ligOff <= 0 || ligOff >= len(ls) {
				continue
			}
			lig := ls[ligOff:]
			ligGID := u16(lig, 0)
			compCount := u16(lig, 2)
			if compCount < 2 || compCount > 8 {
				continue
			}
			comps := make([]int, 0, compCount)
			comps = append(comps, firsts[i])
			for c := 0; c < compCount-1; c++ {
				comps = append(comps, u16(lig, 4+c*2))
			}
			if _, ok := out[ligGID]; !ok {
				out[ligGID] = comps
			}
		}
	}
}

func embeddedLigatures(prog []byte) map[int]string {
	defer func() { _ = recover() }()
	tabs := sfntTables(prog)
	cmap, hasCmap := tabs["cmap"]
	gsub, hasGSUB := tabs["GSUB"]
	if !hasCmap || !hasGSUB {
		return nil
	}
	g2u := cmapGIDToUnicode(cmap)
	if len(g2u) == 0 {
		return nil
	}
	ligs := gsubLigatures(gsub)
	if len(ligs) == 0 {
		return nil
	}
	out := map[int]string{}
	for ligGID, comps := range ligs {
		text := make([]rune, 0, len(comps))
		ok := true
		for _, g := range comps {
			r, found := g2u[g]
			if !found || !unicode.IsLetter(r) {
				ok = false
				break
			}
			text = append(text, r)
		}
		if ok && len(text) > 0 {
			out[ligGID] = string(text)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
