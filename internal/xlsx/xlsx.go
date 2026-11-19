// Package xlsx пишет простую книгу Excel (.xlsx) с одним листом: строка
// заголовков, данные, закреплённая шапка и автофильтр. Без внешних
// зависимостей — формат Office Open XML собирается вручную.
package xlsx

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Cell — значение ячейки: число (IsNumber) или текст.
type Cell struct {
	Text     string
	Number   float64
	IsNumber bool
}

// Text — текстовая ячейка.
func Text(s string) Cell { return Cell{Text: s} }

// Number — числовая ячейка (в Excel по ней работают сортировка и сумма).
func Number(v float64) Cell { return Cell{Number: v, IsNumber: true} }

// Sheet — лист книги.
type Sheet struct {
	Name   string
	Header []string
	Rows   [][]Cell
}

const (
	maxCellChars   = 32767 // предел Excel на текст в ячейке
	maxSheetName   = 31
	minColWidth    = 8
	maxColWidth    = 60
	lineHeightPt   = 15
	maxVisibleRows = 8 // строки с длинными списками не раздуваем больше 8 линий
	styleDefault   = 0
	styleHeader    = 1
	styleWrap      = 2
)

// Write записывает книгу с одним листом.
func Write(w io.Writer, sheet Sheet) error {
	zw := zip.NewWriter(w)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"xl/workbook.xml", workbook(sheet.Name)},
		{"xl/_rels/workbook.xml.rels", workbookRels},
		{"xl/styles.xml", styles},
		{"xl/worksheets/sheet1.xml", worksheet(sheet)},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ColumnName переводит номер столбца (с нуля) в буквы: 0 → A, 26 → AA.
func ColumnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

func worksheet(sheet Sheet) string {
	cols := len(sheet.Header)
	for _, row := range sheet.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	widths := make([]int, cols)
	for i, h := range sheet.Header {
		widths[i] = textWidth(h) + 2 // место под кнопку автофильтра
	}
	for _, row := range sheet.Rows {
		for i, c := range row {
			if w := cellWidth(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0">`)
	if len(sheet.Header) > 0 {
		b.WriteString(`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`)
	}
	b.WriteString(`</sheetView></sheetViews>`)
	b.WriteString(`<sheetFormatPr defaultRowHeight="15"/>`)
	if cols > 0 {
		b.WriteString(`<cols>`)
		for i, w := range widths {
			w = max(minColWidth, min(maxColWidth, w))
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, w)
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	rowNum := 0
	if len(sheet.Header) > 0 {
		rowNum++
		fmt.Fprintf(&b, `<row r="%d">`, rowNum)
		for i, h := range sheet.Header {
			writeCell(&b, i, rowNum, Text(h), styleHeader)
		}
		b.WriteString(`</row>`)
	}
	for _, row := range sheet.Rows {
		rowNum++
		lines := 1
		for _, c := range row {
			if !c.IsNumber {
				lines = max(lines, strings.Count(c.Text, "\n")+1)
			}
		}
		if lines > 1 {
			fmt.Fprintf(&b, `<row r="%d" ht="%d" customHeight="1">`, rowNum, min(lines, maxVisibleRows)*lineHeightPt)
		} else {
			fmt.Fprintf(&b, `<row r="%d">`, rowNum)
		}
		for i, c := range row {
			writeCell(&b, i, rowNum, c, styleWrap)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if len(sheet.Header) > 0 && rowNum > 1 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, ColumnName(len(sheet.Header)-1), rowNum)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func writeCell(b *strings.Builder, col, row int, c Cell, style int) {
	ref := ColumnName(col) + strconv.Itoa(row)
	if c.IsNumber {
		fmt.Fprintf(b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, styleDefault, strconv.FormatFloat(c.Number, 'f', -1, 64))
		return
	}
	if c.Text == "" {
		return
	}
	fmt.Fprintf(b, `<c r="%s" t="inlineStr" s="%d"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(truncate(c.Text)))
}

// textWidth — ширина самой длинной строки текста в символах.
func textWidth(s string) int {
	width := 0
	for _, line := range strings.Split(s, "\n") {
		width = max(width, utf8.RuneCountInString(line))
	}
	return width
}

func cellWidth(c Cell) int {
	if c.IsNumber {
		return len(strconv.FormatFloat(c.Number, 'f', -1, 64)) + 1
	}
	return textWidth(c.Text) + 1
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxCellChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxCellChars-1]) + "…"
}

// escape экранирует XML и выбрасывает управляющие символы, недопустимые в XML
// 1.0 (кроме табуляции и переводов строки): иначе Excel откажется открыть файл.
func escape(s string) string {
	var b strings.Builder
	for _, r := range strings.ReplaceAll(s, "\r\n", "\n") {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r == '\r':
			b.WriteString("\n")
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xD800 && r <= 0xDFFF):
			// недопустимый символ — пропускаем
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sheetName приводит имя листа к правилам Excel: до 31 символа, без []:*?/\.
func sheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "Лист1"
	}
	if runes := []rune(name); len(runes) > maxSheetName {
		name = string(runes[:maxSheetName])
	}
	return name
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
	`</Types>`

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const workbookRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

func workbook(name string) string {
	name = escape(sheetName(name))
	return xmlHeader + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets><sheet name="` + name + `" sheetId="1" r:id="rId1"/></sheets>` +
		`</workbook>`
}

// Стили: 0 — обычный, 1 — шапка (жирный, серая заливка, рамка снизу),
// 2 — текст с переносом по строкам, выравнивание по верху.
const styles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
	`<font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
	`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFE7E6E6"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border>` +
	`<border><left/><right/><top/><bottom style="thin"><color auto="1"/></bottom><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="3">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"><alignment vertical="top"/></xf>` +
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1"><alignment vertical="center"/></xf>` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`
