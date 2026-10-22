package psscript

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPsScriptParsesInWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверка предназначена для Windows PowerShell")
	}
	for name, script := range map[string]string{
		"process operations": PsScript,
		"server command":     CommandScript,
		"command cleanup":    CleanupCommandScript,
		"installed programs": InstalledProgramsScript,
	} {
		t.Run(name, func(t *testing.T) {
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
		})
	}
}

func TestInstalledProgramsScriptAvoidsWin32Product(t *testing.T) {
	for _, required := range []string{
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*`,
		`HKEY_USERS\$sid\Software\Microsoft\Windows\CurrentVersion\Uninstall\*`,
		// Владелец записи отличает личную установку сотрудника от общей:
		// на терминальном сервере это главное, что нужно администратору.
		"Owner = $owner",
		`Owner = "Весь компьютер"`,
		"Resolve-AccountName",
		"Профили без активного сеанса",
		"PROGRAM|",
		"RegistryPath",
		"QuietUninstallString",
		"CanSilentUninstall",
		"SCAN_WARNING|",
		"SCAN_SUMMARY|",
		"$scannedSources",
		"$failedSources",
	} {
		if !strings.Contains(InstalledProgramsScript, required) {
			t.Fatalf("InstalledProgramsScript должен содержать %q", required)
		}
	}
	if strings.Contains(InstalledProgramsScript, "Win32_Product") {
		t.Fatal("InstalledProgramsScript не должен обращаться к Win32_Product")
	}
}

func TestInstalledProgramsScriptRunsLocally(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверка предназначена для Windows PowerShell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", `$source=[Console]::In.ReadToEnd(); & ([ScriptBlock]::Create($source))`,
	)
	command.Stdin = strings.NewReader(InstalledProgramsScript)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("локальная проверка сканера превысила 30 секунд: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("локальная проверка сканера завершилась с ошибкой: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "SCAN_SUMMARY|") {
		t.Fatalf("локальная проверка не вернула сводку сканирования:\n%s", output)
	}
}

func TestOwnerLookupUsesQuserBeforeGetOwner(t *testing.T) {
	processQuery := strings.Index(PsScript, `$processes = Get-CimInstance`)
	quser := strings.LastIndex(PsScript, `$lookup = Get-SessionOwnerLookup`)
	getOwner := strings.LastIndex(PsScript, `$ownerInfo = Invoke-CimMethod`)
	if processQuery < 0 || quser < processQuery || getOwner < quser {
		t.Fatal("после поиска процессов quser должен выполняться до резервного GetOwner")
	}
	for _, required := range []string{
		`STAGE|Определение пользователей сеансов через quser`,
		`STAGE|Сопоставление процессов с пользователями`,
		`SESSION_LOOKUP|`,
		`-Property ProcessId,SessionId,Name,CreationDate`,
	} {
		if !strings.Contains(PsScript, required) {
			t.Fatalf("оптимизированный поиск владельцев должен содержать %q", required)
		}
	}
	if strings.Contains(PsScript, "Start-Sleep -Milliseconds 750") {
		t.Fatal("фиксированная задержка подтверждения завершения процесса не должна возвращаться")
	}
}

func TestProcessSearchAllowsSlowCIMServer(t *testing.T) {
	for _, required := range []string{
		`$directCimTimeoutSeconds = 20`,
		`STAGE|Подключение CIM/DCOM`,
		`STAGE|Поиск процессов`,
		`STAGE|Сопоставление процессов с пользователями`,
		`$processQueryTimeoutSeconds = 60`,
		`Поиск процессов не завершён за `,
		`GetOwner -OperationTimeoutSec 15`,
	} {
		if !strings.Contains(PsScript, required) {
			t.Fatalf("поиск процессов должен содержать %q", required)
		}
	}
}

func TestRemoteUsersScriptRunsOnTarget(t *testing.T) {
	for _, required := range []string{
		`Name='explorer.exe'`,
		"Win32_UserProfile",
		"GetOwnerSid",
		"GetOwner",
		"REMOTE_USER|",
		"WARN_REMOTE_USER|",
		"USERS_OK",
	} {
		if !strings.Contains(RemoteUsersScript, required) {
			t.Fatalf("RemoteUsersScript должен содержать %q", required)
		}
	}

	// Главное свойство этого сценария: он выполняется на целевом компьютере,
	// поэтому вызовы CIM локальные. Появление CIM-сессии вернуло бы разбор
	// сеансов на машину администратора — по два обращения DCOM на процесс.
	if strings.Contains(RemoteUsersScript, "CimSession") {
		t.Fatal("сценарий не должен работать через CIM-сессию: вызовы обязаны быть локальными")
	}
	if strings.Contains(RemoteUsersScript, "Win32_LoggedOnUser") {
		t.Fatal("массовая ассоциация Win32_LoggedOnUser не должна использоваться")
	}

	// Разбор владельца делается через -InputObject: у Invoke-CimMethod нет
	// параметра -Filter, и попытка его передать роняла каждую сессию.
	if !strings.Contains(RemoteUsersScript, "-InputObject $explorer") {
		t.Fatal("GetOwner должен вызываться с -InputObject")
	}
	if strings.Contains(RemoteUsersScript, "Invoke-CimMethod -CimSession") {
		t.Fatal("вызов метода не должен идти через CIM-сессию")
	}
}

func TestPsScriptNoLongerCarriesUserScan(t *testing.T) {
	// Разбор сеансов переехал в RemoteUsersScript. Старая ветка осталась бы
	// мёртвым кодом в сценарии, который правят под другие задачи.
	if strings.Contains(PsScript, "scanusers") {
		t.Fatal("ветка scanusers должна быть удалена из PsScript")
	}
}

func TestCommandScriptUsesDCOMWithoutWinRM(t *testing.T) {
	for _, required := range []string{
		"New-CimSessionOption -Protocol Dcom",
		"Win32_Process",
		"StdRegProv",
		"CredentialAndScriptFromStdin",
		"ExpiresUtcTicks",
		"Remove-StaleRemoteJobs",
		"OutputTruncated",
		"Start-Sleep -Milliseconds 750",
		"Security.Cryptography.Aes",
		`$cimConnectTimeoutSeconds = 20`,
		`$cimOperationTimeoutSeconds = 60`,
		`OperationTimeoutSec = $cimOperationTimeoutSeconds`,
	} {
		if !strings.Contains(CommandScript, required) {
			t.Fatalf("CommandScript должен содержать %q", required)
		}
	}
	for _, forbidden := range []string{"Invoke-Command", "TrustedHosts"} {
		if strings.Contains(CommandScript, forbidden) {
			t.Fatalf("CommandScript не должен зависеть от %q", forbidden)
		}
	}
	if strings.Contains(CommandScript, "OperationTimeoutSec = 15") {
		t.Fatal("транспорт команд не должен обрываться по прежнему внутреннему тайм-ауту 15 секунд")
	}
}

func TestCommandScriptSupportsInteractiveUserSession(t *testing.T) {
	for _, required := range []string{
		`TargetUserSID`,
		`TargetUserName`,
		`TargetSessionID`,
		`Registry::HKEY_CURRENT_USER\Software\1Cproc\Jobs\$JobId`,
		`RegisterTaskDefinition`,
		`RunEx($null, 4, $targetSessionID, $targetUserName)`,
		`SESSION_EXECUTION|`,
		`schtasks.exe /Delete`,
	} {
		if !strings.Contains(CommandScript, required) {
			t.Fatalf("режим пользовательской сессии должен содержать %q", required)
		}
	}
	if !strings.Contains(CleanupCommandScript, `TargetUserSID`) {
		t.Fatal("аварийная очистка должна учитывать пользовательский раздел реестра")
	}
	if !strings.Contains(CleanupCommandScript, `schtasks.exe /End`) {
		t.Fatal("аварийная очистка должна останавливать запущенную пользовательскую задачу")
	}
}

func TestCommandScriptRemovesInputChunksBeforeExecution(t *testing.T) {
	removeChunks := strings.Index(CommandScript, `Remove-ItemProperty -LiteralPath $registryPath -Name ("Script{0:D4}" -f $index)`)
	executeScript := strings.Index(CommandScript, `& ([ScriptBlock]::Create($source))`)
	if removeChunks < 0 || executeScript < 0 || removeChunks > executeScript {
		t.Fatal("части пользовательского сценария должны удаляться до его выполнения")
	}
}

func TestCommandScriptHandlesMissingOptionalRegistryValues(t *testing.T) {
	for _, required := range []string{
		`$AllowMissing -and [uint32]$result.ReturnValue -in @(1, 2)`,
		`$outputTruncatedText = "0"`,
		`$outputTruncatedText = "1"`,
		`Set-JobString "OutputTruncated" $outputTruncatedText`,
	} {
		if !strings.Contains(CommandScript, required) {
			t.Fatalf("CommandScript должен содержать %q", required)
		}
	}
	if strings.Contains(CommandScript, `if ($AllowMissing -and [uint32]$result.ReturnValue -eq 2)`) {
		t.Fatal("необязательное значение не должно обрабатывать только код 2")
	}
	outputFlag := strings.Index(CommandScript, `Set-JobString "OutputTruncated" $outputTruncatedText`)
	statusOK := strings.Index(CommandScript, `Set-JobString "Status" "OK"`)
	if outputFlag < 0 || statusOK < 0 || outputFlag > statusOK {
		t.Fatal("OutputTruncated должен быть опубликован до статуса OK")
	}
}
