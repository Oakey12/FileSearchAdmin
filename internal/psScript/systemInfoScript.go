package psscript

// SystemInfoScript выполняется на целевом компьютере через RunRemoteScript и
// отдаёт одну Base64-запись со сводкой для системного администратора. Данные
// берутся из CIM: имя и домен, версия Windows, процессор, память, диски и
// сетевые адреса. Тяжёлых классов вроде Win32_Product здесь нет, поэтому сбор
// укладывается в секунды.
const SystemInfoScript = `
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

function Write-InfoWarning([string]$Stage, [string]$Message) {
    $record = [PSCustomObject]@{
        Stage = Limit-Text $Stage
        Message = Limit-Text $Message
    }
    $json = ConvertTo-Json -InputObject $record -Compress
    $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
    Write-Output ("INFO_WARNING|" + $payload)
}

function Get-SafeInstance([string]$ClassName, [string]$Stage) {
    try {
        return @(Get-CimInstance -ClassName $ClassName -ErrorAction Stop)
    } catch {
        Write-InfoWarning $Stage $_.Exception.Message
        return @()
    }
}

function Format-Gigabytes([object]$Bytes) {
    if ($null -eq $Bytes) {
        return ""
    }
    try {
        $value = [double]$Bytes
    } catch {
        return ""
    }
    if ($value -le 0) {
        return ""
    }
    return ([math]::Round($value / 1GB, 1)).ToString([Globalization.CultureInfo]::InvariantCulture)
}

function Format-LocalTime([object]$Value) {
    if ($null -eq $Value) {
        return ""
    }
    try {
        return ([DateTime]$Value).ToString("dd.MM.yyyy HH:mm")
    } catch {
        return ""
    }
}

$computerSystem = Get-SafeInstance "Win32_ComputerSystem" "Сведения о компьютере" | Select-Object -First 1
$operatingSystem = Get-SafeInstance "Win32_OperatingSystem" "Сведения об ОС" | Select-Object -First 1
$processors = Get-SafeInstance "Win32_Processor" "Сведения о процессоре"
$bios = Get-SafeInstance "Win32_BIOS" "Сведения о BIOS" | Select-Object -First 1

$computerName = ""
$domain = ""
$partOfDomain = $false
$manufacturer = ""
$model = ""
$loggedOnUser = ""
$memoryTotal = ""
if ($null -ne $computerSystem) {
    $computerName = Limit-Text $computerSystem.Name
    $domain = Limit-Text $computerSystem.Domain
    $partOfDomain = [bool]$computerSystem.PartOfDomain
    $manufacturer = Limit-Text $computerSystem.Manufacturer
    $model = Limit-Text $computerSystem.Model
    $loggedOnUser = Limit-Text $computerSystem.UserName
    $memoryTotal = Format-Gigabytes $computerSystem.TotalPhysicalMemory
}

$osCaption = ""
$osVersion = ""
$osBuild = ""
$osArchitecture = ""
$osInstallDate = ""
$lastBootTime = ""
$uptime = ""
$memoryFree = ""
if ($null -ne $operatingSystem) {
    $osCaption = Limit-Text $operatingSystem.Caption
    $osVersion = Limit-Text $operatingSystem.Version
    $osBuild = Limit-Text $operatingSystem.BuildNumber
    $osArchitecture = Limit-Text $operatingSystem.OSArchitecture
    $osInstallDate = Format-LocalTime $operatingSystem.InstallDate
    $lastBootTime = Format-LocalTime $operatingSystem.LastBootUpTime
    if ($computerName -eq "") {
        $computerName = Limit-Text $operatingSystem.CSName
    }
    if ($null -ne $operatingSystem.FreePhysicalMemory) {
        $memoryFree = Format-Gigabytes ([double]$operatingSystem.FreePhysicalMemory * 1KB)
    }
    try {
        $span = (Get-Date) - [DateTime]$operatingSystem.LastBootUpTime
        $uptime = [string][math]::Floor($span.TotalDays) + " дн. " + $span.Hours + " ч. " + $span.Minutes + " мин."
    } catch {}
}

$processorName = ""
$processorCores = 0
$processorLogical = 0
$processorClock = 0
foreach ($processor in $processors) {
    if ($processorName -eq "") {
        $processorName = Limit-Text $processor.Name
        $processorClock = [int]$processor.MaxClockSpeed
    }
    $processorCores = $processorCores + [int]$processor.NumberOfCores
    $processorLogical = $processorLogical + [int]$processor.NumberOfLogicalProcessors
}

$serialNumber = ""
if ($null -ne $bios) {
    $serialNumber = Limit-Text $bios.SerialNumber
}

$addresses = @()
try {
    $adapters = @(Get-CimInstance -ClassName Win32_NetworkAdapterConfiguration -Filter "IPEnabled=True" -ErrorAction Stop)
    foreach ($adapter in $adapters) {
        foreach ($address in @($adapter.IPAddress)) {
            $text = Limit-Text $address
            if ($text -ne "" -and $text -notlike "*:*") {
                $addresses += $text
            }
        }
    }
} catch {
    Write-InfoWarning "Сетевые адреса" $_.Exception.Message
}

$disks = @()
try {
    $volumes = @(Get-CimInstance -ClassName Win32_LogicalDisk -Filter "DriveType=3" -ErrorAction Stop)
    foreach ($volume in $volumes) {
        $total = Format-Gigabytes $volume.Size
        $free = Format-Gigabytes $volume.FreeSpace
        if ($total -eq "") {
            continue
        }
        $disks += (Limit-Text $volume.DeviceID) + " свободно " + $free + " ГБ из " + $total + " ГБ"
    }
} catch {
    Write-InfoWarning "Сведения о дисках" $_.Exception.Message
}

# Папки профилей на системном диске. Размер считается обходом файлов, и на
# терминальном сервере это самая дорогая часть сбора, поэтому на всю секцию
# отведён бюджет времени: что не успели посчитать, помечается явно, а не
# задерживает ответ на минуты.
$userFolders = @()
try {
    $usersRoot = Join-Path $env:SystemDrive "Users"
    $sizeBudget = [Diagnostics.Stopwatch]::StartNew()
    $budgetSeconds = 45
    foreach ($folder in @(Get-ChildItem -LiteralPath $usersRoot -Directory -Force -ErrorAction Stop | Sort-Object Name)) {
        $sizeText = "размер не посчитан"
        if ($sizeBudget.Elapsed.TotalSeconds -lt $budgetSeconds) {
            try {
                $total = (Get-ChildItem -LiteralPath $folder.FullName -Recurse -File -Force -ErrorAction SilentlyContinue |
                    Measure-Object -Property Length -Sum).Sum
                $gigabytes = Format-Gigabytes $total
                if ($gigabytes -eq "") {
                    $sizeText = "пусто"
                } else {
                    $sizeText = $gigabytes + " ГБ"
                }
            } catch {}
        }
        $changed = Format-LocalTime $folder.LastWriteTime
        $line = (Limit-Text $folder.Name) + " — " + $sizeText
        if ($changed -ne "") {
            $line = $line + ", изменена " + $changed
        }
        $userFolders += $line
    }
} catch {
    Write-InfoWarning "Папки пользователей" $_.Exception.Message
}

$record = [PSCustomObject]@{
    ComputerName = $computerName
    Domain = $domain
    PartOfDomain = $partOfDomain
    LoggedOnUser = $loggedOnUser
    Manufacturer = $manufacturer
    Model = $model
    SerialNumber = $serialNumber
    OSCaption = $osCaption
    OSVersion = $osVersion
    OSBuild = $osBuild
    OSArchitecture = $osArchitecture
    OSInstallDate = $osInstallDate
    LastBootTime = $lastBootTime
    Uptime = $uptime
    ProcessorName = $processorName
    ProcessorCores = $processorCores
    ProcessorLogical = $processorLogical
    ProcessorClockMHz = $processorClock
    MemoryTotalGB = $memoryTotal
    MemoryFreeGB = $memoryFree
    IPAddresses = $addresses
    Disks = $disks
    UserFolders = $userFolders
}

$json = ConvertTo-Json -InputObject $record -Compress -Depth 3
$payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
Write-Output ("SYSTEM_INFO|" + $payload)
`
