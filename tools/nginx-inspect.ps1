# Только чтение: ничего не меняет. Запускать на компьютере, где работает Nginx,
# в PowerShell от имени администратора:
#
#   powershell -ExecutionPolicy Bypass -File .\nginx-inspect.ps1
#
# Показывает, где установлен Nginx, как он запускается (служба, задание
# планировщика, вручную), какие порты слушает, какие сайты описаны в
# конфигурации (полный текст через nginx -T, со всеми include) и какие порты
# открыты в брандмауэре. Отчёт дублируется в файл на рабочем столе.

$ErrorActionPreference = 'Continue'
$report = Join-Path ([Environment]::GetFolderPath('Desktop')) ("nginx-report-" + (Get-Date -Format 'yyyyMMdd-HHmmss') + ".txt")
Start-Transcript -Path $report -Force | Out-Null

function Write-Head($text) { Write-Host "`n=== $text ===" -ForegroundColor Cyan }

Write-Head 'Компьютер'
Write-Host "Имя: $env:COMPUTERNAME"
Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Where-Object { $_.IPAddress -ne '127.0.0.1' } |
    ForEach-Object { Write-Host "IP:  $($_.IPAddress) ($($_.InterfaceAlias))" }

# 1. Процессы nginx: путь и командная строка (-p — папка установки, -c — конфиг)
Write-Head 'Процессы nginx'
$processes = @(Get-CimInstance Win32_Process -Filter "Name='nginx.exe'" -ErrorAction SilentlyContinue)
if ($processes.Count -eq 0) {
    Write-Host 'nginx.exe не запущен.' -ForegroundColor Yellow
} else {
    foreach ($process in $processes) {
        Write-Host ("PID {0}, родитель {1}: {2}" -f $process.ProcessId, $process.ParentProcessId, $process.CommandLine)
    }
}

# Папки установки: по запущенным процессам, иначе — поиск nginx.exe в типичных местах
$exePaths = @($processes | ForEach-Object { $_.ExecutablePath } | Where-Object { $_ } | Sort-Object -Unique)
if ($exePaths.Count -eq 0) {
    Write-Host 'Ищу nginx.exe на дисках C:\ (до 4 уровней вглубь)...'
    $exePaths = @(Get-ChildItem -Path 'C:\' -Filter 'nginx.exe' -Recurse -Depth 4 -File -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty FullName)
}
Write-Head 'Установки nginx'
if ($exePaths.Count -eq 0) { Write-Host 'nginx.exe не найден.' -ForegroundColor Yellow }
foreach ($exe in $exePaths) {
    $prefix = Split-Path -Parent $exe
    Write-Host "nginx.exe: $exe"
    Write-Host "Папка (prefix): $prefix"
    Push-Location $prefix
    try {
        & $exe -v 2>&1 | ForEach-Object { Write-Host "  $_" }
    } finally { Pop-Location }
}

# 2. Как запускается: служба, задание планировщика или вручную
Write-Head 'Запуск: службы'
$services = @(Get-CimInstance Win32_Service -ErrorAction SilentlyContinue |
    Where-Object { $_.PathName -match 'nginx|nssm|winsw' -or $_.Name -match 'nginx' -or $_.DisplayName -match 'nginx' })
if ($services.Count -eq 0) { Write-Host 'Служб, связанных с nginx, нет — вероятно, запускается вручную или заданием.' }
foreach ($service in $services) {
    Write-Host ("{0} ({1}) — {2}, запуск: {3}" -f $service.Name, $service.DisplayName, $service.State, $service.StartMode)
    Write-Host "  команда: $($service.PathName)"
    Write-Host "  учётная запись: $($service.StartName)"
}

Write-Head 'Запуск: задания планировщика'
$tasks = @(Get-ScheduledTask -ErrorAction SilentlyContinue | Where-Object {
    ($_.Actions | ForEach-Object { "$($_.Execute) $($_.Arguments) $($_.WorkingDirectory)" }) -match 'nginx'
})
if ($tasks.Count -eq 0) { Write-Host 'Заданий с nginx нет.' }
foreach ($task in $tasks) {
    Write-Host ("{0}{1} — {2}" -f $task.TaskPath, $task.TaskName, $task.State)
    foreach ($action in $task.Actions) { Write-Host "  $($action.Execute) $($action.Arguments) (папка: $($action.WorkingDirectory))" }
}

Write-Head 'Запуск: автозагрузка'
Get-CimInstance Win32_StartupCommand -ErrorAction SilentlyContinue |
    Where-Object { $_.Command -match 'nginx' } |
    ForEach-Object { Write-Host "$($_.Name): $($_.Command) ($($_.Location))" }

# 3. Какие порты слушает компьютер и кто
Write-Head 'Слушающие порты (кроме системных RPC)'
Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
    Where-Object { $_.LocalPort -lt 49152 } |
    Sort-Object LocalPort, LocalAddress -Unique |
    ForEach-Object {
        $owner = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
        $name = $owner.Name
        if ($_.OwningProcess -eq 4) {
            # PID 4 — ядро: SMB на 139/445, остальное — HTTP.sys (IIS, WinRM и т.п.)
            $name = if ($_.LocalPort -in 139, 445) { 'System (общий доступ к файлам, SMB)' } else { 'System (HTTP.sys: IIS, WinRM и т.п.)' }
        }
        Write-Host ("{0,-6} {1,-16} {2}" -f $_.LocalPort, $_.LocalAddress, $name)
    }

# 4. Полная конфигурация: nginx -T печатает итоговый конфиг со всеми include
foreach ($exe in $exePaths) {
    $prefix = Split-Path -Parent $exe
    Write-Head "Конфигурация ($prefix)"
    Push-Location $prefix
    try {
        $dump = @(& $exe -T -p "$prefix\" 2>&1 | ForEach-Object { "$_" })
    } finally { Pop-Location }

    Write-Host '--- Сводка: server { listen / server_name / root / location } ---' -ForegroundColor Green
    $dump | Where-Object { $_ -match '^\s*(# configuration file|server\s*\{|listen\s|server_name\s|root\s|alias\s|location\s|include\s|proxy_pass\s)' } |
        ForEach-Object { Write-Host "  $($_.Trim())" }

    Write-Host '--- Полный текст (nginx -T) ---' -ForegroundColor Green
    $dump | ForEach-Object { Write-Host $_ }
}

# 5. Брандмауэр: входящие разрешения по портам
Write-Head 'Брандмауэр: входящие правила с портами'
Get-NetFirewallRule -Direction Inbound -Enabled True -Action Allow -ErrorAction SilentlyContinue |
    ForEach-Object {
        $ports = ($_ | Get-NetFirewallPortFilter -ErrorAction SilentlyContinue).LocalPort
        if ($ports -and $ports -ne 'Any' -and ($_.DisplayName -match 'nginx|http|web|1212|1213|8080' -or ($ports -join ',') -match '\b(80|443|8080|1212|1213)\b')) {
            "{0} — порт(ы): {1}" -f $_.DisplayName, ($ports -join ', ')
        }
    } | Sort-Object -Unique | ForEach-Object { Write-Host $_ }

Stop-Transcript | Out-Null
Write-Host "`nОтчёт сохранён: $report" -ForegroundColor Cyan
