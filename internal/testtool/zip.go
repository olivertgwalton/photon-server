package testtool

import (
	"archive/zip"
	"os"
	"slices"
	"testing"
)

// Zip writes a zip archive to path holding files, each a name and its content, written in name
// order: an EPUB or a comic archive as a reader finds one.
func Zip(tb testing.TB, path string, files map[string]string) {
	tb.Helper()
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	w := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		e, err := w.Create(name)
		if err == nil {
			_, err = e.Write([]byte(files[name]))
		}
		if err != nil {
			tb.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
}

// EPUB writes an EPUB 3 to path whose package document holds metadata, the elements of its
// metadata element, with a cover the package names.
func EPUB(tb testing.TB, path, metadata string, cover []byte) {
	tb.Helper()
	Zip(tb, path, map[string]string{
		"mimetype": "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
			<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OEBPS/content.opf": `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
			<metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">` + metadata + `</metadata>
			<manifest><item id="cover" href="images/cover.png" media-type="image/png" properties="cover-image"/>
				<item id="c1" href="text/c1.xhtml" media-type="application/xhtml+xml"/></manifest>
			<spine><itemref idref="c1"/></spine></package>`,
		"OEBPS/images/cover.png": string(cover),
		"OEBPS/text/c1.xhtml":    `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>It was a dark and stormy night.</p></body></html>`,
	})
}
