param(
    # Поставить недостающие Git, Go и Node.js через winget.
    [switch]$Install,
    # Не запускать тесты (например, если PowerShell/WMI на этом ПК ограничен).
    [switch]$SkipTests,
    # Не собирать exe в конце.
    [switch]$SkipBuild
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wailsVersion = "v2.16.0"

function Write-Step([string]$text) { Write-Host "`n== $text" -ForegroundColor Cyan }

function Update-SessionPath {
    $machine = [Environment]::GetEnvironmentVariable("Path", "Machine")
    $user = [Environment]::GetEnvironmentVariable("Path", "User")
    $env:Path = "$machine;$user"
}

function Test-Tool([string]$name) { [bool](Get-Command $name -ErrorAction SilentlyContinue) }

Write-Step "Проверка инструментов"
$tools = @(
    @{ Cmd = "git";  Id = "Git.Git";           Name = "Git" },
    @{ Cmd = "go";   Id = "GoLang.Go";         Name = "Go" },
    @{ Cmd = "node"; Id = "OpenJS.NodeJS.LTS"; Name = "Node.js" }
)
$missing = @($tools | Where-Object { -not (Test-Tool $_.Cmd) })
if ($missing.Count -gt 0) {
    if (-not $Install) {
        $ids = ($missing | ForEach-Object { $_.Id }) -join " "
        throw ("Не найдено: " + (($missing | ForEach-Object { $_.Name }) -join ", ") +
            "`nУстановите: winget install $ids`nили запустите: .\tools\setup-dev.ps1 -Install")
    }
    foreach ($t in $missing) {
        Write-Host "Устанавливаю $($t.Name)…"
        winget install --id $t.Id -e --accept-package-agreements --accept-source-agreements
    }
    Update-SessionPath
    $still = @($tools | Where-Object { -not (Test-Tool $_.Cmd) })
    if ($still.Count -gt 0) {
        throw "После установки не видно: $(($still | ForEach-Object { $_.Name }) -join ', '). Откройте новое окно PowerShell и запустите скрипт снова."
    }
}
Write-Host (git --version)
Write-Host (go version)
Write-Host "node $(node -v), npm $(npm -v)"

Write-Step "Wails CLI $wailsVersion"
$gobin = Join-Path (go env GOPATH) "bin"
if ($env:Path -notlike "*$gobin*") { $env:Path = "$gobin;$env:Path" }
$wails = Join-Path $gobin "wails.exe"
$haveWails = (Test-Path $wails) -and ((& $wails version 2>$null | Select-Object -First 1) -eq $wailsVersion)
if (-not $haveWails) {
    go install "github.com/wailsapp/wails/v2/cmd/wails@$wailsVersion"
    if ($LASTEXITCODE -ne 0) { throw "go install wails не удался" }
}
Write-Host (& $wails version | Select-Object -First 1)
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$gobin*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$gobin", "User")
    Write-Host "Добавил $gobin в PATH пользователя (в новых окнах wails будет виден сразу)."
}

Push-Location $root
try {
    Write-Step "Модули Go"
    go mod download
    if ($LASTEXITCODE -ne 0) { throw "go mod download не удался" }
    go build ./...
    if ($LASTEXITCODE -ne 0) { throw "go build не удался" }

    Write-Step "Модули npm"
    Push-Location frontend
    try {
        npm ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw "npm ci не удался" }
    } finally { Pop-Location }

    if (-not $SkipTests) {
        Write-Step "Тесты Go"
        go test ./...
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Часть тестов Go не прошла. Тесты internal/psScript зависят от PowerShell/WMI на этом ПК — посмотрите вывод выше."
        }
        Write-Step "Тесты интерфейса"
        Push-Location frontend
        try {
            npm test
            if ($LASTEXITCODE -ne 0) { Write-Warning "Тесты интерфейса не прошли." }
        } finally { Pop-Location }
    }

    if (-not $SkipBuild) {
        Write-Step "Сборка"
        & $wails build -s -skipbindings -o filesearch-admin-dev.exe
        if ($LASTEXITCODE -ne 0) { throw "wails build не удался" }
        Write-Host "Готово: $(Join-Path $root 'build\bin\filesearch-admin-dev.exe')" -ForegroundColor Green
    }
} finally { Pop-Location }

Write-Host "`nВсё готово. Запуск с живой перезагрузкой: wails dev" -ForegroundColor Green
