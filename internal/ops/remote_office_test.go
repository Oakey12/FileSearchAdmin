package ops

import (
	"context"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanEditExternalDocument(t *testing.T) {
	for _, path := range []string{"report.xlsx", "document.DOCX", "slides.pptx", "form.pdf", "data.ods"} {
		if !CanEditExternalDocument(path) {
			t.Fatalf("CanEditExternalDocument(%q) = false", path)
		}
	}
	for _, path := range []string{"program.exe", "shortcut.lnk", "archive.zip", "script.ps1"} {
		if CanEditExternalDocument(path) {
			t.Fatalf("CanEditExternalDocument(%q) = true", path)
		}
	}
}

func TestCanEditInternalDocument(t *testing.T) {
	for _, path := range []string{"report.xlsx", "document.DOCX"} {
		if !CanEditInternalDocument(path) {
			t.Fatalf("CanEditInternalDocument(%q) = false", path)
		}
	}
	for _, path := range []string{"legacy.xls", "legacy.doc", "slides.pptx", "script.ps1"} {
		if CanEditInternalDocument(path) {
			t.Fatalf("CanEditInternalDocument(%q) = true", path)
		}
	}
}

func TestPrepareRemoteDocumentBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.xlsx")
	content := []byte("test office content")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file := constandstruct.RemoteFile{
		Name:             "report.xlsx",
		FullPath:         path,
		Size:             info.Size(),
		LastWriteTimeUTC: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
	backupPath, err := prepareRemoteDocumentBackup(context.Background(), path, file)
	if err != nil {
		t.Fatalf("prepareRemoteDocumentBackup() error = %v", err)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(content) {
		t.Fatalf("backup = %q", backup)
	}
}

func TestPrepareRemoteDocumentBackupRejectsChangedFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.xlsx")
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	file := constandstruct.RemoteFile{
		Name:             "report.xlsx",
		FullPath:         path,
		Size:             1,
		LastWriteTimeUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := prepareRemoteDocumentBackup(context.Background(), path, file); err == nil {
		t.Fatal("ожидалась ошибка устаревшего снимка файла")
	}
}
