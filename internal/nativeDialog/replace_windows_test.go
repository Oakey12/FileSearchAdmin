//go:build windows

package nativedialog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileWithoutBackup(t *testing.T) {
	directory := t.TempDir()
	originalPath := filepath.Join(directory, "original.docx")
	replacementPath := filepath.Join(directory, "replacement.tmp")
	if err := os.WriteFile(originalPath, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacementPath, []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFileWithoutBackup(originalPath, replacementPath); err != nil {
		t.Fatalf("ReplaceFileWithoutBackup() error = %v", err)
	}
	content, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new content" {
		t.Fatalf("original content = %q", content)
	}
	if _, err := os.Stat(replacementPath); !os.IsNotExist(err) {
		t.Fatalf("replacement still exists: %v", err)
	}
}
