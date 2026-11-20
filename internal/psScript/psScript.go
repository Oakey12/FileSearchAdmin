package psscript

const PsScript = `
param(
    [Parameter(Mandatory=$true)][string]$ComputerName,
    [Parameter(Mandatory=$true)][ValidateSet("find","findfast","terminate","notify","diagnose")][string]$Action,
    [string]$ProcessNames,
    [string]$ProcessIds,
    [string]$Message,
    [string]$DefaultUser,
    [switch]$DryRun,
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

# ---------------------------------------------------------------------------
# Завершение процессов 1С.
#
# Один конвейер для боевого завершения и для теста (-DryRun): тест проходит те
# же шаги и расходится с боем только перед необратимыми вызовами.
#   1. Поиск процессов; личность процесса — PID + время запуска.
#   2. Владельцы сеансов (quser, затем GetOwner) — до завершения.
#   3. taskkill /F пакетом, с ожиданием завершения самого taskkill.
#   4. Оставшимся — Win32_Process.Terminate по одному, с кодом ответа.
#   5. Ожидание выхода: TerminateProcess асинхронен.
# ---------------------------------------------------------------------------

$TaskkillTimeoutMs = 20000
$AfterTaskkillGraceMs = 2000
$ExitWaitTimeoutMs = 8000
$TaskkillPidsPerCall = 200

function Format-Reason([string]$Text) {
    if ($null -eq $Text) {
        return ""
    }
    return $Text.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ").Trim()
}

# Коды возврата методов Win32_Process (Create и Terminate).
function Get-Win32ProcessCodeText([uint32]$Code) {
    switch ($Code) {
        0 { return "успешно" }
        2 { return "доступ запрещён (код 2)" }
        3 { return "недостаточно привилегий (код 3)" }
        8 { return "неизвестная ошибка (код 8)" }
        9 { return "путь не найден (код 9)" }
        21 { return "неверный параметр (код 21)" }
        default { return ("код " + $Code) }
    }
}

# PID после завершения может достаться другому процессу, поэтому процесс
# опознаётся по паре PID + время запуска.
function Get-ProcessKey($Process) {
    $ticks = 0
    if ($Process.CreationDate) {
        $ticks = ([datetime]$Process.CreationDate).Ticks
    }
    return ([string]$Process.ProcessId + "@" + $ticks)
}

function Get-TargetProcesses($CimSession, [string[]]$Names, [int[]]$Ids) {
    $nameFilter = ($Names | ForEach-Object { "Name='" + $_.Replace("'", "''") + "'" }) -join " or "
    $filter = "(" + $nameFilter + ")"
    if ($Ids.Count -gt 0) {
        $filter += " and (" + (($Ids | ForEach-Object { "ProcessId=" + $_ }) -join " or ") + ")"
    }
    return @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter $filter -Property Handle,ProcessId,SessionId,Name,CreationDate -OperationTimeoutSec 60)
}

# Другие исполняемые файлы 1С (1cv7.exe, 1cv7l.exe, 1cv8.exe...), не входящие в
# список завершения. Если 1С на сервере запущена под другим именем, поиск по
# основному имени их не видит и выглядит как «не закрывается».
function Get-OtherOneCVariants($CimSession, [string[]]$Names) {
    $counts = @{}
    $found = @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter "Name LIKE '1cv%'" -Property Name -OperationTimeoutSec 60)
    foreach ($process in $found) {
        $name = [string]$process.Name
        if ($Names -contains $name) {
            continue
        }
        if ($counts.ContainsKey($name)) {
            $counts[$name] = $counts[$name] + 1
        } else {
            $counts[$name] = 1
        }
    }
    return $counts
}

# Заполняет $Owners (номер сеанса -> пользователь). Предупреждения пишет в вывод.
function Resolve-Owners([string]$ServerName, [object[]]$Processes, [hashtable]$Owners) {
    Write-Output "STAGE|Определение пользователей сеансов через quser"
    $lookup = Get-SessionOwnerLookup $ServerName
    Write-Output ("TIMING|quser|" + $lookup.ElapsedMs)
    if ($lookup.ErrorText) {
        Write-Output ("WARN_QUSER|" + $lookup.ErrorText)
    }
    foreach ($sessionKey in $lookup.Owners.Keys) {
        $Owners[[int]$sessionKey] = $lookup.Owners[$sessionKey]
    }
    $getOwnerUnavailable = $false
    foreach ($process in $Processes) {
        $sessionId = [int]$process.SessionId
        if ($Owners.ContainsKey($sessionId) -or $getOwnerUnavailable) {
            continue
        }
        try {
            $info = Invoke-CimMethod -InputObject $process -MethodName GetOwner -OperationTimeoutSec 15 -ErrorAction Stop
            if ($info.ReturnValue -eq 0 -and $info.User) {
                $Owners[$sessionId] = "$($info.Domain)\$($info.User)"
            } else {
                throw ("GetOwner вернул код " + $info.ReturnValue)
            }
        } catch {
            Write-Output ("WARN_OWNER|" + $process.ProcessId + "|" + (Format-Reason $_.Exception.Message))
            $getOwnerUnavailable = $true
        }
    }
}

function Get-OwnerLabel([hashtable]$Owners, $Process) {
    $sessionId = [int]$Process.SessionId
    if ($Owners.ContainsKey($sessionId)) {
        return (Format-Reason ([string]$Owners[$sessionId]))
    }
    return "неизвестно"
}

function Get-TaskkillPath($CimSession) {
    $os = Get-CimInstance -CimSession $CimSession -ClassName Win32_OperatingSystem -Property WindowsDirectory -OperationTimeoutSec 20
    $windowsDirectory = ([string]$os.WindowsDirectory).Trim()
    if ([string]::IsNullOrWhiteSpace($windowsDirectory)) {
        throw "не удалось определить каталог Windows"
    }
    return ($windowsDirectory.TrimEnd([char]'\') + "\System32\taskkill.exe")
}

# Запускает команду на сервере и ждёт, пока она завершится. Create возвращается
# сразу после старта: без ожидания результат проверялся бы раньше, чем taskkill
# успел отработать (на загруженном сервере он перебирает процессы секундами).
function Invoke-RemoteCommandAndWait($CimSession, [string]$CommandLine, [string]$ImageName, [int]$TimeoutMs) {
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $created = Invoke-CimMethod -CimSession $CimSession -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $CommandLine } -OperationTimeoutSec 20
    $code = [uint32]$created.ReturnValue
    if ($code -ne 0) {
        return [PSCustomObject]@{ Started = $false; Finished = $false; ElapsedMs = $timer.ElapsedMilliseconds; ErrorText = ("запуск не выполнен: " + (Get-Win32ProcessCodeText $code)) }
    }
    $filter = "ProcessId=" + [uint32]$created.ProcessId + " and Name='" + $ImageName + "'"
    $finished = $false
    while ($true) {
        $alive = @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter $filter -Property ProcessId -OperationTimeoutSec 15)
        if ($alive.Count -eq 0) {
            $finished = $true
            break
        }
        if ($timer.ElapsedMilliseconds -ge $TimeoutMs) {
            break
        }
        Start-Sleep -Milliseconds 200
    }
    return [PSCustomObject]@{ Started = $true; Finished = $finished; ElapsedMs = $timer.ElapsedMilliseconds; ErrorText = "" }
}

# taskkill /F пакетами по имени образа. Фильтр IMAGENAME — защита: даже если PID
# успел достаться другому процессу, taskkill его не тронет. Пишет STEP_WARN.
function Invoke-TaskkillStep($CimSession, [object[]]$Processes) {
    try {
        $taskkillPath = Get-TaskkillPath $CimSession
    } catch {
        Write-Output ("STEP_WARN|taskkill|" + (Format-Reason $_.Exception.Message))
        return
    }
    foreach ($group in ($Processes | Group-Object Name)) {
        $imageName = [string]$group.Name
        if ($imageName -notmatch '^[A-Za-z0-9._-]+$') {
            Write-Output ("STEP_WARN|taskkill|некорректное имя процесса: " + (Format-Reason $imageName))
            continue
        }
        $groupProcesses = @($group.Group)
        for ($offset = 0; $offset -lt $groupProcesses.Count; $offset += $TaskkillPidsPerCall) {
            $last = [Math]::Min($offset + $TaskkillPidsPerCall, $groupProcesses.Count) - 1
            $pidArguments = ($groupProcesses[$offset..$last] | ForEach-Object { " /PID " + $_.ProcessId }) -join ""
            $commandLine = '"' + $taskkillPath + '" /F /FI "IMAGENAME eq ' + $imageName + '"' + $pidArguments
            try {
                $result = Invoke-RemoteCommandAndWait $CimSession $commandLine "taskkill.exe" $TaskkillTimeoutMs
                if (-not $result.Started) {
                    Write-Output ("STEP_WARN|taskkill|" + $result.ErrorText)
                } elseif (-not $result.Finished) {
                    Write-Output ("STEP_WARN|taskkill|не завершился за " + [int]($TaskkillTimeoutMs / 1000) + " с")
                }
            } catch {
                Write-Output ("STEP_WARN|taskkill|" + (Format-Reason $_.Exception.Message))
            }
        }
    }
}

# Win32_Process.Terminate: синхронный вызов с кодом ответа — точная причина,
# если процесс не завершается. Пустая строка — запрос принят.
function Invoke-CimTerminate($Process) {
    try {
        $result = Invoke-CimMethod -InputObject $Process -MethodName Terminate -Arguments @{ Reason = [uint32]0 } -OperationTimeoutSec 15 -ErrorAction Stop
        $code = [uint32]$result.ReturnValue
        if ($code -eq 0) {
            return ""
        }
        return (Get-Win32ProcessCodeText $code)
    } catch {
        $exception = $_.Exception
        # Процесс завершился между проверками — это не ошибка.
        if ($exception.GetType().FullName -eq "Microsoft.Management.Infrastructure.CimException" -and [string]$exception.NativeErrorCode -eq "NotFound") {
            return ""
        }
        return (Format-Reason $exception.Message)
    }
}

# Ждёт выхода процессов и возвращает те, что живы к истечению срока.
# TimeoutMs = 0 — одна проверка без ожидания.
function Wait-ProcessesGone($CimSession, [object[]]$Processes, [int]$TimeoutMs) {
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $pending = @($Processes)
    while ($pending.Count -gt 0) {
        $filter = ($pending | ForEach-Object { "ProcessId=" + $_.ProcessId }) -join " or "
        $alive = @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter $filter -Property ProcessId,Name,CreationDate -OperationTimeoutSec 15)
        $aliveKeys = @{}
        foreach ($process in $alive) {
            $aliveKeys[(Get-ProcessKey $process)] = $true
        }
        $pending = @($pending | Where-Object { $aliveKeys.ContainsKey((Get-ProcessKey $_)) })
        if ($pending.Count -eq 0 -or $timer.ElapsedMilliseconds -ge $TimeoutMs) {
            break
        }
        Start-Sleep -Milliseconds 250
    }
    return $pending
}

function Get-KeySet([object[]]$Processes) {
    $keys = @{}
    foreach ($process in $Processes) {
        $keys[(Get-ProcessKey $process)] = $true
    }
    return $keys
}

# Протокол вывода:
#   VARIANT|имя|количество          другая 1С, не входит в список завершения
#   GONE|PID|причина                выбранный PID уже не выполняется
#   PROBE|taskkill|ok/slow/fail|мс|подробности   (только тест)
#   PLAN|PID|сеанс|имя|владелец     был бы завершён (только тест)
#   STEP_WARN|шаг|причина           шаг не сработал, используется следующий
#   KILLED|PID|сеанс|имя|владелец|способ
#   KILL_FAIL|PID|владелец|причина
#   KILL_PENDING|PID|владелец|причина
function Invoke-TerminateAction($CimSession, [string]$ServerName, [string[]]$Names, [int[]]$RequestedIds, [bool]$IsDryRun) {
    Write-Output "STAGE|Поиск процессов"
    $queryTimer = [Diagnostics.Stopwatch]::StartNew()
    try {
        $targets = @(Get-TargetProcesses $CimSession $Names $RequestedIds)
    } catch {
        throw ("Поиск процессов не завершён: " + $_.Exception.Message)
    }
    Write-Timing "Поиск процессов" $queryTimer

    foreach ($id in $RequestedIds) {
        if (-not ($targets | Where-Object { [int]$_.ProcessId -eq $id })) {
            Write-Output ("GONE|" + $id + "|процесс уже не выполняется или это не " + ($Names -join ", "))
        }
    }

    if ($RequestedIds.Count -eq 0) {
        try {
            $variants = Get-OtherOneCVariants $CimSession $Names
            foreach ($name in $variants.Keys) {
                Write-Output ("VARIANT|" + $name + "|" + $variants[$name])
            }
        } catch {
            Write-Output ("WARN_VARIANT|" + (Format-Reason $_.Exception.Message))
        }
    }

    if ($targets.Count -eq 0) {
        if ($RequestedIds.Count -eq 0) {
            Write-Output "NONE_FOUND"
        }
        return
    }

    $owners = @{}
    Resolve-Owners $ServerName $targets $owners

    if ($IsDryRun) {
        # Без завершения проверяем весь путь запуска: taskkill с фильтром по
        # несуществующему имени стартует на сервере, перебирает процессы и
        # ничего не завершает. Время его работы — оценка шага 1 на этом сервере.
        Write-Output "STAGE|Тест: пробный запуск taskkill без завершения"
        try {
            $taskkillPath = Get-TaskkillPath $CimSession
            $probe = Invoke-RemoteCommandAndWait $CimSession ('"' + $taskkillPath + '" /FI "IMAGENAME eq fsadmin-dryrun-probe.exe"') "taskkill.exe" $TaskkillTimeoutMs
            if (-not $probe.Started) {
                Write-Output ("PROBE|taskkill|fail|" + $probe.ElapsedMs + "|" + $probe.ErrorText)
            } elseif (-not $probe.Finished) {
                Write-Output ("PROBE|taskkill|slow|" + $probe.ElapsedMs + "|не завершился за " + [int]($TaskkillTimeoutMs / 1000) + " с")
            } else {
                Write-Output ("PROBE|taskkill|ok|" + $probe.ElapsedMs + "|")
            }
        } catch {
            Write-Output ("PROBE|taskkill|fail|0|" + (Format-Reason $_.Exception.Message))
        }

        $stillRunning = Get-KeySet @(Wait-ProcessesGone $CimSession $targets 0)
        foreach ($process in $targets) {
            if ($stillRunning.ContainsKey((Get-ProcessKey $process))) {
                Write-Output ("PLAN|" + $process.ProcessId + "|" + $process.SessionId + "|" + $process.Name + "|" + (Get-OwnerLabel $owners $process))
            } else {
                Write-Output ("GONE|" + $process.ProcessId + "|закрылся сам во время проверки")
            }
        }
        return
    }

    $actionTimer = [Diagnostics.Stopwatch]::StartNew()
    $method = @{}
    $failure = @{}

    Write-Output "STAGE|Завершение: taskkill /F"
    Invoke-TaskkillStep $CimSession $targets
    $survivors = @(Wait-ProcessesGone $CimSession $targets $AfterTaskkillGraceMs)
    $survivorKeys = Get-KeySet $survivors
    foreach ($process in $targets) {
        $key = Get-ProcessKey $process
        if (-not $survivorKeys.ContainsKey($key)) {
            $method[$key] = "taskkill"
        }
    }

    if ($survivors.Count -gt 0) {
        Write-Output "STAGE|Завершение оставшихся: Win32_Process.Terminate"
        foreach ($process in $survivors) {
            $reason = Invoke-CimTerminate $process
            if ($reason) {
                $failure[(Get-ProcessKey $process)] = $reason
            }
        }
        Write-Output "STAGE|Ожидание выхода процессов"
        $remainingKeys = Get-KeySet @(Wait-ProcessesGone $CimSession $survivors $ExitWaitTimeoutMs)
        foreach ($process in $survivors) {
            $key = Get-ProcessKey $process
            if (-not $remainingKeys.ContainsKey($key)) {
                $method[$key] = "Terminate"
                $failure.Remove($key)
            }
        }
    }
    Write-Timing "Завершение процессов" $actionTimer

    foreach ($process in $targets) {
        $key = Get-ProcessKey $process
        $owner = Get-OwnerLabel $owners $process
        if ($method.ContainsKey($key)) {
            Write-Output ("KILLED|" + $process.ProcessId + "|" + $process.SessionId + "|" + $process.Name + "|" + $owner + "|" + $method[$key])
        } elseif ($failure.ContainsKey($key)) {
            Write-Output ("KILL_FAIL|" + $process.ProcessId + "|" + $owner + "|" + $failure[$key])
        } else {
            Write-Output ("KILL_PENDING|" + $process.ProcessId + "|" + $owner + "|завершение принято, но процесс не вышел за " + [int]($ExitWaitTimeoutMs / 1000) + " с")
        }
    }
}

# --- Тест оповещения -------------------------------------------------------
# Кто получил бы сообщение и запускается ли msg.exe. Ничего не отправляет:
# пробный запуск — "msg.exe /?" (только справка).
# Ответ: NOTIFY_RECIPIENT|сеанс|пользователь|состояние, NOTIFY_WARN|текст,
# NOTIFY_PROBE|ok/slow/fail|мс|подробности, NOTIFY_DRY|ok.

# Сеансы по quser: пользователь, номер, состояние (Active/Disc, Активно/Диск).
function Get-QuserSessions([string]$ServerName) {
    $quserPath = Join-Path $env:SystemRoot "System32\quser.exe"
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
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $startInfo
    try {
        if (-not $process.Start()) {
            throw "не удалось запустить quser.exe"
        }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(5000)) {
            $process.Kill()
            throw "quser не ответил за 5 секунд"
        }
        $stdout = $stdoutTask.Result
        if ($process.ExitCode -ne 0 -and [string]::IsNullOrWhiteSpace($stdout)) {
            throw ("quser: " + ($stderrTask.Result).Trim())
        }
    } finally {
        $process.Dispose()
    }
    $sessions = @()
    foreach ($line in [regex]::Split($stdout, "\r?\n")) {
        $tokens = @(($line.Trim() -replace '^>', '') -split '\s+' | Where-Object { $_ -ne "" })
        for ($index = 1; $index -lt $tokens.Count; $index++) {
            $sessionId = 0
            if ([int]::TryParse($tokens[$index], [ref]$sessionId)) {
                $state = ""
                if ($index + 1 -lt $tokens.Count) {
                    $state = $tokens[$index + 1]
                }
                $sessions += [PSCustomObject]@{ Id = $sessionId; User = $tokens[0].TrimStart([char]'>'); State = $state }
                break
            }
        }
    }
    return $sessions
}

# Запасной путь, если quser недоступен: владельцы процессов пользовательских
# сеансов через CIM (с учётными данными из программы). Состояние неизвестно.
function Get-CimSessions($CimSession) {
    $serviceAccounts = @("SYSTEM", "LOCAL SERVICE", "NETWORK SERVICE")
    $byId = @{}
    $processes = @(Get-CimInstance -CimSession $CimSession -ClassName Win32_Process -Filter "SessionId > 0" -Property Handle,ProcessId,SessionId,Name -OperationTimeoutSec 30)
    foreach ($process in $processes) {
        $id = [int]$process.SessionId
        if ($byId.ContainsKey($id) -or @("csrss.exe", "winlogon.exe", "dwm.exe", "fontdrvhost.exe", "LogonUI.exe") -contains $process.Name) {
            continue
        }
        try {
            $owner = Invoke-CimMethod -InputObject $process -MethodName GetOwner -OperationTimeoutSec 10
            if ([uint32]$owner.ReturnValue -eq 0 -and -not [string]::IsNullOrWhiteSpace($owner.User) -and $serviceAccounts -notcontains ([string]$owner.User).ToUpper()) {
                $byId[$id] = [PSCustomObject]@{ Id = $id; User = ([string]$owner.Domain + "\" + [string]$owner.User).TrimStart([char]'\'); State = "" }
            }
        } catch {}
    }
    return @($byId.Values | Sort-Object Id)
}

function Invoke-NotifyDryRun($CimSession, [string]$ServerName, [string]$MsgExe, [string]$SafeMessage) {
    Write-Output "STAGE|Получатели оповещения"
    $sessions = @()
    try {
        $sessions = @(Get-QuserSessions $ServerName)
    } catch {
        Write-Output ("NOTIFY_WARN|список сеансов через quser не получен (" + (Format-Reason $_.Exception.Message) + "), получатели определены по процессам")
        try {
            $sessions = @(Get-CimSessions $CimSession)
        } catch {
            Write-Output ("NOTIFY_WARN|получателей определить не удалось: " + (Format-Reason $_.Exception.Message))
        }
    }
    foreach ($item in $sessions) {
        Write-Output ("NOTIFY_RECIPIENT|" + $item.Id + "|" + (Format-Reason $item.User) + "|" + (Format-Reason $item.State))
    }

    Write-Output "STAGE|Пробный запуск msg.exe"
    try {
        $probe = Invoke-RemoteCommandAndWait $CimSession ('"' + $MsgExe + '" /?') "msg.exe" 10000
        if (-not $probe.Started) {
            Write-Output ("NOTIFY_PROBE|fail|" + $probe.ElapsedMs + "|" + $probe.ErrorText)
        } elseif (-not $probe.Finished) {
            Write-Output ("NOTIFY_PROBE|slow|" + $probe.ElapsedMs + "|msg.exe запустился, но не завершился за 10 с")
        } else {
            Write-Output ("NOTIFY_PROBE|ok|" + $probe.ElapsedMs + "|")
        }
    } catch {
        Write-Output ("NOTIFY_PROBE|fail|0|" + (Format-Reason $_.Exception.Message))
    }
    Write-Output "NOTIFY_DRY|ok"
}

# --- Тест подключения ------------------------------------------------------
# Проверяет по шагам то, без чего не работает ни одна операция, и ничего на
# сервере не меняет (кроме пробного запуска "cmd.exe /c exit 0"). Ответ —
# строки DIAG|шаг|ok/fail/skip|мс|подробности; причины разбирает приложение.

function Write-Diag([string]$Step, [string]$Status, [long]$Ms, [string]$Detail) {
    Write-Output ("DIAG|" + $Step + "|" + $Status + "|" + $Ms + "|" + (Format-Reason $Detail))
}

# Текст ошибки с кодом HRESULT: по коду приложение отличает «доступ запрещён»
# от «сервер RPC недоступен» независимо от языка Windows.
function Get-DiagErrorText($ErrorRecord) {
    $exception = $ErrorRecord.Exception
    while ($null -ne $exception.InnerException -and -not ($exception -is [Microsoft.Management.Infrastructure.CimException])) {
        $exception = $exception.InnerException
    }
    $code = $null
    try {
        $property = $exception.ErrorData.CimInstanceProperties["error_Code"]
        if ($null -ne $property -and $null -ne $property.Value) {
            $code = [uint32]$property.Value
        }
    } catch {}
    if ($null -eq $code) {
        try { $code = [uint32]([int64]$exception.HResult -band 0xFFFFFFFF) } catch {}
    }
    $text = ([string]$exception.Message).Trim()
    if ($null -ne $code -and $code -ne 0) {
        $text = $text + " (0x" + $code.ToString("X8") + ")"
    }
    return $text
}

function Invoke-DiagnoseAction($Credential) {
    $timer = [Diagnostics.Stopwatch]::StartNew()
    Write-Output "STAGE|Проверка порта 135 (RPC)"
    $portOk = $false
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $async = $client.BeginConnect($ComputerName, 135, $null, $null)
        if ($async.AsyncWaitHandle.WaitOne(3000) -and $client.Connected) {
            $client.EndConnect($async)
            $portOk = $true
        }
    } catch {
    } finally {
        $client.Close()
    }
    if ($portOk) {
        Write-Diag "port" "ok" $timer.ElapsedMilliseconds ""
    } else {
        Write-Diag "port" "fail" $timer.ElapsedMilliseconds "нет ответа на порт 135 за 3 с"
        return
    }

    $diagSession = $null
    foreach ($variant in @("dcom", "dcom_privacy")) {
        $timer = [Diagnostics.Stopwatch]::StartNew()
        Write-Output ("STAGE|Подключение CIM/DCOM" + $(if ($variant -eq "dcom_privacy") { " с шифрованием" } else { "" }))
        try {
            if ($variant -eq "dcom") {
                $option = New-CimSessionOption -Protocol Dcom
            } else {
                # -PacketPrivacy сам выбирает набор параметров DCOM; вместе с
                # -Protocol его указать нельзя.
                $option = New-CimSessionOption -PacketPrivacy
            }
            $diagSession = New-CimSession -ComputerName $ComputerName -Credential $Credential -SessionOption $option -OperationTimeoutSec 20 -ErrorAction Stop
            Write-Diag $variant "ok" $timer.ElapsedMilliseconds ""
            break
        } catch {
            $errorText = Get-DiagErrorText $_
            Write-Diag $variant "fail" $timer.ElapsedMilliseconds $errorText
            # Неверный пароль, недоступный RPC или тайм-аут шифрование не
            # исправит, а лишняя попытка входа приближает блокировку учётной записи.
            if ($errorText -match "8007052E|800706BA|80070079|800705B4") {
                break
            }
        }
    }
    if ($null -eq $diagSession) {
        return
    }

    try {
        $timer = [Diagnostics.Stopwatch]::StartNew()
        Write-Output "STAGE|Чтение WMI"
        try {
            $os = Get-CimInstance -CimSession $diagSession -ClassName Win32_OperatingSystem -Property CSName,Caption -OperationTimeoutSec 20 -ErrorAction Stop
            Write-Diag "wmi" "ok" $timer.ElapsedMilliseconds ((([string]$os.CSName) + ", " + ([string]$os.Caption)).Trim(", "))
        } catch {
            Write-Diag "wmi" "fail" $timer.ElapsedMilliseconds (Get-DiagErrorText $_)
        }

        $timer = [Diagnostics.Stopwatch]::StartNew()
        Write-Output "STAGE|Поиск процессов 1С"
        try {
            $found = @(Get-CimInstance -CimSession $diagSession -ClassName Win32_Process -Filter "Name LIKE '1cv%'" -Property Name -OperationTimeoutSec 30 -ErrorAction Stop)
            $summary = @($found | Group-Object Name | Sort-Object Name | ForEach-Object { $_.Name + " ×" + $_.Count }) -join ", "
            Write-Diag "processes" "ok" $timer.ElapsedMilliseconds $summary
        } catch {
            Write-Diag "processes" "fail" $timer.ElapsedMilliseconds (Get-DiagErrorText $_)
        }

        $timer = [Diagnostics.Stopwatch]::StartNew()
        Write-Output "STAGE|Пробный запуск команды"
        try {
            $result = Invoke-CimMethod -CimSession $diagSession -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = "cmd.exe /c exit 0" } -OperationTimeoutSec 20 -ErrorAction Stop
            if ([uint32]$result.ReturnValue -eq 0) {
                Write-Diag "launch" "ok" $timer.ElapsedMilliseconds ""
            } else {
                Write-Diag "launch" "fail" $timer.ElapsedMilliseconds (Get-Win32ProcessCodeText ([uint32]$result.ReturnValue))
            }
        } catch {
            Write-Diag "launch" "fail" $timer.ElapsedMilliseconds (Get-DiagErrorText $_)
        }
    } finally {
        Remove-CimSession -CimSession $diagSession -ErrorAction SilentlyContinue
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

    if ($Action -eq "diagnose") {
        Invoke-DiagnoseAction $credential
        exit 0
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

            if ($DryRun) {
                # Тест: тот же путь до этого места, дальше — проверка вместо
                # рассылки. Ничего не отправляется.
                Invoke-NotifyDryRun $session $ComputerName $msgExe $safeMessage
                Write-Timing "Тест оповещения" $notifyTimer
                exit 0
            }

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

    if ($Action -eq "terminate") {
        $requestedIds = @()
        if (-not [string]::IsNullOrWhiteSpace($ProcessIds)) {
            foreach ($idText in $ProcessIds.Split(",")) {
                $pidValue = 0
                if (-not [int]::TryParse($idText.Trim(), [ref]$pidValue) -or $pidValue -le 0) {
                    Write-Output ("KILL_FAIL|" + (Format-Reason $idText) + "|неизвестно|некорректный PID")
                    continue
                }
                $requestedIds += $pidValue
            }
            if ($requestedIds.Count -eq 0) {
                exit 0
            }
        }
        Invoke-TerminateAction $session $ComputerName $allowedNames $requestedIds ([bool]$DryRun)
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
