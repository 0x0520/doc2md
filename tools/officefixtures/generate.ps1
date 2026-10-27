<#
.SYNOPSIS
Generates real-world test documents with Microsoft Office (Windows only).

.DESCRIPTION
Builds one specification in Word and saves it as DOCX, PDF, RTF, ODT, filtered
HTML and TXT, plus a large copy for timing; a workbook in Excel saved as XLSX,
XLS, ODS and CSV; and a deck in PowerPoint saved as PPTX, ODP and PDF. The
files are for comparing doc2md's output across formats by hand. They are
written to an ignored directory and must never be committed.

.EXAMPLE
pwsh tools/officefixtures/generate.ps1
go run ./cmd/doc2md tools/officefixtures/out/spec.pdf
#>
param(
  [string]$Out = (Join-Path $PSScriptRoot 'out'),
  [int]$Copies = 30
)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force $Out | Out-Null
$Out = (Resolve-Path $Out).Path

# Office leaves its COM servers running after Quit; only the ones started here are stopped.
$apps = 'WINWORD', 'EXCEL', 'POWERPNT'
$before = @(Get-Process $apps -ErrorAction SilentlyContinue | ForEach-Object Id)
function Stop-OwnOffice {
  Start-Sleep -Seconds 2
  Get-Process $apps -ErrorAction SilentlyContinue |
    Where-Object { $before -notcontains $_.Id -and $_.MainWindowHandle -eq 0 } |
    Stop-Process -Force -Confirm:$false
}

Add-Type -AssemblyName System.Drawing
$png = Join-Path $Out 'chart.png'
$bmp = New-Object System.Drawing.Bitmap 240, 160
for ($y = 0; $y -lt 160; $y++) {
  for ($x = 0; $x -lt 240; $x++) { $bmp.SetPixel($x, $y, [System.Drawing.Color]::FromArgb($x, $y + 60, 180)) }
}
$bmp.Save($png, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Dispose()

# Built-in style and caption ids, so the script works with any Office UI language.
$Normal = -1; $Heading1 = -2; $Heading2 = -3; $Heading3 = -4; $Title = -63
$ListBullet = -49; $ListNumber = -50; $CaptionFigure = -1; $CaptionTable = -2

# edge.* holds constructs real documents use and simple readers lose: content
# controls, notes, text boxes, tracked changes, outline numbering, nested tables.
function New-WordEdgeCases($word) {
  $doc = $word.Documents.Add()
  $sel = $word.Selection
  function Para($style, $text) {
    $sel.Range.Style = [int]$style
    $sel.TypeText($text)
    $sel.TypeParagraph()
  }

  Para $Heading1 'Content controls'
  $sel.Range.Style = $Normal
  $start = $sel.Range.Start
  $sel.TypeText('Customer: Example Ltd signed on 2026-03-01.')
  $sel.TypeParagraph()
  $null = $doc.ContentControls.Add(1, $doc.Range($start + 10, $start + 21))
  $t = $doc.Tables.Add($sel.Range, 2, 2)
  $t.Borders.Enable = $true
  $t.Cell(1, 1).Range.Text = 'Field'; $t.Cell(1, 2).Range.Text = 'Value'
  $t.Cell(2, 1).Range.Text = 'Order'; $t.Cell(2, 2).Range.Text = 'A-1042'
  $cell = $t.Cell(2, 2).Range
  $null = $doc.ContentControls.Add(1, $doc.Range($cell.Start, $cell.End - 1))
  $sel.EndKey(6) | Out-Null

  Para $Heading1 'Notes'
  $sel.Range.Style = $Normal
  $sel.TypeText('A sentence with a footnote')
  $null = $doc.Footnotes.Add($sel.Range, [Type]::Missing, 'The footnote text.')
  $sel.EndKey(6) | Out-Null
  $sel.TypeText(' and one with an endnote')
  $null = $doc.Endnotes.Add($sel.Range, [Type]::Missing, 'The endnote text.')
  $sel.EndKey(6) | Out-Null
  $sel.TypeText('.')
  $sel.TypeParagraph()

  Para $Heading1 'Text box'
  Para $Normal 'The box next to this paragraph holds a note.'
  $box = $doc.Shapes.AddTextbox(1, 300, 200, 200, 60)
  $box.TextFrame.TextRange.Text = 'Text inside a text box.'

  Para $Heading1 'Tracked changes'
  $doc.TrackRevisions = $true
  $sel.Range.Style = $Normal
  $sel.TypeText('Kept text, ')
  $sel.TypeText('inserted text, ')
  $doc.TrackRevisions = $false
  $sel.TypeText('removed text, ')
  $sel.MoveLeft(1, 14, 1) | Out-Null
  $doc.TrackRevisions = $true
  $sel.Delete() | Out-Null
  $doc.TrackRevisions = $false
  $sel.EndKey(6) | Out-Null
  $sel.TypeText('end.')
  $sel.TypeParagraph()

  Para $Heading1 'Outline numbering'
  $sel.Range.Style = $Normal
  $start = $sel.Range.Start
  $sel.TypeText("First level`rSecond level`rThird level`rFirst level again")
  $r = $doc.Range($start, $sel.Range.End)
  $r.ListFormat.ApplyListTemplate($word.ListGalleries.Item(3).ListTemplates.Item(1), $false)
  $r.Paragraphs.Item(2).Range.ListFormat.ListLevelNumber = 2
  $r.Paragraphs.Item(3).Range.ListFormat.ListLevelNumber = 3
  $sel.EndKey(6) | Out-Null
  $sel.TypeParagraph()
  $sel.Range.ListFormat.RemoveNumbers()

  Para $Heading1 'Nested table'
  $outer = $doc.Tables.Add($sel.Range, 2, 2)
  $outer.Borders.Enable = $true
  $outer.Cell(1, 1).Range.Text = 'Outer A'; $outer.Cell(1, 2).Range.Text = 'Outer B'
  $outer.Cell(2, 1).Range.Text = 'Outer C'
  $inner = $outer.Cell(2, 2).Tables.Add($outer.Cell(2, 2).Range, 2, 2)
  $inner.Cell(1, 1).Range.Text = 'in 1'; $inner.Cell(1, 2).Range.Text = 'in 2'
  $inner.Cell(2, 1).Range.Text = 'in 3'; $inner.Cell(2, 2).Range.Text = 'in 4'
  $sel.EndKey(6) | Out-Null

  Para $Heading1 'Comments and symbols'
  $sel.Range.Style = $Normal
  $sel.TypeText('A commented word')
  $null = $doc.Comments.Add($doc.Range($sel.Range.Start - 4, $sel.Range.Start), 'A reviewer comment.')
  $sel.EndKey(6) | Out-Null
  $sel.TypeText(' and symbols: € © – “quoted” ½ α β ✓.')
  $sel.TypeParagraph()

  $base = Join-Path $Out 'edge'
  $doc.SaveAs2("$base.docx", 16)
  $doc.SaveAs2("$base.odt", 23)
  $doc.SaveAs2("$base.rtf", 6)
  $doc.SaveAs2("$base.html", 10)
  $doc.ExportAsFixedFormat("$base.pdf", 17)
  $doc.Close(0)
}

function New-WordDocuments {
  $word = New-Object -ComObject Word.Application
  $word.Visible = $false
  $word.DisplayAlerts = 0
  try {
    $doc = $word.Documents.Add()
    $sel = $word.Selection
    function Para($style, $text) {
      $sel.Range.Style = [int]$style
      $sel.TypeText($text)
      $sel.TypeParagraph()
    }

    $sec = $doc.Sections.Item(1)
    $sec.Headers.Item(1).Range.Text = 'Fixture Corp - Product Specification'
    $footer = $sec.Footers.Item(1).Range
    $footer.Text = 'Page '
    $footer.Collapse(0)
    $null = $footer.Fields.Add($footer, 33)

    Para $Title 'Product Specification'
    Para $Normal 'Version 2.1, internal draft for review.'
    $sel.InsertBreak(7)

    Para $Normal 'Contents'
    $null = $doc.TablesOfContents.Add($sel.Range, $true, 1, 3)
    $sel.EndKey(6) | Out-Null
    $sel.TypeParagraph()
    $sel.InsertBreak(7)

    Para $Heading1 '1 Introduction'
    Para $Normal 'This document describes the import service. It covers the data flow, the validation rules and the error handling. Readers should know the basic architecture.'
    Para $Heading2 '1.1 Scope'
    Para $Normal 'The service accepts files from three sources and stores them after validation.'
    Para $Heading3 '1.1.1 Out of scope'
    Para $Normal 'Payment processing and reporting are handled elsewhere.'

    Para $Heading1 '2 Requirements'
    Para $ListBullet 'Every upload is checked for size and type.'
    Para $ListBullet 'Rejected files are reported to the sender.'
    Para $ListBullet 'Accepted files are archived for ten years.'
    Para $ListNumber 'Receive the file.'
    Para $ListNumber 'Validate the content.'
    Para $ListNumber 'Store the result.'
    Para $Normal 'The following table lists the status values.'

    $t = $doc.Tables.Add($sel.Range, 4, 3)
    $t.Borders.Enable = $true
    $cells = @(@('Status', 'Meaning', 'Deadline'), @('open', 'received, not checked', '5 days'), @('checked', 'approved', '2 days'), @('closed', 'archived', ''))
    for ($r = 0; $r -lt 4; $r++) { for ($c = 0; $c -lt 3; $c++) { $t.Cell($r + 1, $c + 1).Range.Text = $cells[$r][$c] } }
    $t.Rows.Item(1).HeadingFormat = -1
    $t.Cell(4, 2).Merge($t.Cell(4, 3))
    $sel.EndKey(6) | Out-Null
    $sel.InsertCaption($CaptionTable, ': Status values')
    $sel.TypeParagraph()

    Para $Normal 'A diagram of the flow:'
    $null = $sel.InlineShapes.AddPicture($png)
    $sel.TypeParagraph()
    $sel.InsertCaption($CaptionFigure, ': Data flow')
    $sel.TypeParagraph()

    Para $Heading1 '3 Glossary'
    $sel.Range.Style = $Normal
    $sel.TypeText('A term in ')
    $sel.Font.Bold = 1; $sel.TypeText('bold'); $sel.Font.Bold = 0
    $sel.TypeText(', one in ')
    $sel.Font.Italic = 1; $sel.TypeText('italic'); $sel.Font.Italic = 0
    $sel.TypeText(' and a ')
    $null = $doc.Hyperlinks.Add($sel.Range, 'https://example.org/spec', '', '', 'link')
    $sel.EndKey(6) | Out-Null
    $sel.TypeText('.')
    $sel.TypeParagraph()

    # A continuous break after the columns makes Word balance them, so the text
    # really runs in two columns instead of filling the left one.
    $sel.InsertBreak(2)
    Para $Heading1 '4 Two-column section'
    $sel.InsertBreak(3)
    $doc.Sections.Item($doc.Sections.Count).PageSetup.TextColumns.SetCount(2)
    for ($i = 1; $i -le 6; $i++) {
      Para $Normal "Paragraph $i of the two-column section. It is long enough to wrap over several lines in a narrow column, so the reading order across columns is tested properly."
    }
    $sel.InsertBreak(3)
    $doc.Sections.Item($doc.Sections.Count).PageSetup.TextColumns.SetCount(1)

    Para $Heading1 '5 Long table'
    $rows = 60
    $lt = $doc.Tables.Add($sel.Range, $rows + 1, 3)
    $lt.Borders.Enable = $true
    $lt.Cell(1, 1).Range.Text = 'ID'; $lt.Cell(1, 2).Range.Text = 'Name'; $lt.Cell(1, 3).Range.Text = 'Amount'
    $lt.Rows.Item(1).HeadingFormat = -1
    for ($r = 2; $r -le $rows + 1; $r++) {
      $lt.Cell($r, 1).Range.Text = "R$($r - 1)"
      $lt.Cell($r, 2).Range.Text = "Item number $($r - 1)"
      $lt.Cell($r, 3).Range.Text = '{0:N2}' -f (($r - 1) * 12.5)
    }
    $sel.EndKey(6) | Out-Null
    Para $Normal 'End of document.'
    $doc.TablesOfContents.Item(1).Update()

    $base = Join-Path $Out 'spec'
    $doc.SaveAs2("$base.docx", 16)
    $doc.SaveAs2("$base.rtf", 6)
    $doc.SaveAs2("$base.odt", 23)
    $doc.SaveAs2("$base.html", 10)
    $doc.SaveAs2("$base.txt", 2)
    $doc.ExportAsFixedFormat("$base.pdf", 17)
    $doc.Close(0)

    $big = $word.Documents.Add()
    for ($i = 0; $i -lt $Copies; $i++) {
      $r = $big.Content
      $r.Collapse(0)
      $r.InsertFile("$base.docx")
    }
    $big.SaveAs2((Join-Path $Out 'big.docx'), 16)
    $big.ExportAsFixedFormat((Join-Path $Out 'big.pdf'), 17)
    $big.Close(0)

    New-WordEdgeCases $word
  } finally {
    $word.Quit()
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($word)
  }
}

# PowerShell passes its UI locale to COM, and a German Excel then stores English
# format codes such as "yyyy-mm-dd" as literal text. Setting the property with an
# explicit en-US locale keeps the codes portable.
function Set-NumberFormat($range, [string]$format) {
  [void][System.__ComObject].InvokeMember('NumberFormat', [System.Reflection.BindingFlags]::SetProperty,
    $null, $range, @($format), [Globalization.CultureInfo]'en-US')
}

function New-ExcelWorkbooks {
  $xl = New-Object -ComObject Excel.Application
  $xl.Visible = $false
  $xl.DisplayAlerts = $false
  try {
    $wb = $xl.Workbooks.Add()
    while ($wb.Worksheets.Count -lt 3) { $null = $wb.Worksheets.Add([Type]::Missing, $wb.Worksheets.Item($wb.Worksheets.Count)) }
    $s1 = $wb.Worksheets.Item(1); $s1.Name = 'Orders'
    $s1.Range('A1').Value2 = 'Quarterly orders'
    $null = $s1.Range('A1:D1').Merge()
    $hdr = @('Date', 'Customer', 'Quantity', 'Total')
    for ($c = 0; $c -lt 4; $c++) { $s1.Cells.Item(3, $c + 1).Value2 = $hdr[$c] }
    for ($r = 0; $r -lt 8; $r++) {
      $s1.Cells.Item(4 + $r, 1).Formula = "=DATE(2026,1,$($r * 3 + 1))"
      $s1.Cells.Item(4 + $r, 2).Value2 = "Customer $([char](65 + $r))"
      $s1.Cells.Item(4 + $r, 3).Value2 = [double](($r + 1) * 4)
      $s1.Cells.Item(4 + $r, 4).Formula = "=C$(4 + $r)*19.99"
    }
    Set-NumberFormat $s1.Range('A4:A11') 'yyyy-mm-dd'
    Set-NumberFormat $s1.Range('D4:D12') '#,##0.00'
    $s1.Cells.Item(12, 3).Value2 = 'Sum'
    $s1.Cells.Item(12, 4).Formula = '=SUM(D4:D11)'
    $s1.Cells.Item(5, 2).Value2 = 'Pipe | inside'
    $s1.Cells.Item(6, 2).Value2 = "Line one`nline two"
    $null = $s1.Shapes.AddPicture($png, 0, -1, 300, 20, 120, 80)

    $wb.Worksheets.Item(2).Name = 'Empty'
    $s3 = $wb.Worksheets.Item(3); $s3.Name = 'Sparse'
    $s3.Range('C5').Value2 = 'far away'
    $s3.Range('A1').Value2 = 'origin'
    $s3.Range('E2').Value2 = [double]0.125
    Set-NumberFormat $s3.Range('E2') '0.0%'

    $wb.SaveAs((Join-Path $Out 'book.xlsx'), 51)
    $wb.SaveAs((Join-Path $Out 'book.xls'), 56)
    $wb.SaveAs((Join-Path $Out 'book.ods'), 60)
    $wb.Worksheets.Item(1).Activate()
    $wb.SaveAs((Join-Path $Out 'book.csv'), 6)
    $wb.Close($false)
  } finally {
    $xl.Quit()
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($xl)
  }
}

function New-PowerPointDecks {
  $pp = New-Object -ComObject PowerPoint.Application
  try {
    $pres = $pp.Presentations.Add(0)
    $s = $pres.Slides.Add(1, 1)
    $s.Shapes.Item(1).TextFrame.TextRange.Text = 'Release Review'
    $s.Shapes.Item(2).TextFrame.TextRange.Text = 'Status of the import service'
    $s = $pres.Slides.Add(2, 2)
    $s.Shapes.Item(1).TextFrame.TextRange.Text = 'Highlights'
    $tr = $s.Shapes.Item(2).TextFrame.TextRange
    $tr.Text = "Faster uploads`rFewer errors`rNew validation rules`rSecond level point"
    $tr.Paragraphs(4).IndentLevel = 2
    $s.NotesPage.Shapes.Placeholders.Item(2).TextFrame.TextRange.Text = 'Speaker note: mention the error rate.'
    $s = $pres.Slides.Add(3, 11)
    $s.Shapes.Item(1).TextFrame.TextRange.Text = 'Numbers'
    $tbl = $s.Shapes.AddTable(3, 3, 50, 120, 600, 150).Table
    $vals = @(@('Metric', 'Before', 'After'), @('Errors', '12', '3'), @('Time', '40 s', '9 s'))
    for ($r = 0; $r -lt 3; $r++) { for ($c = 0; $c -lt 3; $c++) { $tbl.Cell($r + 1, $c + 1).Shape.TextFrame.TextRange.Text = $vals[$r][$c] } }
    $s = $pres.Slides.Add(4, 11)
    $s.Shapes.Item(1).TextFrame.TextRange.Text = 'Diagram'
    $null = $s.Shapes.AddPicture($png, 0, -1, 100, 120, 240, 160)
    $tb = $s.Shapes.AddTextbox(1, 400, 150, 250, 80)
    $tb.TextFrame.TextRange.Text = 'A free text box next to the picture.'
    $pres.SaveAs((Join-Path $Out 'deck.pptx'), 24)
    $pres.SaveAs((Join-Path $Out 'deck.odp'), 35)
    $pres.SaveAs((Join-Path $Out 'deck.pdf'), 32)
    $pres.Close()
  } finally {
    $pp.Quit()
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($pp)
  }
}

try {
  New-WordDocuments
  New-ExcelWorkbooks
  New-PowerPointDecks
} finally {
  Stop-OwnOffice
}
Get-ChildItem $Out -File | Select-Object Name, Length
