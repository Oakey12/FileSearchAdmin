package ops

import (
	"context"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"strings"
	"testing"
)

func TestExecutePowerShellScriptOnTargetRejectsInvalidIP(t *testing.T) {
	var logs []string
	started := ExecutePowerShellScriptOnTarget(
		context.Background(),
		"not-an-ip",
		`DOMAIN\user`,
		"пароль",
		"Get-Date",
		"поле ввода",
		func(line string) { logs = append(logs, line) },
		nil,
	)
	if started {
		t.Fatal("выполнение запущено для некорректного IP-адреса")
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "корректный IP-адрес") {
		t.Fatalf("неожиданный журнал: %#v", logs)
	}
}

func TestLogPowerShellResult(t *testing.T) {
	var logs []string
	logPowerShellResult(
		constandstruct.Server{Name: "Сервер 1"},
		[]string{
			"TIMING|Выполнение PowerShell|125",
			`SESSION_EXECUTION|7|DOMAIN\user`,
			"COMMAND_LINE|результат|с разделителем",
			"COMMAND_OK",
		},
		func(line string) { logs = append(logs, line) },
		func(line string) { logs = append(logs, line) },
	)
	joined := strings.Join(logs, "\n")
	for _, want := range []string{"125 мс", "сессии DOMAIN\\user (ID 7)", "результат|с разделителем", "выполнена успешно"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("журнал %q не содержит %q", joined, want)
		}
	}
}

func TestLogPowerShellResultReportsControlledError(t *testing.T) {
	var logs []string
	logPowerShellResult(
		constandstruct.Server{Name: "Сервер 1"},
		[]string{"ERROR|Access denied"},
		func(line string) { logs = append(logs, line) },
		func(line string) { logs = append(logs, line) },
	)
	if len(logs) != 1 || !strings.Contains(logs[0], "Access denied") {
		t.Fatalf("неожиданный журнал: %#v", logs)
	}
}

func TestLogPowerShellResultSeparatesCommandOutput(t *testing.T) {
	var events []string
	var output []string
	logPowerShellResult(
		constandstruct.Server{Name: "192.168.1.10"},
		[]string{"COMMAND_LINE|секрет", "COMMAND_OUTPUT_TRUNCATED", "COMMAND_OK"},
		func(line string) { events = append(events, line) },
		func(line string) { output = append(output, line) },
	)
	if strings.Contains(strings.Join(events, "\n"), "секрет") {
		t.Fatalf("вывод команды попал в служебный журнал: %#v", events)
	}
	if len(output) != 1 || !strings.Contains(output[0], "секрет") {
		t.Fatalf("вывод не попал в экранный журнал: %#v", output)
	}
	if !strings.Contains(strings.Join(events, "\n"), "ограничен") {
		t.Fatalf("нет предупреждения об ограничении вывода: %#v", events)
	}
}
