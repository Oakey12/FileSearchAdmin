package remotefunc

import (
	"slices"
	"strings"
	"testing"
)

// Флаг теста и выбранные PID доходят до сценария; пароль в аргументы не попадает.
func TestRemoteScriptArgsForTerminate(t *testing.T) {
	dry := remoteScriptArgs(`C:\t\s.ps1`, "10.0.0.1", `DOMAIN\admin`, remoteRequest{
		action: "terminate", processNames: "1cv7s.exe", processIDs: "10,20", dryRun: true,
	})
	for _, want := range []string{"-DryRun", "-ProcessIds", "10,20", "terminate", "-CredentialFromStdin"} {
		if !slices.Contains(dry, want) {
			t.Fatalf("в аргументах нет %q: %v", want, dry)
		}
	}

	real := remoteScriptArgs(`C:\t\s.ps1`, "10.0.0.1", `DOMAIN\admin`, remoteRequest{
		action: "terminate", processNames: "1cv7s.exe",
	})
	if slices.Contains(real, "-DryRun") || slices.Contains(real, "-ProcessIds") {
		t.Fatalf("боевой вызов по всем процессам не должен нести -DryRun/-ProcessIds: %v", real)
	}
	if strings.Contains(strings.Join(real, " "), "пароль") {
		t.Fatal("пароль не должен попадать в аргументы")
	}
}
