package officeeditor

import (
	"fmt"
	nativedialog "filesearch-admin/internal/nativeDialog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xuri/excelize/v2"
)

const (
	MaxExcelRows    = 1000
	MaxExcelColumns = 100
	MaxExcelUnzip   = 512 * 1024 * 1024
	MaxExcelXML     = 32 * 1024 * 1024
)

type ExcelDocument struct {
	mu       sync.Mutex
	path     string
	workbook *excelize.File
	snapshot fileSnapshot
}

func OpenExcel(path string) (*ExcelDocument, error) {
	if !strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return nil, fmt.Errorf("встроенный табличный редактор поддерживает только XLSX")
	}
	snapshot, err := captureSnapshot(path)
	if err != nil {
		return nil, err
	}
	workbook, err := excelize.OpenFile(path, excelize.Options{
		UnzipSizeLimit:    MaxExcelUnzip,
		UnzipXMLSizeLimit: MaxExcelXML,
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть XLSX: %w", err)
	}
	if len(workbook.GetSheetList()) == 0 {
		_ = workbook.Close()
		return nil, fmt.Errorf("книга не содержит доступных листов")
	}
	return &ExcelDocument{path: path, workbook: workbook, snapshot: snapshot}, nil
}

func (document *ExcelDocument) Sheets() []string {
	document.mu.Lock()
	defer document.mu.Unlock()
	return append([]string(nil), document.workbook.GetSheetList()...)
}

func (document *ExcelDocument) Dimensions(sheet string) (rows, columns int, truncated bool, err error) {
	document.mu.Lock()
	defer document.mu.Unlock()
	dimension, err := document.workbook.GetSheetDimension(sheet)
	if err != nil {
		return 0, 0, false, err
	}
	parts := strings.Split(dimension, ":")
	lastCell := parts[len(parts)-1]
	columns, rows, err = excelize.CellNameToCoordinates(lastCell)
	if err != nil || rows < 1 || columns < 1 {
		rows, columns, err = 1, 1, nil
	}
	if rows > MaxExcelRows {
		rows = MaxExcelRows
		truncated = true
	}
	if columns > MaxExcelColumns {
		columns = MaxExcelColumns
		truncated = true
	}
	return rows, columns, truncated, nil
}

func (document *ExcelDocument) Cell(sheet string, row, column int) (string, error) {
	document.mu.Lock()
	defer document.mu.Unlock()
	cell, err := excelize.CoordinatesToCellName(column, row)
	if err != nil {
		return "", err
	}
	formula, err := document.workbook.GetCellFormula(sheet, cell)
	if err != nil {
		return "", err
	}
	if formula != "" {
		return "=" + formula, nil
	}
	return document.workbook.GetCellValue(sheet, cell)
}

func (document *ExcelDocument) SetCell(sheet string, row, column int, value string) error {
	document.mu.Lock()
	defer document.mu.Unlock()
	cell, err := excelize.CoordinatesToCellName(column, row)
	if err != nil {
		return err
	}
	if strings.HasPrefix(value, "=") {
		formula := strings.TrimPrefix(value, "=")
		if strings.TrimSpace(formula) == "" {
			return fmt.Errorf("формула после знака = не указана")
		}
		return document.workbook.SetCellFormula(sheet, cell, formula)
	}
	return document.workbook.SetCellValue(sheet, cell, value)
}

func (document *ExcelDocument) Save() error {
	document.mu.Lock()
	defer document.mu.Unlock()
	if err := document.snapshot.verify(document.path); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(document.path), ".1cproc-xlsx-*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный XLSX: %w", err)
	}
	tempPath := tempFile.Name()
	completed := false
	defer func() {
		_ = tempFile.Close()
		if !completed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := document.workbook.Write(tempFile); err != nil {
		return fmt.Errorf("не удалось сформировать XLSX: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("не удалось записать временный XLSX: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("не удалось закрыть временный XLSX: %w", err)
	}
	if err := nativedialog.ReplaceFileWithoutBackup(document.path, tempPath); err != nil {
		return err
	}
	completed = true
	snapshot, err := captureSnapshot(document.path)
	if err != nil {
		return err
	}
	document.snapshot = snapshot
	return nil
}

func (document *ExcelDocument) Close() error {
	document.mu.Lock()
	defer document.mu.Unlock()
	if document.workbook == nil {
		return nil
	}
	err := document.workbook.Close()
	document.workbook = nil
	return err
}
