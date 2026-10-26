package psscript

import (
	"encoding/base64"
	"strings"
)

// findApplicationScript ищет приложение на целевом компьютере по пяти
// источникам, от самых надёжных к косвенным: записи деинсталляции в реестре,
// службы, запущенные процессы, папки установки и исполняемые файлы. Одного
// реестра мало: программы вроде TrueConf ставятся в профиль пользователя, а
// антивирусы (Dr.Web и т.п.) закрывают свои ключи самозащитой и называют
// процессы иначе, чем продукт (dwservice.exe, dwengine.exe).
//
// Имена сравниваются без регистра, пробелов и знаков препинания: «dr.web»,
// «DrWeb» и «Dr.Web Security Space» должны находить друг друга.
//
// Запрос подставляется в Base64: любые кавычки и спецсимволы в имени
// программы не должны попадать в текст удалённого сценария.
const findApplicationScript = findScriptPrelude + findMatchFunctions + findScriptBody

const findScriptPrelude = `
$ErrorActionPreference = "Stop"
$query = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String("__QUERY_B64__")).Trim()
if ($query -eq "") {
    Write-Output "APP_DONE|0"
    return
}

$maxMatches = 60
$appMatches = New-Object Collections.ArrayList
$seen = @{}

# Имя ищется без расширения: в DisplayName пишут «TrueConf Client», а
# пользователь вводит «TrueConf.exe».
$nameQuery = $query
if ($nameQuery.ToLower().EndsWith(".exe")) {
    $nameQuery = $nameQuery.Substring(0, $nameQuery.Length - 4)
}
$fileFilter = $query
if (-not $fileFilter.ToLower().EndsWith(".exe")) {
    $fileFilter = $fileFilter + "*.exe"
}
`

// findMatchFunctions — правила сравнения имён. Вынесены отдельно, чтобы
// проверять их на фиксированных строках (psScript_test.go): ложное
// «установлена» хуже, чем «не найдена». Функции читают $queryKey из сценария.
const findMatchFunctions = `
# Кириллические буквы, неотличимые от латинских. «1С:Предприятие» пишут
# с кириллической «С», а набирают чаще латинскую «1c» — для сравнения это
# одна и та же буква.
$lookalikeLetters = @(
    @("а", "a"), @("в", "b"), @("е", "e"), @("ё", "e"), @("к", "k"), @("м", "m"), @("н", "h"),
    @("о", "o"), @("р", "p"), @("с", "c"), @("т", "t"), @("у", "y"), @("х", "x")
)

function Get-MatchKey([object]$Value) {
    if ($null -eq $Value) { return "" }
    $key = ([string]$Value).ToLowerInvariant() -replace '[^\p{L}\p{Nd}]', ''
    foreach ($pair in $lookalikeLetters) { $key = $key.Replace($pair[0], $pair[1]) }
    return $key
}

# Коды продуктов MSI и папки кэша — GUID (у Inno Setup с хвостом «_is1»). Из
# шестнадцатеричных цифр в них складывается что угодно: «1c» входит в каждый
# код Office (…0000000FF1CE).
function Test-GuidName([object]$Value) {
    return ([string]$Value).Trim() -match '^\{?[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}\}?(_is1)?$'
}

# Части путей, которые есть почти у всего: по ним «windows» или «program»
# отметили бы каждую службу как установленную программу.
$genericPathParts = @{}
foreach ($part in @("c", "windows", "system32", "syswow64", "programfiles", "programfilesx86", "programdata",
        "commonfiles", "users", "appdata", "local", "locallow", "roaming", "microsoft", "bin", "temp")) {
    $genericPathParts[$part] = $true
}

function Test-Match([object]$Value) {
    $key = Get-MatchKey $Value
    if ($key -eq "") { return $false }
    return $key.Contains($queryKey)
}

# Пути и имена папок — косвенный признак, поэтому сравниваются по частям
# (склеенный путь даёт случайные совпадения вроде «…svchostexe»), без GUID и
# только для запросов от трёх символов.
function Test-PathMatch([object]$Value) {
    if ($queryKey.Length -lt 3) { return $false }
    # У службы PathName — командная строка: путь в кавычках или до «.exe»,
    # дальше аргументы («svchost.exe -k netsvcs»), они не часть имени.
    $path = ([string]$Value).Trim()
    if ($path.StartsWith('"')) {
        $end = $path.IndexOf('"', 1)
        if ($end -gt 0) { $path = $path.Substring(1, $end - 1) }
    } else {
        $exe = $path.ToLowerInvariant().IndexOf(".exe")
        if ($exe -ge 0) { $path = $path.Substring(0, $exe + 4) }
    }
    foreach ($part in ($path -split '[\\/]')) {
        $part = $part.Trim().Trim('"')
        if ($part -eq "" -or (Test-GuidName $part)) { continue }
        if ($genericPathParts.ContainsKey((Get-MatchKey $part))) { continue }
        if (Test-Match $part) { return $true }
    }
    return $false
}
`

const findScriptBody = `
# Запрос из одних знаков («++») после очистки пуст — тогда сравниваем как есть.
$queryKey = Get-MatchKey $nameQuery
if ($queryKey -eq "") { $queryKey = $nameQuery.ToLowerInvariant() }

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
            $keyName = Limit-Text $key.PSChildName
            try {
                $values = Get-ItemProperty $key.PSPath -ErrorAction Stop
            } catch {
                # Самозащита антивируса закрывает значения ключа, но имя
                # ключа («DrWeb…») видно и по нему программу ещё можно узнать.
                if (-not (Test-GuidName $keyName) -and (Test-Match $keyName)) {
                    Add-Match "реестр, значения закрыты" $root.Owner $keyName "" ""
                }
                continue
            }
            # Имя ключа — запасной признак: только когда DisplayName пуст и
            # ключ назван словами, а не кодом продукта.
            $displayName = Limit-Text $values.DisplayName
            $name = $displayName
            if ($displayName -ne "") {
                if (-not (Test-Match $displayName)) { continue }
            } else {
                if ((Test-GuidName $keyName) -or -not (Test-Match $keyName)) { continue }
                $name = $keyName
            }
            Add-Match "реестр" $root.Owner $name (Limit-Text $values.DisplayVersion) (Limit-Text $values.InstallLocation)
        }
    } catch {
        Write-FindWarning ("Реестр: " + $root.Path) $_.Exception.Message
    }
}

# 2. Службы: антивирусы, агенты и серверные программы работают службами, и их
# видно, даже когда запись в реестре закрыта самозащитой.
try {
    foreach ($service in @(Get-CimInstance -ClassName Win32_Service -Property Name,DisplayName,PathName,State -ErrorAction Stop)) {
        if ($appMatches.Count -ge $maxMatches) { break }
        if (-not (Test-Match $service.DisplayName) -and -not (Test-Match $service.Name) -and -not (Test-PathMatch $service.PathName)) { continue }
        $state = "остановлена"
        if ([string]$service.State -eq "Running") { $state = "работает" }
        $serviceName = Limit-Text $service.DisplayName
        if ($serviceName -eq "") { $serviceName = Limit-Text $service.Name }
        Add-Match ("служба, " + $state) "Весь компьютер" $serviceName "" (Limit-Text $service.PathName)
    }
} catch {
    Write-FindWarning "Службы" $_.Exception.Message
}

# 3. Запущенные процессы — по имени и по пути: у Dr.Web процесс dwservice.exe,
# но лежит он в папке DrWeb. GetOwner — это вызов CIM (~0,3 с), поэтому
# владельцев спрашиваем не больше чем у 20 процессов: на терминальном сервере
# широкий запрос находит сотни копий, и проверка упиралась бы в тайм-аут.
$ownerLookups = 0
try {
    foreach ($process in @(Get-CimInstance -ClassName Win32_Process -Property Name,ExecutablePath,ProcessId,Handle -ErrorAction Stop)) {
        if ($appMatches.Count -ge $maxMatches) { break }
        $processName = Limit-Text $process.Name
        if ($processName -eq "") { continue }
        if (-not (Test-Match $processName) -and -not (Test-PathMatch $process.ExecutablePath)) { continue }
        $processOwner = ""
        if ($ownerLookups -lt 20) {
            $ownerLookups++
            try {
                $ownerInfo = Invoke-CimMethod -InputObject $process -MethodName GetOwner -ErrorAction Stop
                if ([uint32]$ownerInfo.ReturnValue -eq 0) {
                    $processOwner = Limit-Text (([string]$ownerInfo.Domain) + "\" + ([string]$ownerInfo.User)).Trim("\")
                }
            } catch {}
        }
        Add-Match "запущен" $processOwner $processName "" (Limit-Text $process.ExecutablePath)
    }
} catch {
    Write-FindWarning "Запущенные процессы" $_.Exception.Message
}

# 4. Папки установки: Program Files, Program Files (x86), ProgramData и одна
# ступень вглубь («Doctor Web\Dr.Web Security Space»).
foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:ProgramData)) {
    if ([string]::IsNullOrWhiteSpace($root) -or $appMatches.Count -ge $maxMatches) { continue }
    try {
        foreach ($folder in @(Get-ChildItem -LiteralPath $root -Directory -Depth 1 -ErrorAction SilentlyContinue)) {
            if ($appMatches.Count -ge $maxMatches) { break }
            if (Test-PathMatch $folder.Name) {
                Add-Match "папка" "Весь компьютер" (Limit-Text $folder.Name) "" (Limit-Text $folder.FullName)
            }
        }
    } catch {
        Write-FindWarning ("Папки установки: " + $root) $_.Exception.Message
    }
}

# 5. Файлы в типичных каталогах установки. Обход всего диска занимал бы минуты,
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
