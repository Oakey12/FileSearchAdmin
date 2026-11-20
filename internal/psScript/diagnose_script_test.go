package psscript

import (
	"strings"
	"testing"
)

// Тест подключения запускается до обычного подключения и ничего не завершает
// и не рассылает: единственный запуск — безвредный "cmd.exe /c exit 0".
func TestDiagnoseActionIsHarmless(t *testing.T) {
	dispatch := strings.Index(PsScript, `if ($Action -eq "diagnose")`)
	connect := strings.Index(PsScript, "$connectionTimer = [Diagnostics.Stopwatch]::StartNew()")
	if dispatch < 0 || connect < 0 || dispatch > connect {
		t.Fatal("тест подключения должен идти до обычного подключения CIM")
	}
	start := strings.Index(PsScript, "function Invoke-DiagnoseAction")
	end := strings.Index(PsScript[start:], "\ntry {")
	if start < 0 || end < 0 {
		t.Fatal("нет функции Invoke-DiagnoseAction")
	}
	body := PsScript[start : start+end]
	for _, forbidden := range []string{"taskkill", "-MethodName Terminate", "msg.exe", "Invoke-TerminateAction"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("тест подключения не должен содержать %q", forbidden)
		}
	}
	for _, required := range []string{`"cmd.exe /c exit 0"`, "New-CimSessionOption -PacketPrivacy", "135", "Write-Diag"} {
		if !strings.Contains(body, required) {
			t.Fatalf("тест подключения должен содержать %q", required)
		}
	}
}
