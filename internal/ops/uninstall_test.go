package ops

import (
	constandstruct "filesearch-admin/internal/constAndStruct"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestValidateInstalledProgramRegistryPath(t *testing.T) {
	valid := []string{
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Example`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\{12345678-1234-1234-1234-123456789ABC}`,
	}
	for _, path := range valid {
		if err := validateInstalledProgramRegistryPath(path); err != nil {
			t.Fatalf("корректный путь %q отклонён: %v", path, err)
		}
	}

	invalid := []string{
		`HKEY_USERS\S-1-5-21-1-2-3-1001\Software\Microsoft\Windows\CurrentVersion\Uninstall\Example`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Run\Example`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Example\Child`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Example'; Remove-Item C:\ -Recurse; '`,
	}
	for _, path := range invalid {
		if err := validateInstalledProgramRegistryPath(path); err == nil {
			t.Fatalf("опасный путь %q принят", path)
		}
	}
}

func TestBuildUninstallScriptReadsProtectedRegistryValue(t *testing.T) {
	program := constandstruct.InstalledProgram{
		Name:         "O'Brien Tools",
		RegistryPath: `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Example`,
	}
	script := buildUninstallScript(program)
	for _, required := range []string{
		`$expectedName = 'O''Brien Tools'`,
		"QuietUninstallString",
		"WindowsInstaller",
		"Start-Process",
		"UNINSTALL_OK|",
		"$actualName -cne $expectedName",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("сценарий удаления должен содержать %q", required)
		}
	}
	if strings.Contains(script, "$item.UninstallString") {
		t.Fatal("интерактивная UninstallString не должна использоваться")
	}
}

func TestBuildUninstallScriptParsesInWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверка предназначена для Windows PowerShell")
	}
	script := buildUninstallScript(constandstruct.InstalledProgram{
		Name:         "Example",
		RegistryPath: `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Example`,
	})
	command := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		`$source=[Console]::In.ReadToEnd(); $tokens=$null; $errors=$null; [Management.Automation.Language.Parser]::ParseInput($source,[ref]$tokens,[ref]$errors) | Out-Null; if ($errors.Count -gt 0) { $errors | ForEach-Object { $_.Message }; exit 1 }`,
	)
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell-скрипт содержит синтаксическую ошибку: %v\n%s", err, output)
	}
}

func TestParseUninstallResult(t *testing.T) {
	if err := parseUninstallResult([]string{"COMMAND_LINE|UNINSTALL_OK|0", "COMMAND_OK"}); err != nil {
		t.Fatalf("успешный ответ отклонён: %v", err)
	}
	if err := parseUninstallResult([]string{"COMMAND_OK"}); err == nil {
		t.Fatal("ответ без UNINSTALL_OK принят")
	}
	if err := parseUninstallResult([]string{"ERROR|Access denied"}); err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("неожиданная удалённая ошибка: %v", err)
	}
}
