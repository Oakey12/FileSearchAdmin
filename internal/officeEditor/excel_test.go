package officeeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestExcelDocumentEditsOriginalWithoutBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.xlsx")
	workbook := excelize.NewFile()
	if err := workbook.SetCellValue("Sheet1", "A1", "old"); err != nil {
		t.Fatal(err)
	}
	if err := workbook.SetCellFormula("Sheet1", "B1", "1+1"); err != nil {
		t.Fatal(err)
	}
	if err := workbook.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	_ = workbook.Close()

	document, err := OpenExcel(path)
	if err != nil {
		t.Fatal(err)
	}
	defer document.Close()
	if value, err := document.Cell("Sheet1", 1, 2); err != nil || value != "=1+1" {
		t.Fatalf("formula cell = %q, %v", value, err)
	}
	if err := document.SetCell("Sheet1", 1, 1, "new"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetCell("Sheet1", 2, 1, "=SUM(2,3)"); err != nil {
		t.Fatal(err)
	}
	if err := document.Save(); err != nil {
		t.Fatal(err)
	}

	reopened, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if value, _ := reopened.GetCellValue("Sheet1", "A1"); value != "new" {
		t.Fatalf("A1 = %q", value)
	}
	if formula, _ := reopened.GetCellFormula("Sheet1", "A2"); formula != "SUM(2,3)" {
		t.Fatalf("A2 formula = %q", formula)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "report.xlsx" {
			t.Fatalf("unexpected persistent file %q", entry.Name())
		}
	}
}

func TestExcelDocumentRejectsExternallyChangedOriginal(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.xlsx")
	workbook := excelize.NewFile()
	if err := workbook.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	_ = workbook.Close()
	document, err := OpenExcel(path)
	if err != nil {
		t.Fatal(err)
	}
	defer document.Close()
	if err := document.SetCell("Sheet1", 1, 1, "local edit"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("external")
	_ = file.Close()
	_ = os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second))
	if err := document.Save(); err == nil || !strings.Contains(err.Error(), "изменён") {
		t.Fatalf("Save() error = %v", err)
	}
}
