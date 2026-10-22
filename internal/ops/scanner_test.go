package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"reflect"
	"strings"
	"testing"
)

func TestParseInstalledPrograms(t *testing.T) {
	visualStudio := encodedProgramLine(t, constandstruct.InstalledProgram{
		Name: "Visual Studio Code", Version: "1.99", Publisher: "Microsoft", Architecture: "x64",
		RegistryPath:       `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\VSCode`,
		CanSilentUninstall: true, UninstallDescription: "QuietUninstallString",
	})
	sevenZip := encodedProgramLine(t, constandstruct.InstalledProgram{
		Name: "7-Zip", Version: "24.09", Publisher: "Igor Pavlov", Architecture: "x64",
		RegistryPath:       `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\7-Zip`,
		CanSilentUninstall: true, UninstallDescription: "MSI",
	})

	programs, warnings, err := parseInstalledPrograms([]string{
		"TIMING|Выполнение PowerShell|100",
		visualStudio,
		sevenZip,
		visualStudio,
		"COMMAND_LINE|SCAN_SUMMARY|2|0",
		"COMMAND_OK",
	})
	if err != nil {
		t.Fatalf("parseInstalledPrograms() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	want := []constandstruct.InstalledProgram{
		{
			Name: "7-Zip", Version: "24.09", Publisher: "Igor Pavlov", Architecture: "x64",
			RegistryPath:       `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\7-Zip`,
			CanSilentUninstall: true, UninstallDescription: "MSI",
		},
		{
			Name: "Visual Studio Code", Version: "1.99", Publisher: "Microsoft", Architecture: "x64",
			RegistryPath:       `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\VSCode`,
			CanSilentUninstall: true, UninstallDescription: "QuietUninstallString",
		},
	}
	if !reflect.DeepEqual(programs, want) {
		t.Fatalf("parseInstalledPrograms() = %#v, want %#v", programs, want)
	}
}

func TestParseInstalledProgramsReportsRemoteError(t *testing.T) {
	_, _, err := parseInstalledPrograms([]string{"ERROR|Access denied"})
	if err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestParseInstalledProgramsRequiresConfirmation(t *testing.T) {
	_, _, err := parseInstalledPrograms(nil)
	if err == nil || !strings.Contains(err.Error(), "подтверждения") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestParseInstalledProgramsReportsPartialRegistryScan(t *testing.T) {
	warning := encodedProgramWarningLine(t, installedProgramScanWarning{
		Stage: "HKEY_USERS", Message: "Отказано в доступе",
	})
	programs, warnings, err := parseInstalledPrograms([]string{
		warning,
		"COMMAND_LINE|SCAN_SUMMARY|2|1",
		"COMMAND_OK",
	})
	if err != nil {
		t.Fatalf("parseInstalledPrograms() error = %v", err)
	}
	if len(programs) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "HKEY_USERS") {
		t.Fatalf("programs = %#v, warnings = %#v", programs, warnings)
	}
}

func TestParseInstalledProgramsRejectsTruncatedOutput(t *testing.T) {
	_, _, err := parseInstalledPrograms([]string{
		"COMMAND_LINE|SCAN_SUMMARY|2|0",
		"COMMAND_OUTPUT_TRUNCATED",
		"COMMAND_OK",
	})
	if err == nil || !strings.Contains(err.Error(), "обрезан") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestParseInstalledProgramsRequiresReadableRegistrySource(t *testing.T) {
	_, _, err := parseInstalledPrograms([]string{
		"COMMAND_LINE|SCAN_SUMMARY|0|2",
		"COMMAND_OK",
	})
	if err == nil || !strings.Contains(err.Error(), "ни одной ветки") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestRunInstalledProgramScanRetriesTransientTimeout(t *testing.T) {
	attempts := 0
	runner := func(context.Context, string, string, string, string, int) ([]string, error) {
		attempts++
		if attempts == 1 {
			return []string{"ERROR|Прекращено из-за истечения времени ожидания"}, nil
		}
		return []string{"COMMAND_LINE|SCAN_SUMMARY|2|0", "COMMAND_OK"}, nil
	}
	var logs []string
	programs, warnings, _, err := runInstalledProgramScan(
		context.Background(), "192.0.2.1", "DOMAIN\\admin", "password",
		func(message string) { logs = append(logs, message) }, runner,
	)
	if err != nil || len(programs) != 0 || len(warnings) != 0 || attempts != 2 {
		t.Fatalf("programs=%#v warnings=%#v attempts=%d err=%v", programs, warnings, attempts, err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "Повторная попытка") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRunInstalledProgramScanDoesNotRetryAccessDenied(t *testing.T) {
	attempts := 0
	runner := func(context.Context, string, string, string, string, int) ([]string, error) {
		attempts++
		return []string{"ERROR|Отказано в доступе"}, nil
	}
	_, _, _, err := runInstalledProgramScan(
		context.Background(), "192.0.2.1", "DOMAIN\\admin", "password",
		func(string) {}, runner,
	)
	if err == nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func encodedProgramLine(t *testing.T, program constandstruct.InstalledProgram) string {
	t.Helper()
	data, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	return "COMMAND_LINE|PROGRAM|" + base64.StdEncoding.EncodeToString(data)
}

func encodedProgramWarningLine(t *testing.T, warning installedProgramScanWarning) string {
	t.Helper()
	data, err := json.Marshal(warning)
	if err != nil {
		t.Fatal(err)
	}
	return "COMMAND_LINE|SCAN_WARNING|" + base64.StdEncoding.EncodeToString(data)
}
