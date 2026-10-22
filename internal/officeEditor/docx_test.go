package officeeditor

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDOCXDocumentEditsOriginalWithoutBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "document.docx")
	createTestDOCX(t, path, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Первый</w:t></w:r></w:p><w:p><w:r><w:t>Второй &amp; текст</w:t></w:r></w:p></w:body></w:document>`)

	document, err := OpenDOCX(path)
	if err != nil {
		t.Fatal(err)
	}
	if paragraphs := document.Paragraphs(); len(paragraphs) != 2 || paragraphs[1] != "Второй & текст" {
		t.Fatalf("paragraphs = %#v", paragraphs)
	}
	if err := document.SetParagraph(0, "Новый <текст> & данные"); err != nil {
		t.Fatal(err)
	}
	if err := document.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDOCX(path)
	if err != nil {
		t.Fatal(err)
	}
	paragraphs := reopened.Paragraphs()
	if paragraphs[0] != "Новый <текст> & данные" || paragraphs[1] != "Второй & текст" {
		t.Fatalf("saved paragraphs = %#v", paragraphs)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "document.docx" {
			t.Fatalf("unexpected persistent file %q", entry.Name())
		}
	}
}

func TestApplyDOCXParagraphEditsPreservesUneditedParts(t *testing.T) {
	source := []byte(`<w:document><w:body><w:p><w:r><w:t>A</w:t></w:r></w:p><w:tbl/><w:p><w:r><w:t>B</w:t></w:r><w:r><w:t>C</w:t></w:r></w:p></w:body></w:document>`)
	result, err := applyDOCXParagraphEdits(source, map[int]string{1: "D&E"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(result)
	if !strings.Contains(text, `<w:t>A</w:t>`) || !strings.Contains(text, `<w:tbl/>`) || !strings.Contains(text, `<w:t>D&amp;E</w:t>`) {
		t.Fatalf("result = %s", text)
	}
	if strings.Contains(text, `<w:t>C</w:t>`) {
		t.Fatalf("second run text was not cleared: %s", text)
	}
}

func createTestDOCX(t *testing.T, path, documentXML string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	contentTypes, err := archive.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = contentTypes.Write([]byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`))
	document, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = document.Write([]byte(documentXML))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
