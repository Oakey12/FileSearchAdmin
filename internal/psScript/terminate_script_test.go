package psscript

import (
	"strings"
	"testing"
)

// Тест (-DryRun) проходит тот же конвейер, что и бой, и возвращается раньше
// любого завершающего вызова: taskkill /F и Win32_Process.Terminate.
func TestTerminateDryRunStopsBeforeKilling(t *testing.T) {
	start := strings.Index(PsScript, "function Invoke-TerminateAction")
	if start < 0 {
		t.Fatal("нет функции Invoke-TerminateAction")
	}
	body := PsScript[start:]
	dryRun := strings.Index(body, "if ($IsDryRun) {")
	owners := strings.Index(body, "Resolve-Owners $ServerName $targets $owners")
	dryReturn := strings.Index(body[dryRun:], "\n        return\n")
	taskkill := strings.Index(body, "Invoke-TaskkillStep $CimSession $targets")
	terminate := strings.Index(body, "Invoke-CimTerminate $process")
	if dryRun < 0 || owners < 0 || dryReturn < 0 || taskkill < 0 || terminate < 0 {
		t.Fatal("не найдены ключевые шаги конвейера завершения")
	}
	if owners > dryRun {
		t.Fatal("владельцы должны определяться до ветки теста — общий путь для теста и боя")
	}
	if dryRun+dryReturn > taskkill || taskkill > terminate {
		t.Fatal("ветка теста должна завершаться до taskkill /F и Terminate, а taskkill идти раньше Terminate")
	}
	dryBlock := body[dryRun : dryRun+dryReturn]
	for _, forbidden := range []string{"/F /FI", "Invoke-CimTerminate", "Invoke-TaskkillStep"} {
		if strings.Contains(dryBlock, forbidden) {
			t.Fatalf("ветка теста не должна содержать %q", forbidden)
		}
	}
}

// Ключевые свойства алгоритма: ожидание taskkill, резервный Terminate,
// опознание по PID + времени запуска, поиск других вариантов 1С.
func TestTerminatePipelineProperties(t *testing.T) {
	for _, required := range []string{
		`ValidateSet("find","findfast","terminate","notify","diagnose")`,
		"[switch]$DryRun",
		"$TaskkillTimeoutMs = 20000",
		"function Invoke-RemoteCommandAndWait",
		"-MethodName Terminate",
		"function Get-ProcessKey",
		"Name LIKE '1cv%'",
		"KILL_PENDING|",
	} {
		if !strings.Contains(PsScript, required) {
			t.Fatalf("сценарий завершения должен содержать %q", required)
		}
	}
	for _, removed := range []string{"Invoke-FilteredTaskkill", `"killids"`, "-OperationTimeoutSec 5\n"} {
		if strings.Contains(PsScript, removed) {
			t.Fatalf("устаревшее %q должно быть удалено", removed)
		}
	}
}
