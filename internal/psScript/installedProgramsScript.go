package psscript

// InstalledProgramsScript выполняется уже на целевом компьютере через
// RunRemoteScript. Он читает стандартные ветки деинсталляции Windows и выдаёт
// по одной компактной Base64-записи на программу. Win32_Product намеренно не
// используется: его запрос может запустить проверку установленных MSI-пакетов.
const InstalledProgramsScript = `
$ErrorActionPreference = "Stop"

function Limit-Text([object]$Value) {
    if ($null -eq $Value) {
        return ""
    }
    $text = ([string]$Value).Trim()
    if ($text.Length -gt 300) {
        return $text.Substring(0, 300)
    }
    return $text
}

function Normalize-RegistryPath([object]$Value) {
    $text = ([string]$Value).Trim()
    return $text -replace '^Microsoft\.PowerShell\.Core\\Registry::', ''
}

function Format-InstallDate([object]$Value) {
    $text = Limit-Text $Value
    if ($text -match "^\d{8}$") {
        try {
            return [DateTime]::ParseExact(
                $text,
                "yyyyMMdd",
                [Globalization.CultureInfo]::InvariantCulture
            ).ToString("dd.MM.yyyy")
        } catch {}
    }
    return $text
}

function Write-ScanWarning([string]$Stage, [string]$Message) {
    $record = [PSCustomObject]@{
        Stage = Limit-Text $Stage
        Message = Limit-Text $Message
    }
    $json = ConvertTo-Json -InputObject $record -Compress
    $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
    Write-Output ("SCAN_WARNING|" + $payload)
}

$registrySources = @(
    [PSCustomObject]@{
        Path = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*"
        BasePath = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall"
        Architecture = "x64"
        Owner = "Весь компьютер"
    },
    [PSCustomObject]@{
        Path = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*"
        BasePath = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"
        Architecture = "x86"
        Owner = "Весь компьютер"
    }
)

# Добавляем программы из загруженных пользовательских профилей. Реестровые
# кусты не загружаются принудительно, чтобы сканирование не меняло состояние
# профилей на целевом компьютере.
$userHives = @()
try {
    $userHives = @(Get-ChildItem -Path "Registry::HKEY_USERS" -ErrorAction Stop |
        Where-Object { $_.PSChildName -match "^S-1-5-21-(\d+-){3}\d+$" })
} catch {
    Write-ScanWarning "HKEY_USERS" $_.Exception.Message
}
function Resolve-AccountName([string]$Sid) {
    try {
        $account = (New-Object Security.Principal.SecurityIdentifier($Sid)).Translate([Security.Principal.NTAccount])
        return Limit-Text $account.Value
    } catch {
        return $Sid
    }
}

$loadedSids = @{}
foreach ($hive in $userHives) {
    $sid = $hive.PSChildName
    $loadedSids[$sid] = $true
    $owner = Resolve-AccountName $sid
    $registrySources += [PSCustomObject]@{
        Path = "Registry::HKEY_USERS\$sid\Software\Microsoft\Windows\CurrentVersion\Uninstall\*"
        BasePath = "Registry::HKEY_USERS\$sid\Software\Microsoft\Windows\CurrentVersion\Uninstall"
        Architecture = "x64"
        Owner = $owner
    }
    $registrySources += [PSCustomObject]@{
        Path = "Registry::HKEY_USERS\$sid\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*"
        BasePath = "Registry::HKEY_USERS\$sid\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall"
        Architecture = "x86"
        Owner = $owner
    }
}

# Куст профиля существует, пока пользователь не вышел из системы. У вышедших
# сотрудников личные установки не видны, и сказать об этом нужно явно: иначе
# пустой список выглядит как «ничего не установлено».
try {
    $offline = @(Get-CimInstance -ClassName Win32_UserProfile -ErrorAction Stop |
        Where-Object { -not $_.Special -and $_.SID -match "^S-1-5-21-" -and -not $loadedSids.ContainsKey($_.SID) })
    if ($offline.Count -gt 0) {
        $names = @($offline | ForEach-Object { Resolve-AccountName $_.SID }) -join ", "
        Write-ScanWarning "Профили без активного сеанса" ("личные установки не видны у: " + $names)
    }
} catch {
    Write-ScanWarning "Win32_UserProfile" $_.Exception.Message
}

$programs = New-Object "System.Collections.Generic.List[object]"
$scannedSources = 0
$failedSources = 0
foreach ($source in $registrySources) {
    $entries = @()
    try {
        if (-not (Test-Path -LiteralPath $source.BasePath -ErrorAction Stop)) {
            continue
        }
        $entries = @(Get-ItemProperty -Path $source.Path -ErrorAction Stop)
        $scannedSources++
    } catch {
        $failedSources++
        Write-ScanWarning (Normalize-RegistryPath $source.BasePath) $_.Exception.Message
        continue
    }
    foreach ($entry in $entries) {
        $displayName = Limit-Text $entry.DisplayName
        if ([string]::IsNullOrWhiteSpace($displayName)) {
            continue
        }
        if ([int]$entry.SystemComponent -eq 1) {
            continue
        }

        $registryPath = Normalize-RegistryPath $entry.PSPath
        $productCode = Limit-Text $entry.PSChildName
        $isMSI = [int]$entry.WindowsInstaller -eq 1 -and $productCode -match '^\{[0-9A-Fa-f-]{36}\}$'
        $hasQuietCommand = -not [string]::IsNullOrWhiteSpace([string]$entry.QuietUninstallString)
        # Пользовательские ветки HKU доступны на запись самому пользователю.
        # Не запускаем указанные там команды с административными правами.
        $machineScope = $registryPath -like 'HKEY_LOCAL_MACHINE\*'
        $canSilentUninstall = $machineScope -and ($isMSI -or $hasQuietCommand)
        $uninstallDescription = "Тихое удаление недоступно"
        if (-not $machineScope) {
            $uninstallDescription = "Пользовательская установка: удаление недоступно"
        } elseif ($isMSI) {
            $uninstallDescription = "MSI"
        } elseif ($hasQuietCommand) {
            $uninstallDescription = "QuietUninstallString"
        }

        [void]$programs.Add([PSCustomObject]@{
            Name = $displayName
            Version = Limit-Text $entry.DisplayVersion
            Publisher = Limit-Text $entry.Publisher
            InstallDate = Format-InstallDate $entry.InstallDate
            Architecture = $source.Architecture
            Owner = $source.Owner
            RegistryPath = $registryPath
            CanSilentUninstall = $canSilentUninstall
            UninstallDescription = $uninstallDescription
        })
    }
}

$uniquePrograms = @($programs | Sort-Object RegistryPath -Unique)
foreach ($program in $uniquePrograms) {
    $json = ConvertTo-Json -InputObject $program -Compress
    $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
    Write-Output ("PROGRAM|" + $payload)
}
Write-Output ("SCAN_SUMMARY|" + $scannedSources + "|" + $failedSources)
`
