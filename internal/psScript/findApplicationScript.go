package psscript

import (
	"encoding/base64"
	"strings"
)

// findApplicationScript ищет приложение на целевом компьютере сразу по трём
// источникам: записи деинсталляции в реестре, запущенные процессы и файлы в
// типичных каталогах установки. Одного реестра мало — программы вроде TrueConf
// ставятся в профиль пользователя, и запись о них лежит в ветке этого
// пользователя, а не в общей.
//
// Запрос подставляется в Base64: любые кавычки и спецсимволы в имени
// программы не должны попадать в текст удалённого сценария.
const findApplicationScript = `
$ErrorActionPreference = "Stop"
$query = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String("__QUERY_B64__")).Trim()
if ($query -eq "") {
    Write-Output "APP_DONE|0"
    return
}

$maxMatches = 40
$appMatches = New-Object Collections.ArrayList
$seen = @{}

# Для реестра имя ищется без расширения: в DisplayName пишут «TrueConf Client»,
# а пользователь вводит «TrueConf.exe».
$nameQuery = $query
if ($nameQuery.ToLower().EndsWith(".exe")) {
    $nameQuery = $nameQuery.Substring(0, $nameQuery.Length - 4)
}
$fileFilter = $query
if (-not $fileFilter.ToLower().EndsWith(".exe")) {
    $fileFilter = $fileFilter + "*.exe"
}

function Limit-Text([object]$Value) {
    if ($null -eq $Value) { return "" }
    $text = ([string]$Value).Trim()
    if ($text.Length -gt 300) { return $text.Substring(0, 300) }
    return $text
}

function Write-FindWarning([string]$Stage, [string]$Message) {
    $record = [PSCustomObject]@{ Stage = Limit-Text $Stage; Message = Limit-Text $Message }
    $json = ConvertTo-Json -InputObject $record -Compress
    Write-Output ("APP_WARNING|" + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json)))
}

function Resolve-AccountName([string]$Sid) {
    try {
        $account = (New-Object Security.Principal.SecurityIdentifier($Sid)).Translate([Security.Principal.NTAccount])
        return Limit-Text $account.Value
    } catch {
        return $Sid
    }
}

function Add-Match([string]$Source, [string]$Owner, [string]$Name, [string]$Version, [string]$Path) {
    if ($appMatches.Count -ge $maxMatches) { return }
    $key = ($Source + "|" + $Owner + "|" + $Name + "|" + $Path).ToLower()
    if ($seen.ContainsKey($key)) { return }
    $seen[$key] = $true
    $record = [PSCustomObject]@{
        Source = Limit-Text $Source
        Owner = Limit-Text $Owner
        Name = Limit-Text $Name
        Version = Limit-Text $Version
        Path = Limit-Text $Path
    }
    $json = ConvertTo-Json -InputObject $record -Compress
    [void]$appMatches.Add($json)
}

# 1. Реестр: общие ветки и ветки всех загруженных пользовательских профилей.
$registryRoots = New-Object Collections.ArrayList
[void]$registryRoots.Add([PSCustomObject]@{ Path = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"; Owner = "Весь компьютер" })
[void]$registryRoots.Add([PSCustomObject]@{ Path = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"; Owner = "Весь компьютер" })
$loadedSids = @{}
try {
    foreach ($hive in @(Get-ChildItem "Registry::HKEY_USERS" -ErrorAction Stop)) {
        $leaf = Split-Path $hive.Name -Leaf
        if ($leaf -match '^S-1-5-21-') {
            $loadedSids[$leaf] = $true
            $owner = Resolve-AccountName $leaf
            [void]$registryRoots.Add([PSCustomObject]@{ Path = "Registry::HKEY_USERS\" + $leaf + "\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"; Owner = $owner })
            [void]$registryRoots.Add([PSCustomObject]@{ Path = "Registry::HKEY_USERS\" + $leaf + "\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"; Owner = $owner })
        }
    }
} catch {
    Write-FindWarning "Ветки пользователей" $_.Exception.Message
}

# Личные установки видны только у тех, кто сейчас в системе: куст профиля
# выгружается при выходе. Молчать об этом нельзя — «не найдено» и «не видно»
# для администратора разные ответы.
try {
    $offline = @(Get-CimInstance -ClassName Win32_UserProfile -ErrorAction Stop |
        Where-Object { -not $_.Special -and $_.SID -match "^S-1-5-21-" -and -not $loadedSids.ContainsKey($_.SID) })
    if ($offline.Count -gt 0) {
        $names = @($offline | ForEach-Object { Resolve-AccountName $_.SID }) -join ", "
        Write-FindWarning "Профили без активного сеанса" ("личные установки не видны у: " + $names)
    }
} catch {
    Write-FindWarning "Win32_UserProfile" $_.Exception.Message
}

foreach ($root in $registryRoots) {
    if (-not (Test-Path $root.Path)) { continue }
    try {
        foreach ($key in @(Get-ChildItem $root.Path -ErrorAction Stop)) {
            try {
                $values = Get-ItemProperty $key.PSPath -ErrorAction Stop
            } catch { continue }
            $displayName = Limit-Text $values.DisplayName
            if ($displayName -eq "") { continue }
            if ($displayName -notlike ("*" + $nameQuery + "*")) { continue }
            Add-Match "реестр" $root.Owner $displayName (Limit-Text $values.DisplayVersion) (Limit-Text $values.InstallLocation)
        }
    } catch {
        Write-FindWarning ("Реестр: " + $root.Path) $_.Exception.Message
    }
}

# 2. Запущенные процессы: приложение может работать прямо сейчас.
try {
    foreach ($process in @(Get-CimInstance -ClassName Win32_Process -Property Name,ExecutablePath,ProcessId,Handle -ErrorAction Stop)) {
        $processName = Limit-Text $process.Name
        if ($processName -eq "") { continue }
        if ($processName -notlike ("*" + $nameQuery + "*")) { continue }
        $processOwner = ""
        try {
            $ownerInfo = Invoke-CimMethod -InputObject $process -MethodName GetOwner -ErrorAction Stop
            if ([uint32]$ownerInfo.ReturnValue -eq 0) {
                $processOwner = Limit-Text (([string]$ownerInfo.Domain) + "\" + ([string]$ownerInfo.User)).Trim("\")
            }
        } catch {}
        Add-Match "запущен" $processOwner $processName "" (Limit-Text $process.ExecutablePath)
    }
} catch {
    Write-FindWarning "Запущенные процессы" $_.Exception.Message
}

# 3. Файлы в типичных каталогах установки. Обход всего диска занимал бы минуты,
# поэтому корни перечислены явно и глубина ограничена.
$searchRoots = New-Object Collections.ArrayList
foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:ProgramData)) {
    if (-not [string]::IsNullOrWhiteSpace($root)) { [void]$searchRoots.Add($root) }
}
try {
    $usersRoot = Join-Path $env:SystemDrive "Users"
    foreach ($userProfile in @(Get-ChildItem $usersRoot -Directory -ErrorAction Stop)) {
        foreach ($sub in @("AppData\Local", "AppData\Roaming")) {
            $path = Join-Path $userProfile.FullName $sub
            if (Test-Path $path) { [void]$searchRoots.Add($path) }
        }
    }
} catch {
    Write-FindWarning "Профили пользователей" $_.Exception.Message
}

foreach ($root in $searchRoots) {
    if ($appMatches.Count -ge $maxMatches) { break }
    try {
        $found = @(Get-ChildItem -LiteralPath $root -Filter $fileFilter -Recurse -Depth 4 -File -ErrorAction SilentlyContinue)
        foreach ($file in $found) {
            $version = ""
            try { $version = Limit-Text $file.VersionInfo.FileVersion } catch {}
            # Владелец определяется разбором пути, а не регулярным выражением:
            # обратные слэши проходят через несколько слоёв экранирования, и
            # шаблон легко испортить незаметно.
            $fileOwner = "Весь компьютер"
            $usersPrefix = (Join-Path $env:SystemDrive "Users") + [IO.Path]::DirectorySeparatorChar
            if ($file.FullName.StartsWith($usersPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                $profileName = $file.FullName.Substring($usersPrefix.Length).Split([IO.Path]::DirectorySeparatorChar)[0]
                if (-not [string]::IsNullOrWhiteSpace($profileName)) {
                    $fileOwner = "профиль " + $profileName
                }
            }
            Add-Match "файл" $fileOwner (Limit-Text $file.Name) $version (Limit-Text $file.FullName)
            if ($appMatches.Count -ge $maxMatches) { break }
        }
    } catch {
        Write-FindWarning ("Поиск файлов: " + $root) $_.Exception.Message
    }
}

foreach ($json in $appMatches) {
    Write-Output ("APP_MATCH|" + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json)))
}
Write-Output ("APP_DONE|" + $appMatches.Count)
`

// FindApplicationScript подставляет искомое имя в сценарий поиска.
func FindApplicationScript(query string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(query)))
	return strings.Replace(findApplicationScript, "__QUERY_B64__", encoded, 1)
}
