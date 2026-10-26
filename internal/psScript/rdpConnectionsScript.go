package psscript

// RDPHistoryDays — за сколько дней «Сведения о системе» показывают
// подключения к удалённому рабочему столу.
const RDPHistoryDays = 14

// rdpConnectionsFunction определяет Get-RdpConnections: кто и откуда
// подключался к целевому компьютеру по RDP. Источник — журналы службы
// удалённых рабочих столов, они ведутся без настройки аудита:
//
//   - LocalSessionManager/Operational, события 21 (вход в сеанс) и 25
//     (переподключение): учётная запись DOMAIN\user и адрес клиента;
//   - RemoteConnectionManager/Operational, событие 1149 (учётные данные
//     приняты) — запасной источник, если первый журнал пуст или выключен.
//
// Записи сводятся в пары «учётная запись — адрес клиента» со временем
// последнего подключения и их числом: администратору нужен ответ «кто
// откуда ходит», а не сотни одинаковых строк. Имя клиентского ПК берётся
// обратным DNS-запросом с общим бюджетом времени, чтобы недоступный DNS не
// задерживал сбор сведений.
//
// Функция пользуется Limit-Text, Format-LocalTime и Write-InfoWarning из
// SystemInfoScript; в PowerShell они разрешаются в момент вызова.
const rdpConnectionsFunction = `
function Read-RdpEvents([string]$LogName, [int[]]$Ids, [DateTime]$Since, [string]$Stage) {
    try {
        return @(Get-WinEvent -FilterHashtable @{ LogName = $LogName; Id = $Ids; StartTime = $Since } -MaxEvents 1000 -ErrorAction Stop)
    } catch {
        # «Событий нет» — нормальный ответ, а не сбой.
        if ([string]$_.FullyQualifiedErrorId -notlike "NoMatchingEventsFound*") {
            Write-InfoWarning $Stage $_.Exception.Message
        }
        return @()
    }
}

function Get-RdpConnections([int]$Days) {
    $since = (Get-Date).AddDays(-$Days)
    $records = New-Object Collections.ArrayList

    foreach ($event in (Read-RdpEvents "Microsoft-Windows-TerminalServices-LocalSessionManager/Operational" @(21, 25) $since "Журнал входов RDP")) {
        try {
            $data = ([xml]$event.ToXml()).Event.UserData.EventXML
        } catch { continue }
        $address = Limit-Text $data.Address
        # LOCAL — вход за консолью самого компьютера, не удалённое подключение.
        if ($address -eq "" -or $address -eq "LOCAL") { continue }
        [void]$records.Add([PSCustomObject]@{ Time = $event.TimeCreated; User = Limit-Text $data.User; Address = $address })
    }

    if ($records.Count -eq 0) {
        foreach ($event in (Read-RdpEvents "Microsoft-Windows-TerminalServices-RemoteConnectionManager/Operational" @(1149) $since "Журнал проверки подлинности RDP")) {
            try {
                $data = ([xml]$event.ToXml()).Event.UserData.EventXML
            } catch { continue }
            $user = Limit-Text $data.Param1
            $userDomain = Limit-Text $data.Param2
            if ($userDomain -ne "" -and $user -ne "") { $user = $userDomain + "\" + $user }
            $address = Limit-Text $data.Param3
            if ($address -eq "") { continue }
            [void]$records.Add([PSCustomObject]@{ Time = $event.TimeCreated; User = $user; Address = $address })
        }
    }

    if ($records.Count -eq 0) {
        return @("за " + $Days + " дн. подключений не было")
    }

    $pairs = @($records | Group-Object { ($_.User + "|" + $_.Address).ToLower() } | ForEach-Object {
        $latest = @($_.Group | Sort-Object Time -Descending)[0]
        [PSCustomObject]@{ User = $latest.User; Address = $latest.Address; Last = $latest.Time; Count = $_.Count }
    } | Sort-Object Last -Descending | Select-Object -First 30)

    $hostNames = @{}
    $dnsBudget = [Diagnostics.Stopwatch]::StartNew()
    foreach ($pair in $pairs) {
        $address = $pair.Address
        if ($hostNames.ContainsKey($address)) { continue }
        $hostNames[$address] = ""
        if ($dnsBudget.Elapsed.TotalSeconds -ge 10) { continue }
        $ip = $null
        if (-not [Net.IPAddress]::TryParse($address, [ref]$ip)) { continue }
        try {
            $lookup = [Net.Dns]::GetHostEntryAsync($ip)
            if ($lookup.Wait(1500)) {
                $name = Limit-Text $lookup.Result.HostName
                if ($name -ne "" -and $name -ne $address) {
                    $hostNames[$address] = $name.Split(".")[0].ToUpper()
                }
            }
        } catch {}
    }

    $lines = @()
    foreach ($pair in $pairs) {
        $user = $pair.User
        if ($user -eq "") { $user = "учётная запись не указана" }
        $from = $pair.Address
        if ($hostNames[$pair.Address] -ne "") { $from = $from + " (" + $hostNames[$pair.Address] + ")" }
        $lines += $user + " с " + $from + " — последнее " + (Format-LocalTime $pair.Last) + ", подключений: " + $pair.Count
    }
    return $lines
}
`
