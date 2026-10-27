# doc2md

[![CI](https://github.com/0x0520/doc2md/actions/workflows/ci.yml/badge.svg)](https://github.com/0x0520/doc2md/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/0x0520/doc2md.svg)](https://pkg.go.dev/github.com/0x0520/doc2md)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Pure-Go conversion of documents to clean, structured Markdown. No external
process, no JRE, no Python, no cgo: bytes in, Markdown out.

doc2md is built for feeding documents to LLMs, search indexes and diff tools,
where headings, tables and reading order matter more than visual fidelity.

## Formats

| Format | Package | What is kept |
|---|---|---|
| `.pdf` | `internal/pdfnative` | reading order across columns, headings from size, weight and numbering (slide titles of exported decks included), bullet and numbered lists, tables (including wrapped and cross-page cells), text in form XObjects, raster figures as `[Image: N]` markers; cover page, table of contents and running headers/footers are dropped; password-protected files return `ErrEncrypted` |
| `.docx` `.docm` | `internal/ooxml` | title and headings from styles (language-independent), font size as a fallback for documents without heading styles, bullet and numbered lists (also through list styles), bold/italic/strikethrough, tables, links, field results such as caption numbers, pictures; the table of contents is dropped |
| `.pptx` `.pptm` | `internal/ooxml` | slides in presentation order, titles as headings, body placeholders as nested bullet lists, text boxes, tables, pictures |
| `.xlsx` `.xlsm` | `internal/sheets` | one heading and table per sheet with formatted cell values, pictures after their sheet |
| `.xls` | `internal/sheets` | legacy BIFF8 (Excel 97–2003) with formula results, dates and percentages, and SpreadsheetML 2003 |
| `.csv` | `internal/sheets` | delimiter detection (`,` `;` tab `\|`), UTF-8, UTF-16 and Windows-1252 input |
| `.odt` `.ods` `.odp` | `internal/odf` | OpenDocument text, spreadsheets and presentations, including templates (`.ott` `.ots` `.otp`): title and headings, bullet and numbered lists, tables, pictures; table of contents and speaker notes dropped |
| `.epub` | `internal/epub` | chapters in spine order, nested as the navigation document says (EPUB 3 `nav`, EPUB 2 `toc.ncx`) |
| `.rtf` | `internal/rtf` | title and headings from styles and outline levels, tables, bullet and numbered lists, document and font code pages, Unicode escapes, PNG/JPEG pictures; the table of contents is dropped |
| `.html` `.htm` `.xhtml` | `internal/htmldoc` | headings, tables, lists (including Word's HTML lists), links, emphasis, inline `data:` pictures, any declared charset; scripts, styling, navigation and Word's TOC dropped |
| `.xml` | `internal/xmldoc` | element tree as headings and labelled values; runs of sibling records as tables |
| `.adoc` `.asciidoc` | `internal/asciidoc` | sections, tables (`cols=`, spans, `a\|` cells), lists, cross-references, attributes |
| `.md` `.txt` | | returned unchanged |

A file whose extension is unknown or missing is identified by its leading bytes,
so `.aspx`, `.config` or extensionless uploads still convert. A known extension
always wins over content sniffing.

## Install

```sh
go install github.com/0x0520/doc2md/cmd/doc2md@latest
```

## Command line

```sh
doc2md report.pdf > report.md
doc2md -o report.md -images ./figs report.pdf
doc2md -verbatim report.pdf > report-full.md
cat report.docx | doc2md - > report.md
cat spec.adoc | doc2md -name spec.adoc - > spec.md
```

`-verbatim` keeps everything a PDF would otherwise drop (cover page, table of
contents, running headers and footers, repeated decorations). Formats without a
byte signature (AsciiDoc, CSV) need `-name` when piped.

## Library

```go
import "github.com/0x0520/doc2md"

md, images, err := doc2md.Convert("report.docx", data)
switch {
case errors.Is(err, doc2md.ErrUnsupportedFormat):
	// route to a fallback
case errors.Is(err, doc2md.ErrEncrypted):
	// the PDF needs a password
}
for _, img := range images {
	// img.Seq matches the "[Image: N]" marker; img.Page, img.Ext, img.Bytes
}
```

| Function | Purpose |
|---|---|
| `Convert(name, data)` | any supported format, chosen by extension or content |
| `ConvertVerbatim(name, data)` | same, without dropping PDF front matter and page furniture |
| `Handles(name)`, `IsPlainText(name)`, `Extensions()` | what the module can read |
| `Render(pdf)`, `RenderWithPages(pdf)`, `RenderVerbatim(pdf)`, `Extract(pdf)` | PDF-specific entry points, with page breaks where needed |

Conversion runs entirely in memory and touches no files. pdfcpu's config
directory is disabled, so it works in containers without a writable `$HOME`.

doc2md makes every decision that needs the source geometry or structure
(headings and their depth, tables, captions, chapter numbering). It deliberately
does not do locale- or consumer-specific cleanup; layer that on top.

## HTTP service

```sh
doc2md serve -addr 127.0.0.1:8090
```

Serves a small web UI at `/` for converting documents in the browser, plus a JSON API:

| Endpoint | |
|---|---|
| `POST /api/convert` | multipart field `file`, optional `verbatim=1` → `{name, format, bytes, verbatim, markdown, images[], ms}`; images carry `seq`, `page`, `ext`, `dataBase64` |
| `GET /api/config` | `maxBytes` and the supported extensions |
| `GET /api/health` | liveness |

An unsupported format answers **415** with a hint on what to do (for `.doc`:
save as `.docx`). A document above the limit (`-max-mib`, default 32) answers
**413**. `-api-only` serves the API without the UI. The service binds to
localhost by default and has no authentication; put it behind a reverse proxy
before exposing it.

The service is also available as a Go package: `service.Handler(service.Options{})`.

## Limits

Not supported, reported as `ErrUnsupportedFormat`:

| Format | Reason |
|---|---|
| `.doc`, `.ppt` | legacy OLE binary formats, no pure-Go reader |
| `.msg`, `.eml` | mail containers; convert their attachments instead |
| `.pages`, `.numbers`, `.key` | undocumented Apple formats |

What does not carry over:

- Only raster figures. Vector drawings (WMF/EMF) are not rasterised; SVG is passed through.
- No OCR. Scanned PDFs without a text layer produce no text.
- HTML pictures only when embedded as `data:` URIs.
- Legacy `.xls` files yield no pictures; Excel 95 (BIFF5) and older are not read.
- Word footnotes, headers and footers are not read. Speaker notes are left out of presentations.
- `.xlsx` and `.xls` cells are formatted with English separators (`2,878.56`) and
  ISO 8601 dates, whatever the author's locale. OpenDocument spreadsheets keep the
  text the authoring application displayed (`2.878,56` from a German Excel).
- Spreadsheet pictures are listed after their sheet, not inside cells.
- Logos and icons repeated across most PDF pages are treated as decoration.
- AsciiDoc `include::` becomes a link; no other file is read.
- Nested blocks in AsciiDoc `a|` cells keep their text, not their structure.
- Spreadsheet cells contribute their displayed value, not the formula.
- Styling, positions and drawings in OpenDocument are dropped.
- EPUBs without a hierarchical navigation document yield flat chapters.
- Font size is only a fallback for `.docx` without heading styles. RTF and
  OpenDocument headings marked only by size come out as paragraphs. PDF relies
  on size, weight and position entirely.

## Development

```sh
go test ./...
go test ./internal/pdfnative/ -update-golden
go test -run '^$' -fuzz FuzzConvert -fuzztime 5m .
go test -run '^$' -bench Render ./internal/pdfnative/
```

`FuzzConvert` feeds arbitrary bytes to every reader and fails on a panic or on
any input that takes longer than two seconds.

PDF tests come in two layers. Line-level tests build the internal line model
directly. End-to-end golden tests describe a page layout in Go, render it to a
real PDF with pdfcpu at test time and compare the Markdown with
`internal/pdfnative/testdata/*.md`. The repository holds no binary fixtures.
Review golden diffs as part of every change.

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache License 2.0](LICENSE)
