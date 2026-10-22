package psscript

const PsScript = `
param(
    [Parameter(Mandatory=$true)][string]$ComputerName,
    [Parameter(Mandatory=$true)][ValidateSet("find","findfast","kill","killids","notify")][string]$Action,
    [string]$ProcessNames,
    [string]$ProcessIds,
    [string]$Message,
    [string]$DefaultUser,
    [switch]$CredentialFromStdin
)

$utf8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$ErrorActionPreference = "Stop"
$session = $null

function Write-ControlledError([string]$Text) {
    Write-Output ("ERROR|" + $Text)
}

function Write-Timing([string]$Stage, [Diagnostics.Stopwatch]$Timer) {
    Write-Output ("TIMING|" + $Stage + "|" + $Timer.ElapsedMilliseconds)
}

function Convert-QuserLinesToMap([object[]]$Lines) {
    $owners = @{}
    foreach ($line in $Lines) {
        $tokens = @(($line.ToString().Trim() -replace '^>', '') -split '\s+' | Where-Object { $_ -ne "" })
        if ($tokens.Count -lt 2) {
            continue
        }

        $userName = $tokens[0].TrimStart([char]'>')
        for ($index = 1; $index -lt $tokens.Count; $index++) {
            $sessionId = 0
            if ([int]::TryParse($tokens[$index], [ref]$sessionId)) {
                if (-not [string]::IsNullOrWhiteSpace($userName)) {
                    $owners[$sessionId] = $userName
                }
                break
            }
        }
    }
    return $owners
}

function Get-SessionOwnerLookup([string]$ServerName) {
    $owners = @{}
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $nativeProcess = $null
    try {
        $quserPath = Join-Path $env:SystemRoot "System32\quser.exe"
        if (-not (Test-Path -LiteralPath $quserPath)) {
            throw "quser.exe не найден"
        }

        $startInfo = New-Object Diagnostics.ProcessStartInfo
        $startInfo.FileName = $quserPath
        $startInfo.Arguments = "/server:" + $ServerName
        $startInfo.UseShellExecute = $false
        $startInfo.CreateNoWindow = $true
        $startInfo.RedirectStandardOutput = $true
        $startInfo.RedirectStandardError = $true
        $oemEncoding = [Text.Encoding]::GetEncoding([Globalization.CultureInfo]::CurrentCulture.TextInfo.OEMCodePage)
        $startInfo.StandardOutputEncoding = $oemEncoding
        $startInfo.StandardErrorEncoding = $oemEncoding

        $nativeProcess = New-Object Diagnostics.Process
        $nativeProcess.StartInfo = $startInfo
        if (-not $nativeProcess.Start()) {
            throw "не удалось запустить quser.exe"
        }
        $stdoutTask = $nativeProcess.StandardOutput.ReadToEndAsync()
        $stderrTask = $nativeProcess.StandardError.ReadToEndAsync()
        if (-not $nativeProcess.WaitForExit(3000)) {
            $nativeProcess.Kill()
            $nativeProcess.WaitForExit()
            throw "quser не ответил за 3 секунды"
        }

        $stdout = $stdoutTask.Result
        $stderr = $stderrTask.Result
        if ($nativeProcess.ExitCode -ne 0) {
            throw ("quser завершился с кодом " + $nativeProcess.ExitCode + ": " + $stderr + " " + $stdout)
        }

        $owners = Convert-QuserLinesToMap ([regex]::Split($stdout, "\r?\n"))
        return [PSCustomObject]@{ Owners = $owners; ErrorText = ""; ElapsedMs = $timer.ElapsedMilliseconds }
    } catch {
        $reason = $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
        return [PSCustomObject]@{ Owners = $owners; ErrorText = $reason; ElapsedMs = $timer.ElapsedMilliseconds }
    } finally {
        if ($nativeProcess) {
            $nativeProcess.Dispose()
        }
    }
}

function Invoke-FilteredTaskkill($CimSession, [string[]]$AllowedNames, [int[]]$ProcessIds) {
    $started = $false
    $verified = $false
    $remaining = @{}
    $errors = @()

    try {
        if ($ProcessIds.Count -eq 0) {
            throw "не указаны PID для taskkill.exe"
        }

        $operatingSystem = Get-CimInstance -CimSession $CimSession -ClassName Win32_OperatingSystem -OperationTimeoutSec 5
        if (-not $operatingSystem -or [string]::IsNullOrWhiteSpace($operatingSystem.WindowsDirectory)) {
            throw "не удалось определить каталог Windows"
        }

        $taskkillExe = Join-Path $operatingSystem.WindowsDirectory "System32\taskkill.exe"
        $pidArguments = ($ProcessIds | ForEach-Object { " /PID " + $_ }) -join ""

        foreach ($allowedName in $AllowedNames) {
            if ($allowedName -notmatch '^[A-Za-z0-9._-]+$') {
                $errors += "некорректное имя процесса: " + $allowedName
                continue
            }

            $commandLine = '"' + $taskkillExe + '" /F /FI "IMAGENAME eq ' + $allowedName + '"' + $pidArguments
            $createResult = Invoke-CimMethod -CimSession $CimSession -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $commandLine } -OperationTimeoutSec 5
            if ($createResult.ReturnValue -eq 0) {
                $started = $true
            } else {
                $errors += "taskkill.exe не запущен, код " + $createResult.ReturnValue
            }
        }

        if ($started) {
            $pidFilter = ($ProcessIds | ForEach-Object { "ProcessId=" + $_ }) -join " or "
            $verifyTimer = [Diagnostics.Stopwatch]::StartNew()
            do {
                $remaining.Clear()
                $remainingProcesses = @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter $pidFilter -OperationTimeoutSec 5)
                foreach ($process in $remainingProcesses) {
                    $remaining[[int]$process.ProcessId] = $process
                }
                if ($remaining.Count -eq 0 -or $verifyTimer.ElapsedMilliseconds -ge 1000) {
                    break
                }
                Start-Sleep -Milliseconds 100
            } while ($true)
            $verified = $true
        }
    } catch {
        $errors += $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
    }

    return [PSCustomObject]@{
        Started = $started
        Verified = $verified
        Remaining = $remaining
        ErrorText = ($errors -join "; ")
    }
}

try {
    if (-not $CredentialFromStdin) {
        Write-ControlledError "Приложение не передало учётные данные"
        exit 1
    }
    if ([string]::IsNullOrWhiteSpace($DefaultUser)) {
        Write-ControlledError "Не указан логин"
        exit 1
    }

    try {
        $encodedPassword = [Console]::In.ReadLine()
        if ([string]::IsNullOrWhiteSpace($encodedPassword)) {
            throw "Не указан пароль"
        }
        $passwordBytes = [Convert]::FromBase64String($encodedPassword)
        $plainPassword = [Text.Encoding]::UTF8.GetString($passwordBytes)
        $securePassword = ConvertTo-SecureString -String $plainPassword -AsPlainText -Force
        $credential = New-Object System.Management.Automation.PSCredential -ArgumentList $DefaultUser, $securePassword
    } catch {
        Write-ControlledError ("Не удалось получить учётные данные: " + $_.Exception.Message)
        exit 1
    } finally {
        $encodedPassword = $null
        $passwordBytes = $null
        $plainPassword = $null
        $securePassword = $null
    }

    $connectionTimer = [Diagnostics.Stopwatch]::StartNew()
    $directCimTimeoutSeconds = 20
    Write-Output "STAGE|Подключение CIM/DCOM"
    $sessionOption = New-CimSessionOption -Protocol Dcom
    try {
        $session = New-CimSession -ComputerName $ComputerName -Credential $credential -SessionOption $sessionOption -OperationTimeoutSec $directCimTimeoutSeconds -ErrorAction Stop
    } catch {
        throw ("Ошибка подключения CIM/DCOM (лимит ожидания " + $directCimTimeoutSeconds + " сек.): " + $_.Exception.Message)
    }
    Write-Timing "Подключение CIM" $connectionTimer


           if ($Action -eq "notify") {
        if ([string]::IsNullOrWhiteSpace($Message)) {
            Write-ControlledError "Не указан текст оповещения"
            exit 1
        }

        $notifyTimer = [Diagnostics.Stopwatch]::StartNew()

        try {
            $operatingSystem = Get-CimInstance -CimSession $session -ClassName Win32_OperatingSystem -OperationTimeoutSec 15

            $windowsDirectory = ([string]$operatingSystem.WindowsDirectory).Trim()
            if ([string]::IsNullOrWhiteSpace($windowsDirectory)) {
                throw "не удалось определить каталог Windows"
            }

            $msgExe = Join-Path $windowsDirectory "System32\msg.exe"

            $safeMessage = $Message.Replace('"', "'").Replace([char]13, " ").Replace([char]10, " ").Trim()

            if ([string]::IsNullOrWhiteSpace($safeMessage)) {
                throw "после обработки текст оповещения оказался пустым"
            }

            # Звёздочка означает все пользовательские сессии сервера.
            $commandLine = '"' + $msgExe + '" * /TIME:86400 "' + $safeMessage + '"'

            $result = Invoke-CimMethod -CimSession $session -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $commandLine } -OperationTimeoutSec 15

            if ([uint32]$result.ReturnValue -ne 0) {
                throw ("msg.exe не запущен, код Win32_Process.Create: " + $result.ReturnValue)
            }

            Write-Output ("NOTIFY_OK|ALL|" + $result.ProcessId)
        } catch {
            $reason = $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
            Write-Output ("NOTIFY_FAIL|" + $reason)
        }

        Write-Timing "Оповещение всех пользовательских сессий" $notifyTimer
        exit 0
    }

    if ([string]::IsNullOrWhiteSpace($ProcessNames)) {
        Write-ControlledError "Не указаны имена процессов"
        exit 1
    }

    $allowedNames = @(
        $ProcessNames.Split(",") |
            ForEach-Object { $_.Trim() } |
            Where-Object { $_ -ne "" }
    )

    if ($Action -eq "killids") {
        if ([string]::IsNullOrWhiteSpace($ProcessIds)) {
            Write-ControlledError "Не указаны PID для завершения"
            exit 1
        }

        $terminateTimer = [Diagnostics.Stopwatch]::StartNew()
        $validPids = @()
        foreach ($idText in $ProcessIds.Split(",")) {
            $pidValue = 0
            if (-not [int]::TryParse($idText.Trim(), [ref]$pidValue) -or $pidValue -le 0) {
                Write-Output ("KILL_FAIL|" + $idText + "|некорректный PID")
                continue
            }
            $validPids += $pidValue
        }

        if ($validPids.Count -eq 0) {
            Write-Timing "Завершение выбранных PID" $terminateTimer
            exit 0
        }

        $killResult = Invoke-FilteredTaskkill $session $allowedNames $validPids
        if (-not [string]::IsNullOrWhiteSpace($killResult.ErrorText)) {
            Write-Output ("KILL_FAIL|0|" + $killResult.ErrorText)
        }

        foreach ($pidValue in $validPids) {
            if (-not $killResult.Started) {
                Write-Output ("KILL_FAIL|" + $pidValue + "|taskkill.exe не был запущен")
                continue
            }
            if (-not $killResult.Verified) {
                Write-Output ("KILL_FAIL|" + $pidValue + "|не удалось подтвердить завершение процесса")
                continue
            }
            if ($killResult.Remaining.ContainsKey([int]$pidValue)) {
                $remainingProcess = $killResult.Remaining[[int]$pidValue]
                if ($allowedNames -contains $remainingProcess.Name) {
                    Write-Output ("KILL_FAIL|" + $pidValue + "|процесс всё ещё выполняется")
                } else {
                    Write-Output ("KILL_FAIL|" + $pidValue + "|PID уже принадлежит другому процессу: " + $remainingProcess.Name)
                }
            } else {
                Write-Output ("KILLED|" + $pidValue + "|0|" + $allowedNames[0] + "|не проверялся")
            }
        }
        Write-Timing "Завершение выбранных PID" $terminateTimer
        exit 0
    }

    $escapedNames = $allowedNames | ForEach-Object { $_.Replace("'", "''") }
    $filter = ($escapedNames | ForEach-Object { "Name='$_'" }) -join " or "
    $queryTimer = [Diagnostics.Stopwatch]::StartNew()
    $processQueryTimeoutSeconds = 60
    Write-Output "STAGE|Поиск процессов"
    try {
        $processes = Get-CimInstance -CimSession $session -ClassName Win32_Process -Filter $filter -Property ProcessId,SessionId,Name,CreationDate -OperationTimeoutSec $processQueryTimeoutSeconds
    } catch {
        throw ("Поиск процессов не завершён за " + $processQueryTimeoutSeconds + " сек.: " + $_.Exception.Message)
    }
    Write-Timing "Поиск процессов" $queryTimer

    if (-not $processes) {
        Write-Output "NONE_FOUND"
        exit 0
    }

    $sessionOwners = @{}
    $quserHasOwners = $false
    $ownerLookupUnavailable = $false


    if ($Action -eq "kill") {
        $actionTimer = [Diagnostics.Stopwatch]::StartNew()
        $processIdsToKill = @($processes | ForEach-Object { [int]$_.ProcessId })
        $killResult = Invoke-FilteredTaskkill $session $allowedNames $processIdsToKill
        if (-not [string]::IsNullOrWhiteSpace($killResult.ErrorText)) {
            Write-Output ("KILL_FAIL|0|" + $killResult.ErrorText)
        }

        foreach ($process in $processes) {
            $pidValue = [int]$process.ProcessId
            if (-not $killResult.Started) {
                Write-Output ("KILL_FAIL|" + $pidValue + "|taskkill.exe не был запущен")
                continue
            }
            if (-not $killResult.Verified) {
                Write-Output ("KILL_FAIL|" + $pidValue + "|не удалось подтвердить завершение процесса")
                continue
            }
            if ($killResult.Remaining.ContainsKey($pidValue)) {
                $remainingProcess = $killResult.Remaining[$pidValue]
                if ($allowedNames -contains $remainingProcess.Name) {
                    Write-Output ("KILL_FAIL|" + $pidValue + "|процесс всё ещё выполняется")
                } else {
                    Write-Output ("KILL_FAIL|" + $pidValue + "|PID уже принадлежит другому процессу: " + $remainingProcess.Name)
                }
            } else {
                Write-Output ("KILLED|" + $pidValue + "|" + $process.SessionId + "|" + $process.Name + "|не проверялся")
            }
        }
        Write-Timing "Завершение всех процессов" $actionTimer
        exit 0
    }
    if ($Action -eq "find") {
        Write-Output "STAGE|Определение пользователей сеансов через quser"
        $lookup = Get-SessionOwnerLookup $ComputerName
        $sessionOwners = $lookup.Owners
        Write-Output ("TIMING|quser|" + $lookup.ElapsedMs)
        if ($lookup.ErrorText) {
            Write-Output ("WARN_QUSER|" + $lookup.ErrorText)
        } elseif ($sessionOwners.Count -gt 0) {
            $quserHasOwners = $true
            Write-Output ("SESSION_LOOKUP|" + $sessionOwners.Count)
        } else {
            Write-Output "WARN_QUSER|quser не вернул пользовательские сеансы"
        }
        Write-Output "STAGE|Сопоставление процессов с пользователями"
    }
    foreach ($process in $processes) {
        if ($Action -eq "find" -or $Action -eq "findfast") {
            $owner = "не проверялся"
            if ($Action -eq "find") {
                $owner = "неизвестно"
                if ($sessionOwners.ContainsKey([int]$process.SessionId)) {
                    $owner = [string]$sessionOwners[[int]$process.SessionId]
                } elseif (-not $quserHasOwners -and -not $ownerLookupUnavailable) {
                    try {
                        Write-Output ("STAGE|Определение владельца PID=" + $process.ProcessId + " через GetOwner")
                        $ownerInfo = Invoke-CimMethod -InputObject $process -MethodName GetOwner -OperationTimeoutSec 15 -ErrorAction Stop
                        if ($ownerInfo.ReturnValue -eq 0 -and $ownerInfo.User) {
                            $owner = "$($ownerInfo.Domain)\$($ownerInfo.User)"
                            $sessionOwners[[int]$process.SessionId] = $owner
                        } else {
                            throw ("GetOwner вернул код " + $ownerInfo.ReturnValue)
                        }
                    } catch {
                        $reason = $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
                        Write-Output ("WARN_OWNER|" + $process.ProcessId + "|" + $reason)
                        $ownerLookupUnavailable = $true
                    }
                }
            }
            Write-Output ("FOUND|" + $process.ProcessId + "|" + $process.SessionId + "|" + $process.Name + "|" + $owner)
            continue
        }
    }
} catch {
    Write-ControlledError $_.Exception.Message
    exit 1
} finally {
    if ($session) {
        Remove-CimSession -CimSession $session -ErrorAction SilentlyContinue
    }
}
`
