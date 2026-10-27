package mdrender

import "testing"

func TestInlineRender(t *testing.T) {
	bold := Emphasis{Bold: true}
	link := &Link{URL: "https://example.org/"}
	var b Inline
	b.Add("A ", Emphasis{}, nil)
	b.Add("bold", bold, nil)
	b.Add(" ", Emphasis{}, nil)
	b.Add("span", bold, nil)
	b.Add(", ", Emphasis{}, nil)
	b.Add("a ", Emphasis{Italic: true}, link)
	b.Add("link", Emphasis{Italic: true}, link)
	b.Add(" and ", Emphasis{}, nil)
	b.Add("gone", Emphasis{Bold: true, Italic: true, Strike: true}, nil)
	if got, want := b.Render(true), "A **bold span**, [_a link_](https://example.org/) and **_~~gone~~_**"; got != want {
		t.Errorf("Render(true) = %q, want %q", got, want)
	}
	if got, want := b.Render(false), "A bold span, [a link](https://example.org/) and gone"; got != want {
		t.Errorf("Render(false) = %q, want %q", got, want)
	}
	b.Reset()
	if !b.Empty() {
		t.Error("Reset left text behind")
	}
}

func TestJoinKeepsRawPiecesVerbatim(t *testing.T) {
	got := Join([]Piece{
		{Text: "see ", Em: Emphasis{Bold: true}},
		{Text: "[Image: 1]", Raw: true},
		{Text: " here", Em: Emphasis{Bold: true}},
	}, true)
	if want := "**see** [Image: 1] **here**"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestActiveScheme(t *testing.T) {
	for href, want := range map[string]bool{
		"javascript:alert(1)":  true,
		" JavaScript:alert(1)": true,
		"java\tscript:x":       true,
		"VBScript:x":           true,
		"data:text/html,x":     true,
		"https://example.org/": false,
		"mailto:a@example.org": false,
		"#anchor":              false,
	} {
		if got := ActiveScheme(href); got != want {
			t.Errorf("ActiveScheme(%q) = %v, want %v", href, got, want)
		}
	}
}
