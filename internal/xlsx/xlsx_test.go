package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestColumnName(t *testing.T) {
	for index, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"} {
		if got := ColumnName(index); got != want {
			t.Fatalf("ColumnName(%d) = %q, ожидали %q", index, got, want)
		}
	}
}

func readZip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("книга — не zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(body)
	}
	return files
}

// Книга содержит все обязательные части, каждая — корректный XML; текст
// экранирован, недопустимые символы выброшены, числа — числовыми ячейками.
func TestWriteProducesWellFormedWorkbook(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Sheet{
		Name:   "Сведения: о/системе",
		Header: []string{"Имя", "Ядер", "Диски"},
		Rows: [][]Cell{
			{Text(`A&B <"x">` + "\x01"), Number(8), Text("C: 100 ГБ\nD: 200 ГБ")},
			{Text("Только имя")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := readZip(t, buf.Bytes())
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		body, ok := files[name]
		if !ok {
			t.Fatalf("нет части %s", name)
		}
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s — некорректный XML: %v", name, err)
			}
		}
	}

	sheet := files["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`A&amp;B &lt;&quot;x&quot;&gt;</t>`,
		`<c r="B2" s="0"><v>8</v></c>`,
		"C: 100 ГБ\nD: 200 ГБ",
		`<row r="2" ht="30" customHeight="1">`,
		`state="frozen"`,
		`<autoFilter ref="A1:C3"/>`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("в листе нет %q:\n%s", want, sheet)
		}
	}
	if strings.Contains(sheet, "\x01") {
		t.Fatal("управляющий символ попал в XML")
	}
	if !strings.Contains(files["xl/workbook.xml"], `name="Сведения_ о_системе"`) {
		t.Fatalf("имя листа не очищено: %s", files["xl/workbook.xml"])
	}
}

func TestLongTextIsTruncatedToExcelLimit(t *testing.T) {
	long := strings.Repeat("я", maxCellChars+10)
	if got := []rune(truncate(long)); len(got) != maxCellChars {
		t.Fatalf("длина %d, ожидали %d", len(got), maxCellChars)
	}
}
