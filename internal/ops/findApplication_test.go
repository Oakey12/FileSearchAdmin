package ops

import (
	"encoding/base64"
	constandstruct "filesearch-admin/internal/constAndStruct"
	psscript "filesearch-admin/internal/psScript"
	"strings"
	"testing"
)

func appLine(prefix, payload string) string {
	return prefix + base64.StdEncoding.EncodeToString([]byte(payload))
}

func TestParseApplicationMatchesReadsAllSources(t *testing.T) {
	lines := []string{
		appLine("COMMAND_LINE|APP_MATCH|", `{"Source":"реестр","Name":"TrueConf Client","Version":"8.5","Path":"C:\\Program Files\\TrueConf"}`),
		appLine("COMMAND_LINE|APP_MATCH|", `{"Source":"запущен","Name":"TrueConf.exe","Path":"C:\\Program Files\\TrueConf\\TrueConf.exe"}`),
		appLine("COMMAND_LINE|APP_WARNING|", `{"Stage":"Ветки пользователей","Message":"отказано в доступе"}`),
		"COMMAND_LINE|APP_DONE|2",
		"COMMAND_OK",
	}

	found, warnings, err := parseApplicationMatches(lines)
	if err != nil {
		t.Fatalf("parseApplicationMatches() вернул ошибку: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("найдено записей %d, ожидалось 2: %#v", len(found), found)
	}
	if found[0].Source != "реестр" || found[0].Name != "TrueConf Client" || found[0].Version != "8.5" {
		t.Fatalf("первая запись разобрана неверно: %#v", found[0])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "отказано в доступе") {
		t.Fatalf("предупреждения разобраны неверно: %#v", warnings)
	}
}

func TestParseApplicationMatchesRequiresCompletion(t *testing.T) {
	line := appLine("COMMAND_LINE|APP_MATCH|", `{"Source":"файл","Name":"app.exe"}`)
	if _, _, err := parseApplicationMatches([]string{line}); err == nil {
		t.Fatal("ответ без APP_DONE должен считаться незавершённым")
	}
}

func TestParseApplicationMatchesAllowsEmptyResult(t *testing.T) {
	found, _, err := parseApplicationMatches([]string{"COMMAND_LINE|APP_DONE|0", "COMMAND_OK"})
	if err != nil {
		t.Fatalf("пустой результат — не ошибка, получено: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("ожидался пустой список, получено %#v", found)
	}
}

func TestFindApplicationScriptEncodesQuerySafely(t *testing.T) {
	// Кавычки в запросе не должны попадать в текст сценария: иначе имя
	// программы могло бы завершить строку и дописать свою команду.
	script := psscript.FindApplicationScript(`Tru"eConf'; Remove-Item C:\ -Recurse`)
	if strings.Contains(script, "Remove-Item C:") {
		t.Fatal("запрос подставлен в сценарий как есть — возможна подстановка команд")
	}
	if strings.Contains(script, "__QUERY_B64__") {
		t.Fatal("заполнитель запроса не заменён")
	}
}

func TestDescribeApplicationMatchSkipsEmptyParts(t *testing.T) {
	text := DescribeApplicationMatch(constandstruct.ApplicationMatch{
		Source: "запущен", Name: "TrueConf.exe", Path: `C:\TrueConf.exe`,
	})
	if strings.Contains(text, "  ") || strings.Contains(text, "()") {
		t.Fatalf("пустая версия оставила пропуск в описании: %q", text)
	}
	if !strings.Contains(text, "запущен") || !strings.Contains(text, "TrueConf.exe") {
		t.Fatalf("описание неполное: %q", text)
	}
}

func TestSummarizeApplicationOwnersKeepsOrderAndDeduplicates(t *testing.T) {
	matches := []constandstruct.ApplicationMatch{
		{Owner: "Весь компьютер", Name: "Python Launcher"},
		{Owner: `SANTEAM\1108`, Name: "Python 3.13"},
		{Owner: "профиль 1114", Name: "python.exe"},
		{Owner: "профиль 1114", Name: "python3.exe"},
		{Owner: `SANTEAM\1108`, Name: "python.exe"},
	}
	owners := SummarizeApplicationOwners(matches)
	want := []string{"Весь компьютер", `SANTEAM\1108`, "профиль 1114"}
	if len(owners) != len(want) {
		t.Fatalf("владельцев %d, ожидалось %d: %#v", len(owners), len(want), owners)
	}
	for i, owner := range want {
		if owners[i] != owner {
			t.Fatalf("владелец %d = %q, ожидался %q", i, owners[i], owner)
		}
	}
}

func TestSummarizeApplicationOwnersNamesUnknownOwner(t *testing.T) {
	owners := SummarizeApplicationOwners([]constandstruct.ApplicationMatch{{Name: "app.exe"}})
	if len(owners) != 1 || !strings.Contains(owners[0], "не определён") {
		t.Fatalf("пустой владелец должен называться явно: %#v", owners)
	}
}
