package ops

import (
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
)

func TestSystemInfoSheetOneRowPerComputer(t *testing.T) {
	sheet := SystemInfoSheet([]constandstruct.SystemInfoResult{
		{Name: "TS1", IP: "10.0.0.1", Info: constandstruct.SystemInfo{
			ComputerName: "TS1", Domain: "corp.local", PartOfDomain: true,
			ProcessorCores: 8, MemoryTotalGB: "31,9", Disks: []string{"C: 100", "D: 200"},
		}},
		{Name: "10.0.0.2", IP: "10.0.0.2", Error: "RPC-сервер недоступен"},
	})

	col := map[string]int{}
	for i, h := range sheet.Header {
		col[h] = i
	}
	if len(sheet.Rows) != 2 {
		t.Fatalf("строк %d, ожидали 2", len(sheet.Rows))
	}
	ok, failed := sheet.Rows[0], sheet.Rows[1]
	for _, row := range sheet.Rows {
		if len(row) != len(sheet.Header) {
			t.Fatalf("в строке %d ячеек, в шапке %d", len(row), len(sheet.Header))
		}
	}
	if ok[col["Статус"]].Text != "OK" || ok[col["В домене"]].Text != "да" {
		t.Fatalf("неверный статус/домен: %+v", ok)
	}
	if c := ok[col["Ядер"]]; !c.IsNumber || c.Number != 8 {
		t.Fatalf("ядра должны быть числом: %+v", c)
	}
	if c := ok[col["ОЗУ всего, ГБ"]]; !c.IsNumber || c.Number != 31.9 {
		t.Fatalf("память с запятой должна стать числом: %+v", c)
	}
	if got := ok[col["Диски"]].Text; got != "C: 100\nD: 200" {
		t.Fatalf("диски: %q", got)
	}
	if failed[col["Сервер"]].Text != "" || failed[col["IP"]].Text != "10.0.0.2" ||
		failed[col["Статус"]].Text != "Ошибка" || failed[col["Ошибка"]].Text != "RPC-сервер недоступен" {
		t.Fatalf("строка с ошибкой: %+v", failed)
	}
}
