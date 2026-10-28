// Command doc2md converts a document to Markdown. The format comes from the file
// extension, or from the document's bytes when reading stdin.
//
// Usage:
//
//	doc2md [flags] <file>      convert a file
//	doc2md [flags] -           read the document from stdin
//	doc2md serve [flags]       run the HTTP service and web UI
//	doc2md -version            print the version
//
// Flags:
//
//	-o <file>       write Markdown to <file> (default: stdout)
//	-images <dir>   extract figures into <dir> as image-<seq>.<ext>
//	-name <file>    original filename for piped input (AsciiDoc, CSV)
//	-verbatim       keep everything a PDF conversion normally drops
//
// Example:
//
//	doc2md -o report.md -images ./figs report.pdf
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/nullwerke/doc2md"
)

func main() {
	// "serve" is a subcommand, checked before flag parsing so it has its own flags.
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if err := serveCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "doc2md serve:", err)
			os.Exit(1)
		}
		return
	}

	out := flag.String("o", "", "write Markdown to this file (default: stdout)")
	imagesDir := flag.String("images", "", "extract the document's figures into this directory")
	name := flag.String("name", "", "original filename for piped input, so formats without a byte signature (AsciiDoc, CSV) still dispatch")
	verbatim := flag.Bool("verbatim", false, "transcribe the document whole, leaving out nothing (PDF: keeps cover, contents, headers/footers, decorations)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("doc2md", version())
		return
	}

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *out, *imagesDir, *name, *verbatim); err != nil {
		fmt.Fprintln(os.Stderr, "doc2md:", err)
		os.Exit(1)
	}
}

// run converts input ("-" for stdin) and writes the Markdown and, if imagesDir is
// set, the figures. given overrides the filename used for format dispatch.
func run(input, outPath, imagesDir, given string, verbatim bool) error {
	data, err := readInput(input)
	if err != nil {
		return err
	}

	// A pipe has no name; an empty one makes Convert identify the format from the bytes.
	name := given
	if name == "" && input != "-" {
		name = filepath.Base(input)
	}
	convert := doc2md.Convert
	if verbatim {
		convert = doc2md.ConvertVerbatim
	}
	md, images, err := convert(name, data)
	if err != nil {
		return err
	}

	if err := writeImages(imagesDir, images); err != nil {
		return err
	}
	return writeMarkdown(outPath, md)
}

func readInput(input string) ([]byte, error) {
	if input == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(input)
}

func writeMarkdown(outPath, md string) error {
	if outPath == "" {
		_, err := io.WriteString(os.Stdout, md)
		return err
	}
	return os.WriteFile(outPath, []byte(md), 0o644)
}

// writeImages writes each figure as image-<seq>.<ext>; the Seq matches the
// "[Image: N]" marker in the Markdown.
func writeImages(dir string, images []doc2md.Image) error {
	if dir == "" || len(images) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create images dir: %w", err)
	}
	for _, img := range images {
		name := fmt.Sprintf("image-%d.%s", img.Seq, img.Ext)
		if err := os.WriteFile(filepath.Join(dir, name), img.Bytes, 0o644); err != nil {
			return fmt.Errorf("write figure %d: %w", img.Seq, err)
		}
	}
	fmt.Fprintf(os.Stderr, "doc2md: wrote %d figure(s) to %s\n", len(images), dir)
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `doc2md converts documents to Markdown.

usage:
  doc2md [flags] <file>         convert a file (format from its extension)
  doc2md [flags] -              read a document from stdin (format from its bytes, or -name)
  doc2md serve [flags]          run the converter as an HTTP service; see "doc2md serve -h"
  doc2md -version               print the version

flags:
  -o <file>        write Markdown to <file> (default: stdout)
  -images <dir>    extract figures into <dir> as image-<seq>.<ext>
  -name <file>     original filename for piped input (needed for AsciiDoc and CSV)
  -verbatim        keep everything. By default a PDF drops its cover page, table of
                   contents, running headers and footers and per-page decorations.

supported: .pdf .docx .docm .pptx .pptm .xlsx .xlsm .xls .csv .odt .ods .odp
           .epub .rtf .html .htm .xhtml .xml .adoc .asciidoc .md .txt
`)
}

// version returns the module version from the build info, or "(devel)" for a local build.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
