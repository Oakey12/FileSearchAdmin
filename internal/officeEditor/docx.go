package officeeditor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	nativedialog "filesearch-admin/internal/nativeDialog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	MaxDOCXDocumentXMLBytes  = 16 * 1024 * 1024
	MaxDOCXUncompressedBytes = 512 * 1024 * 1024
)

var (
	docxParagraphPattern = regexp.MustCompile(`(?s)<w:p(?:\s[^>]*)?>.*?</w:p>`)
	docxTextPattern      = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
)

type DOCXDocument struct {
	mu          sync.Mutex
	path        string
	documentXML []byte
	paragraphs  []string
	edits       map[int]string
	snapshot    fileSnapshot
}

func OpenDOCX(path string) (*DOCXDocument, error) {
	if !strings.EqualFold(filepath.Ext(path), ".docx") {
		return nil, fmt.Errorf("встроенный редактор документов поддерживает только DOCX")
	}
	snapshot, err := captureSnapshot(path)
	if err != nil {
		return nil, err
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть DOCX: %w", err)
	}
	defer archive.Close()
	var documentXML []byte
	var totalUncompressed uint64
	for _, file := range archive.File {
		if file.UncompressedSize64 > MaxDOCXUncompressedBytes-totalUncompressed {
			return nil, fmt.Errorf("распакованный DOCX слишком большой для встроенного редактора")
		}
		totalUncompressed += file.UncompressedSize64
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > MaxDOCXDocumentXMLBytes {
			return nil, fmt.Errorf("структура DOCX слишком большая для встроенного редактора")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		documentXML, err = io.ReadAll(io.LimitReader(reader, MaxDOCXDocumentXMLBytes+1))
		_ = reader.Close()
		if err != nil {
			return nil, err
		}
		break
	}
	if len(documentXML) == 0 {
		return nil, fmt.Errorf("DOCX не содержит word/document.xml")
	}
	if len(documentXML) > MaxDOCXDocumentXMLBytes {
		return nil, fmt.Errorf("структура DOCX слишком большая для встроенного редактора")
	}
	paragraphs := extractDOCXParagraphs(documentXML)
	if len(paragraphs) == 0 {
		return nil, fmt.Errorf("в DOCX не найдены редактируемые текстовые абзацы")
	}
	return &DOCXDocument{
		path: path, documentXML: documentXML, paragraphs: paragraphs,
		edits: make(map[int]string), snapshot: snapshot,
	}, nil
}

func extractDOCXParagraphs(documentXML []byte) []string {
	blocks := docxParagraphPattern.FindAll(documentXML, -1)
	paragraphs := make([]string, 0, len(blocks))
	for _, block := range blocks {
		textNodes := docxTextPattern.FindAllSubmatch(block, -1)
		if len(textNodes) == 0 {
			continue
		}
		var text strings.Builder
		for _, node := range textNodes {
			text.WriteString(html.UnescapeString(string(node[1])))
		}
		paragraphs = append(paragraphs, text.String())
	}
	return paragraphs
}

func (document *DOCXDocument) Paragraphs() []string {
	document.mu.Lock()
	defer document.mu.Unlock()
	result := append([]string(nil), document.paragraphs...)
	for index, value := range document.edits {
		result[index] = value
	}
	return result
}

func (document *DOCXDocument) SetParagraph(index int, value string) error {
	document.mu.Lock()
	defer document.mu.Unlock()
	if index < 0 || index >= len(document.paragraphs) {
		return fmt.Errorf("некорректный номер абзаца")
	}
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	document.edits[index] = value
	return nil
}

func (document *DOCXDocument) Save() error {
	document.mu.Lock()
	defer document.mu.Unlock()
	if len(document.edits) == 0 {
		return nil
	}
	if err := document.snapshot.verify(document.path); err != nil {
		return err
	}
	updatedXML, err := applyDOCXParagraphEdits(document.documentXML, document.edits)
	if err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(document.path), ".1cproc-docx-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный DOCX: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := rewriteDOCX(document.path, tempPath, updatedXML); err != nil {
		return err
	}
	if err := nativedialog.ReplaceFileWithoutBackup(document.path, tempPath); err != nil {
		return err
	}
	completed = true
	document.documentXML = updatedXML
	for index, value := range document.edits {
		document.paragraphs[index] = value
	}
	clear(document.edits)
	snapshot, err := captureSnapshot(document.path)
	if err != nil {
		return err
	}
	document.snapshot = snapshot
	return nil
}

func (document *DOCXDocument) Close() error { return nil }

func applyDOCXParagraphEdits(documentXML []byte, edits map[int]string) ([]byte, error) {
	blocks := docxParagraphPattern.FindAllIndex(documentXML, -1)
	var result bytes.Buffer
	lastOffset := 0
	editableIndex := 0
	for _, blockRange := range blocks {
		block := documentXML[blockRange[0]:blockRange[1]]
		if len(docxTextPattern.FindAllIndex(block, -1)) == 0 {
			continue
		}
		result.Write(documentXML[lastOffset:blockRange[0]])
		if value, changed := edits[editableIndex]; changed {
			result.Write(replaceDOCXParagraphText(block, value))
		} else {
			result.Write(block)
		}
		lastOffset = blockRange[1]
		editableIndex++
	}
	result.Write(documentXML[lastOffset:])
	if editableIndex == 0 {
		return nil, fmt.Errorf("DOCX не содержит редактируемых абзацев")
	}
	return result.Bytes(), nil
}

func replaceDOCXParagraphText(block []byte, value string) []byte {
	nodes := docxTextPattern.FindAllSubmatchIndex(block, -1)
	if len(nodes) == 0 {
		return block
	}
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	var result bytes.Buffer
	lastOffset := 0
	for index, node := range nodes {
		contentStart, contentEnd := node[2], node[3]
		result.Write(block[lastOffset:contentStart])
		if index == 0 {
			result.Write(escaped.Bytes())
		}
		lastOffset = contentEnd
	}
	result.Write(block[lastOffset:])
	return result.Bytes()
}

func rewriteDOCX(sourcePath, destinationPath string, updatedXML []byte) error {
	source, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	zipWriter := zip.NewWriter(destination)
	for _, file := range source.File {
		header := file.FileHeader
		writer, err := zipWriter.CreateHeader(&header)
		if err != nil {
			_ = zipWriter.Close()
			_ = destination.Close()
			return err
		}
		if file.Name == "word/document.xml" {
			if _, err = writer.Write(updatedXML); err != nil {
				_ = zipWriter.Close()
				_ = destination.Close()
				return err
			}
			continue
		}
		reader, err := file.Open()
		if err != nil {
			_ = zipWriter.Close()
			_ = destination.Close()
			return err
		}
		_, copyErr := io.Copy(writer, reader)
		_ = reader.Close()
		if copyErr != nil {
			_ = zipWriter.Close()
			_ = destination.Close()
			return copyErr
		}
	}
	if err := zipWriter.Close(); err != nil {
		_ = destination.Close()
		return err
	}
	if err := destination.Sync(); err != nil {
		_ = destination.Close()
		return err
	}
	return destination.Close()
}
