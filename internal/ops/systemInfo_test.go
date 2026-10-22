package ops

import (
	"encoding/base64"
	"strings"
	"testing"
)

func encodeSystemInfoLine(prefix, payload string) string {
	return prefix + base64.StdEncoding.EncodeToString([]byte(payload))
}

func TestParseSystemInfoReadsSummaryAndWarnings(t *testing.T) {
	lines := []string{
		"COMMAND_LINE|STAGE|Выполнение",
		encodeSystemInfoLine("COMMAND_LINE|INFO_WARNING|", `{"Stage":"Сведения о дисках","Message":"отказано в доступе"}`),
		encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"SRV-1","Domain":"corp.local","PartOfDomain":true,`+
			`"OSCaption":"Windows Server 2019","ProcessorCores":8,"MemoryTotalGB":"31.8","IPAddresses":["192.168.20.31"]}`),
		"COMMAND_OK",
	}

	info, warnings, err := parseSystemInfo(lines)
	if err != nil {
		t.Fatalf("parseSystemInfo() вернул ошибку: %v", err)
	}
	if info.ComputerName != "SRV-1" || info.Domain != "corp.local" || !info.PartOfDomain {
		t.Fatalf("сводка разобрана неверно: %#v", info)
	}
	if info.ProcessorCores != 8 || info.MemoryTotalGB != "31.8" {
		t.Fatalf("числовые поля разобраны неверно: %#v", info)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "отказано в доступе") {
		t.Fatalf("предупреждения разобраны неверно: %#v", warnings)
	}
}

func TestParseSystemInfoRequiresSummary(t *testing.T) {
	if _, _, err := parseSystemInfo([]string{"COMMAND_OK"}); err == nil {
		t.Fatal("ответ без сводки должен считаться ошибкой")
	}
}

func TestParseSystemInfoRejectsDuplicateSummary(t *testing.T) {
	line := encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"SRV-1"}`)
	if _, _, err := parseSystemInfo([]string{line, line}); err == nil {
		t.Fatal("повторная сводка должна отклоняться")
	}
}

func TestFormatSystemInfoSkipsEmptyFields(t *testing.T) {
	line := encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"SRV-1","OSCaption":"Windows Server 2019",`+
		`"ProcessorName":"Xeon","ProcessorCores":8,"ProcessorLogical":16,"MemoryTotalGB":"32","MemoryFreeGB":"8"}`)
	info, _, err := parseSystemInfo([]string{line})
	if err != nil {
		t.Fatalf("parseSystemInfo() вернул ошибку: %v", err)
	}

	rows := FormatSystemInfo(info)
	found := make(map[string]string, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row[1]) == "" {
			t.Fatalf("поле %q попало в таблицу пустым", row[0])
		}
		found[row[0]] = row[1]
	}
	if found["Имя компьютера"] != "SRV-1" {
		t.Fatalf("имя компьютера = %q", found["Имя компьютера"])
	}
	if !strings.Contains(found["Процессор"], "ядер: 8") || !strings.Contains(found["Процессор"], "потоков: 16") {
		t.Fatalf("процессор отформатирован неверно: %q", found["Процессор"])
	}
	if found["Оперативная память"] != "32 ГБ (свободно 8 ГБ)" {
		t.Fatalf("память отформатирована неверно: %q", found["Оперативная память"])
	}
	if _, exists := found["Серийный номер"]; exists {
		t.Fatal("пустой серийный номер не должен показываться")
	}
}

func TestFormatSystemInfoMarksWorkgroup(t *testing.T) {
	line := encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"PC-1","Domain":"WORKGROUP","PartOfDomain":false}`)
	info, _, err := parseSystemInfo([]string{line})
	if err != nil {
		t.Fatalf("parseSystemInfo() вернул ошибку: %v", err)
	}
	for _, row := range FormatSystemInfo(info) {
		if row[0] == "Домен" {
			if !strings.Contains(row[1], "рабочая группа") {
				t.Fatalf("машина вне домена должна помечаться: %q", row[1])
			}
			return
		}
	}
	t.Fatal("строка с доменом отсутствует")
}

func TestFormatSystemInfoListsUserFolders(t *testing.T) {
	line := encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"TERM25",`+
		`"UserFolders":["1108 — 7.5 ГБ, изменена 30.09.2026 17:27","admind — 4 ГБ, изменена 14.08.2026 18:40"]}`)
	info, _, err := parseSystemInfo([]string{line})
	if err != nil {
		t.Fatalf("parseSystemInfo() вернул ошибку: %v", err)
	}
	if len(info.UserFolders) != 2 {
		t.Fatalf("папок разобрано %d, ожидалось 2", len(info.UserFolders))
	}

	var folders string
	for _, row := range FormatSystemInfo(info) {
		if strings.HasPrefix(row[0], "Папки") {
			folders = row[1]
		}
	}
	if folders == "" {
		t.Fatal("строка с папками профилей отсутствует")
	}
	// Каждая папка со своей строки: на терминальном сервере их десятки.
	if !strings.Contains(folders, "\n") {
		t.Fatalf("папки должны разделяться переводом строки: %q", folders)
	}
	if !strings.Contains(folders, "1108") || !strings.Contains(folders, "admind") {
		t.Fatalf("в списке потерялись папки: %q", folders)
	}
}

func TestFormatSystemInfoHidesFoldersWhenEmpty(t *testing.T) {
	line := encodeSystemInfoLine("COMMAND_LINE|SYSTEM_INFO|", `{"ComputerName":"TERM25"}`)
	info, _, err := parseSystemInfo([]string{line})
	if err != nil {
		t.Fatalf("parseSystemInfo() вернул ошибку: %v", err)
	}
	for _, row := range FormatSystemInfo(info) {
		if strings.HasPrefix(row[0], "Папки") {
			t.Fatal("пустой список папок не должен показываться")
		}
	}
}
