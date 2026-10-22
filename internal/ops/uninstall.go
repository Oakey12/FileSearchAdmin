package ops

import (
	"context"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"regexp"
	"strings"
)

var machineUninstallPath = regexp.MustCompile(`(?i)^HKEY_LOCAL_MACHINE\\SOFTWARE\\(?:WOW6432Node\\)?Microsoft\\Windows\\CurrentVersion\\Uninstall\\[^\\]+$`)

// UninstallInstalledProgram удаляет выбранную программу на целевом компьютере.
// Команда деинсталляции никогда не принимается от интерфейса: удалённый скрипт
// повторно читает защищённую системную запись реестра и сверяет DisplayName.
func UninstallInstalledProgram(
	ctx context.Context,
	targetIP, username, password string,
	program constandstruct.InstalledProgram,
	logLine func(string),
	onComplete func(error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	program.Name = strings.TrimSpace(program.Name)
	program.RegistryPath = strings.TrimSpace(program.RegistryPath)

	if net.ParseIP(targetIP) == nil {
		logLine("Удаление: введите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" || password == "" {
		logLine("Удаление: введите логин и пароль администратора целевого компьютера.")
		return false
	}
	if program.Name == "" || !program.CanSilentUninstall {
		logLine("Удаление: для выбранной программы недоступно безопасное тихое удаление.")
		return false
	}
	if err := validateInstalledProgramRegistryPath(program.RegistryPath); err != nil {
		logLine("Удаление: " + err.Error())
		return false
	}

	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.UninstallTimeout)
		defer cancel()

		target := constandstruct.Server{Name: "Удаление программы", IP: targetIP}
		logLine(fmt.Sprintf("=== Удаление программы %q на %s ===", program.Name, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.UninstallTimeout, logLine)
		lines, runErr := remotefunc.RunRemoteScriptWithProgress(
			operationCtx,
			targetIP,
			username,
			password,
			buildUninstallScript(program),
			remoteProgressLogger(target, logLine),
		)

		resultErr := runErr
		if resultErr == nil {
			resultErr = parseUninstallResult(lines)
		}
		finishStatus(resultErr)

		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine(fmt.Sprintf("Удаление: %v", resultErr))
			}
		} else {
			logRemoteWarnings(target, lines, logLine)
			logLine(fmt.Sprintf("Удаление: программа %q успешно удалена с %s.", program.Name, targetIP))
		}
		logLine("--- Удаление программы завершено ---")

		if onComplete != nil {
			onComplete(resultErr)
		}
	}()
	return true
}

func validateInstalledProgramRegistryPath(path string) error {
	if !machineUninstallPath.MatchString(strings.TrimSpace(path)) {
		return fmt.Errorf("недопустимый системный путь программы")
	}
	return nil
}

func quotePowerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func buildUninstallScript(program constandstruct.InstalledProgram) string {
	registryPath := quotePowerShellLiteral("Registry::" + strings.TrimSpace(program.RegistryPath))
	expectedName := quotePowerShellLiteral(strings.TrimSpace(program.Name))

	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$registryPath = %s
$expectedName = %s

if ($registryPath -notmatch '^Registry::HKEY_LOCAL_MACHINE\\SOFTWARE\\(WOW6432Node\\)?Microsoft\\Windows\\CurrentVersion\\Uninstall\\[^\\]+$') {
    throw "Недопустимый системный путь программы."
}

$item = Get-ItemProperty -LiteralPath $registryPath -ErrorAction Stop
$actualName = ([string]$item.DisplayName).Trim()
if ($actualName -cne $expectedName) {
    throw "Запись программы изменилась после сканирования. Выполните сканирование повторно."
}

$productCode = Split-Path -Path $registryPath -Leaf
$isMSI = [int]$item.WindowsInstaller -eq 1 -and $productCode -match '^\{[0-9A-Fa-f-]{36}\}$'
if ($isMSI) {
    $executable = Join-Path $env:SystemRoot 'System32\msiexec.exe'
    $arguments = '/x "' + $productCode + '" /qn /norestart'
} else {
    $quietCommand = [Environment]::ExpandEnvironmentVariables(([string]$item.QuietUninstallString).Trim())
    if ([string]::IsNullOrWhiteSpace($quietCommand)) {
        throw "Для программы не зарегистрирована команда тихого удаления."
    }

    if ($quietCommand -match '^\s*"([^"]+\.exe)"\s*(.*)$') {
        $executable = $matches[1]
        $arguments = $matches[2]
    } elseif ($quietCommand -match '^\s*(.+?\.exe)(?:\s+(.*))?$') {
        $executable = $matches[1].Trim()
        $arguments = $matches[2]
    } else {
        throw "Команда тихого удаления имеет неподдерживаемый формат."
    }
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        $resolvedExecutable = Get-Command -Name $executable -CommandType Application -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($null -eq $resolvedExecutable) {
            throw "Файл деинсталлятора не найден. Выполните сканирование повторно."
        }
        $executable = $resolvedExecutable.Source
    }
}

$process = Start-Process -FilePath $executable -ArgumentList $arguments -Wait -PassThru -WindowStyle Hidden
$acceptedExitCodes = @(0, 1605, 1614, 1641, 3010)
if ($acceptedExitCodes -notcontains $process.ExitCode) {
    throw ("Деинсталлятор завершился с кодом " + $process.ExitCode + ".")
}

$removed = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if (-not (Test-Path -LiteralPath $registryPath)) {
        $removed = $true
        break
    }
    Start-Sleep -Seconds 2
}
if (-not $removed -and $process.ExitCode -notin @(1641, 3010)) {
    throw "Деинсталлятор завершился, но запись программы осталась. Выполните сканирование повторно."
}

Write-Output ("UNINSTALL_OK|" + $process.ExitCode)
`, registryPath, expectedName)
}

func parseUninstallResult(lines []string) error {
	if text := remoteError(lines); text != "" {
		return fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}
	confirmed := false
	succeeded := false
	for _, line := range lines {
		switch {
		case line == "COMMAND_OK":
			confirmed = true
		case strings.HasPrefix(line, "COMMAND_LINE|UNINSTALL_OK|"):
			succeeded = true
		}
	}
	if !confirmed || !succeeded {
		return fmt.Errorf("ответ не содержит подтверждения успешного удаления")
	}
	return nil
}
