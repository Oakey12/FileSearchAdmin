# Запускать НА сервере Nginx (192.168.150.100), PowerShell от администратора.
#
# Скрипт НЕ меняет nginx.conf и не ломает текущие сайты. Он:
#   1) находит nginx.exe, его prefix и nginx.conf;
#   2) показывает текущие include и все listen/root/server_name — чтобы вы
#      видели существующие сайты и их корни;
#   3) создаёт каталог html-filesearch-admin и servers.json (если их нет);
#   4) кладёт готовый блок на порт 1213 в conf\filesearch-admin.conf;
#   5) делает резервную копию nginx.conf;
#   6) печатает ОДИН шаг, который сделаете вы (подключить блок), и команды
#      проверки/перезагрузки.

$ErrorActionPreference = 'Stop'

function Write-Head($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }

# 1. Найти nginx.exe и prefix ------------------------------------------------
$exe = (Get-Process nginx -ErrorAction SilentlyContinue | Select-Object -First 1).Path
if (-not $exe) { $exe = (Get-Command nginx.exe -ErrorAction SilentlyContinue).Source }
if (-not $exe) { $exe = Read-Host 'Nginx не запущен и не в PATH. Укажите полный путь к nginx.exe' }
$prefix = Split-Path -Parent $exe
$confDir = Join-Path $prefix 'conf'
$conf = Join-Path $confDir 'nginx.conf'
if (-not (Test-Path $conf)) { throw "Не найден $conf. Укажите верный путь к nginx и запустите снова." }

Write-Head 'Где Nginx'
Write-Host "nginx.exe : $exe"
Write-Host "prefix    : $prefix"
Write-Host "nginx.conf: $conf"

# 2. Показать текущую структуру ----------------------------------------------
Write-Head 'include в nginx.conf'
$includes = Select-String -Path $conf -Pattern '^\s*include\s' | ForEach-Object { $_.Line.Trim() }
if ($includes) { $includes | ForEach-Object { Write-Host "  $_" } } else { Write-Host '  (нет include — всё в nginx.conf)' }

Write-Head 'Текущие сайты (listen / root / server_name по всем .conf)'
$allConf = Get-ChildItem $confDir -Recurse -Filter *.conf -ErrorAction SilentlyContinue
foreach ($c in $allConf) {
    Select-String -Path $c.FullName -Pattern '^\s*(listen|server_name|root)\s' |
        ForEach-Object { Write-Host ("  {0}: {1}" -f $c.Name, $_.Line.Trim()) }
}

# Проверка, что 1213 ещё не занят в конфиге
if ($allConf -and (Select-String -Path $allConf.FullName -Pattern 'listen\s+1213\b' -Quiet)) {
    Write-Warning 'Порт 1213 уже упоминается в конфиге — проверьте вручную перед продолжением.'
}

# 3. Каталог + servers.json --------------------------------------------------
$html = Join-Path $prefix 'html-filesearch-admin'
New-Item -ItemType Directory -Force $html | Out-Null
$serversJson = Join-Path $html 'servers.json'
if (-not (Test-Path $serversJson)) {
    $sample = @'
{
  "servers": [
    { "name": "Сервер 1С (основной)", "ip": "192.168.20.31" }
  ]
}
'@
    [IO.File]::WriteAllText($serversJson, $sample, (New-Object System.Text.UTF8Encoding $false))
    Write-Head 'servers.json'
    Write-Host "Создан: $serversJson  — впишите свои серверы (имя + IP)."
} else {
    Write-Head 'servers.json'
    Write-Host "Уже есть: $serversJson"
}

# 4. Блок на порт 1213 отдельным файлом --------------------------------------
$blockFile = Join-Path $confDir 'filesearch-admin.conf'
$block = @'
# FileSearchAdmin — обновления и список серверов (порт 1213)
server {
    listen       1213;
    server_name  _;
    charset      utf-8;
    root         html-filesearch-admin;

    location / {
        autoindex off;
        try_files  $uri =404;
    }
}
'@
[IO.File]::WriteAllText($blockFile, $block, (New-Object System.Text.UTF8Encoding $false))
Write-Head 'Блок Nginx для порта 1213'
Write-Host "Создан: $blockFile"

# 5. Резервная копия nginx.conf ----------------------------------------------
$bak = "$conf.bak-$(Get-Date -Format yyyyMMdd-HHmmss)"
Copy-Item $conf $bak
Write-Host "Резервная копия nginx.conf: $bak"

# 6. Что сделать дальше ------------------------------------------------------
Write-Head 'ЧТО СДЕЛАТЬ ВАМ (один шаг)'
$autoPick = $includes | Where-Object { $_ -match '\*\.conf|conf\.d|sites' }
if ($autoPick) {
    Write-Host 'В nginx.conf уже есть include, который может подхватить новый файл:' -ForegroundColor Green
    $autoPick | ForEach-Object { Write-Host "    $_" }
    Write-Host 'Если его маска/каталог не совпадает с conf\filesearch-admin.conf —'
    Write-Host 'перенесите filesearch-admin.conf в нужный каталог или добавьте строку ниже.'
} else {
    Write-Host 'Откройте nginx.conf и ВНУТРИ блока http { ... } добавьте одну строку:' -ForegroundColor Yellow
    Write-Host '    include filesearch-admin.conf;'
    Write-Host "Файл для правки: $conf"
}

Write-Head 'Применить и проверить'
Write-Host "cd `"$prefix`""
Write-Host '.\nginx.exe -t            # должно быть: syntax is ok / test is successful'
Write-Host '.\nginx.exe -s reload     # применить без остановки (старые сайты не тронутся)'
Write-Host ''
Write-Host '# открыть порт 1213 в брандмауэре:'
Write-Host "New-NetFirewallRule -DisplayName 'Nginx FileSearchAdmin 1213' -Direction Inbound -Protocol TCP -LocalPort 1213 -Action Allow"
Write-Host ''
Write-Host '# проверка:'
Write-Host 'Invoke-WebRequest http://192.168.150.100:1213/servers.json -UseBasicParsing'
Write-Host ''
Write-Host 'Если nginx.exe -t выдал ошибку — верните nginx.conf из резервной копии:' -ForegroundColor Yellow
Write-Host "    Copy-Item '$bak' '$conf' -Force"
