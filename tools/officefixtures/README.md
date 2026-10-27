# Office fixtures

`generate.ps1` drives Microsoft Word, Excel and PowerPoint (Windows, any UI
language) to produce the same content in every format doc2md reads:

| File | Saved as |
|---|---|
| `spec.*` | DOCX, PDF, RTF, ODT, filtered HTML, TXT: title, TOC, numbered headings, bullet and numbered lists, a merged-cell table with caption, a picture, bold/italic/link, a balanced two-column section, a 60-row table across pages |
| `big.*` | DOCX and PDF with 30 copies of `spec` (about 210 pages), for timing |
| `book.*` | XLSX, XLS, ODS, CSV: dates, formulas, number formats, merged title, picture, empty and sparse sheets |
| `deck.*` | PPTX, ODP, PDF: title slide, nested bullets, speaker note, table, picture, text box |

```powershell
pwsh tools/officefixtures/generate.ps1
go run ./cmd/doc2md tools/officefixtures/out/spec.docx
```

Converting the `spec.*` files should give the same Markdown apart from what a
format cannot carry (pictures in HTML, styling in PDF). The output folder is
ignored by git: these are application-generated binaries, and tests build their
inputs in code instead.
