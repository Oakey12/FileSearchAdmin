package psscript

// CommandScript выполняется локально и запускает пользовательский сценарий на
// выбранном компьютере через CIM поверх DCOM. Пароль и сценарий читаются из
// stdin в Base64, поэтому не попадают в аргументы процесса.
//
// Сценарий и результат передаются частями через временный раздел реестра на
// удалённом компьютере. Это позволяет не зависеть от ограничения длины
// командной строки Win32_Process. Create и не требует WinRM/TrustedHosts.
const CommandScript = `
param(
    [Parameter(Mandatory=$true)][string]$ComputerName,
    [string]$DefaultUser,
    [switch]$CredentialAndScriptFromStdin,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-f0-9]{32}$')][string]$JobId,
    [ValidateRange(5, 3600)][int]$RemoteTimeoutSeconds = 110,
    [ValidateRange(1024, 2097152)][int]$MaxOutputCharacters = 524288,
    [ValidatePattern('^$|^S-1-5-21-(\d+-){3}\d+$')][string]$TargetUserSID,
    [string]$TargetUserName,
    [ValidateRange(0, 65535)][int]$TargetSessionID = 0
)

$utf8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$ErrorActionPreference = "Stop"

$session = $null
$jobKey = $null
$remoteProcessId = 0
$hklm = [uint32]2147483650
$registryNamespace = "root/default"
$jobsRoot = "SOFTWARE\1Cproc\Jobs"
$targetingUserSession = -not [string]::IsNullOrWhiteSpace($TargetUserSID)
$taskName = "1Cproc-" + $JobId
if ($targetingUserSession) {
    $hklm = [uint32]2147483651
    $jobsRoot = $TargetUserSID + "\Software\1Cproc\Jobs"
}
$chunkSize = 24000
$cimConnectTimeoutSeconds = 20
$cimOperationTimeoutSeconds = 60

function Write-ControlledError([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) {
        $Text = "Неизвестная ошибка удалённого выполнения"
    }
    $safeText = $Text.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
    Write-Output ("ERROR|" + $safeText)
}

function Invoke-RegistryMethod([string]$MethodName, [hashtable]$Arguments) {
    $parameters = @{
        CimSession = $session
        Namespace = $registryNamespace
        ClassName = "StdRegProv"
        MethodName = $MethodName
        Arguments = $Arguments
        OperationTimeoutSec = $cimOperationTimeoutSeconds
        ErrorAction = "Stop"
    }
    $result = Invoke-CimMethod @parameters
    if ([uint32]$result.ReturnValue -ne 0) {
        throw "Операция реестра $MethodName завершилась с кодом $($result.ReturnValue)"
    }
    return $result
}

function Set-RemoteString([string]$Name, [string]$Value) {
    [void](Invoke-RegistryMethod "SetStringValue" @{
        hDefKey = $hklm
        sSubKeyName = $jobKey
        sValueName = $Name
        sValue = $Value
    })
}

function Get-RemoteString([string]$Name, [switch]$AllowMissing) {
    $parameters = @{
        CimSession = $session
        Namespace = $registryNamespace
        ClassName = "StdRegProv"
        MethodName = "GetStringValue"
        Arguments = @{
            hDefKey = $hklm
            sSubKeyName = $jobKey
            sValueName = $Name
        }
        OperationTimeoutSec = $cimOperationTimeoutSeconds
        ErrorAction = "Stop"
    }
    $result = Invoke-CimMethod @parameters
    if ([uint32]$result.ReturnValue -eq 0) {
        return [string]$result.sValue
    }
    # Разные версии StdRegProv возвращают 1 (ERROR_INVALID_FUNCTION) либо
    # 2 (ERROR_FILE_NOT_FOUND), когда необязательного строкового значения нет.
    # Остальные коды, включая отказ в доступе, по-прежнему считаются ошибкой.
    if ($AllowMissing -and [uint32]$result.ReturnValue -in @(1, 2)) {
        return $null
    }
    throw "Чтение значения реестра $Name завершилось с кодом $($result.ReturnValue)"
}

function Get-RemoteStringAt([string]$Key, [string]$Name) {
    $parameters = @{
        CimSession = $session
        Namespace = $registryNamespace
        ClassName = "StdRegProv"
        MethodName = "GetStringValue"
        Arguments = @{
            hDefKey = $hklm
            sSubKeyName = $Key
            sValueName = $Name
        }
        OperationTimeoutSec = 3
        ErrorAction = "SilentlyContinue"
    }
    $result = Invoke-CimMethod @parameters
    if ($null -ne $result -and [uint32]$result.ReturnValue -eq 0) {
        return [string]$result.sValue
    }
    return $null
}

function Remove-StaleRemoteJobs {
    $enumParameters = @{
        CimSession = $session
        Namespace = $registryNamespace
        ClassName = "StdRegProv"
        MethodName = "EnumKey"
        Arguments = @{ hDefKey = $hklm; sSubKeyName = $jobsRoot }
        OperationTimeoutSec = 3
        ErrorAction = "SilentlyContinue"
    }
    $enumResult = Invoke-CimMethod @enumParameters
    if ($null -eq $enumResult -or [uint32]$enumResult.ReturnValue -ne 0) {
        return
    }

    $nowTicks = [DateTime]::UtcNow.Ticks
    $processedJobs = 0
    foreach ($child in @($enumResult.sNames)) {
        if ($processedJobs -ge 1) {
            break
        }
        if ($child -notmatch '^[a-f0-9]{32}$') {
            continue
        }
        $candidateKey = "$jobsRoot\$child"
        $expiresText = Get-RemoteStringAt $candidateKey "ExpiresUtcTicks"
        $expiresTicks = [int64]0
        if (-not [int64]::TryParse($expiresText, [ref]$expiresTicks) -or $expiresTicks -gt $nowTicks) {
            continue
        }
        $processedJobs++

        $processText = Get-RemoteStringAt $candidateKey "RemoteProcessId"
        $createdText = Get-RemoteStringAt $candidateKey "RemoteProcessCreatedUtcTicks"
        $processId = [uint32]0
        $createdTicks = [int64]0
        if ([uint32]::TryParse($processText, [ref]$processId) -and $processId -gt 0 -and [int64]::TryParse($createdText, [ref]$createdTicks)) {
            try {
                $process = Get-CimInstance -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -Filter ("ProcessId=" + $processId) -OperationTimeoutSec 3 -ErrorAction SilentlyContinue
                if ($null -ne $process -and $process.CreationDate.ToUniversalTime().Ticks -eq $createdTicks) {
                    [void](Invoke-CimMethod -InputObject $process -MethodName "Terminate" -OperationTimeoutSec 3 -ErrorAction SilentlyContinue)
                }
            } catch {}
        }

        try {
            [void](Invoke-CimMethod -CimSession $session -Namespace $registryNamespace -ClassName "StdRegProv" -MethodName "DeleteKey" -Arguments @{ hDefKey = $hklm; sSubKeyName = $candidateKey } -OperationTimeoutSec 3 -ErrorAction SilentlyContinue)
        } catch {}
        if ($targetingUserSession) {
            try {
                $staleTaskName = "1Cproc-" + $child
                [void](Invoke-CimMethod -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -MethodName "Create" -Arguments @{ CommandLine = ('schtasks.exe /Delete /TN "' + $staleTaskName + '" /F') } -OperationTimeoutSec 3 -ErrorAction SilentlyContinue)
            } catch {}
        }
    }
}

try {
    if (-not $CredentialAndScriptFromStdin) {
        throw "Приложение не передало учётные данные и сценарий"
    }
    if ([string]::IsNullOrWhiteSpace($DefaultUser)) {
        throw "Не указан логин"
    }
    if ($targetingUserSession) {
        if ([string]::IsNullOrWhiteSpace($TargetUserName) -or $TargetUserName.Contains([char]13) -or $TargetUserName.Contains([char]10)) {
            throw "Не указано корректное имя выбранного пользователя"
        }
        if ($TargetSessionID -le 0) {
            throw "Не указана выбранная пользовательская сессия"
        }
    } elseif (-not [string]::IsNullOrWhiteSpace($TargetUserName) -or $TargetSessionID -ne 0) {
        throw "Параметры пользовательской сессии переданы не полностью"
    }

    $encodedPassword = [Console]::In.ReadLine()
    $encodedScript = [Console]::In.ReadLine()
    if ([string]::IsNullOrWhiteSpace($encodedPassword)) {
        throw "Не указан пароль"
    }
    if ([string]::IsNullOrWhiteSpace($encodedScript)) {
        throw "Не указан сценарий"
    }

    $scriptBytes = [Convert]::FromBase64String($encodedScript)
    $scriptCipher = [Security.Cryptography.Aes]::Create()
    $scriptCipher.GenerateKey()
    $scriptCipher.GenerateIV()
    $scriptEncryptor = $scriptCipher.CreateEncryptor()
    $encryptedScriptBytes = $scriptEncryptor.TransformFinalBlock($scriptBytes, 0, $scriptBytes.Length)
    $encodedScript = [Convert]::ToBase64String($encryptedScriptBytes)
    $scriptKeyText = [Convert]::ToBase64String($scriptCipher.Key)
    $scriptIVText = [Convert]::ToBase64String($scriptCipher.IV)
    $scriptEncryptor.Dispose()
    $scriptCipher.Dispose()
    $scriptBytes = $null
    $encryptedScriptBytes = $null
    $passwordBytes = [Convert]::FromBase64String($encodedPassword)
    $plainPassword = [Text.Encoding]::UTF8.GetString($passwordBytes)
    $securePassword = ConvertTo-SecureString -String $plainPassword -AsPlainText -Force
    $credential = New-Object System.Management.Automation.PSCredential -ArgumentList $DefaultUser, $securePassword

    $connectionTimer = [Diagnostics.Stopwatch]::StartNew()
    Write-Output "STAGE|Подключение CIM/DCOM"
    $sessionOption = New-CimSessionOption -Protocol Dcom
    $sessionParameters = @{
        ComputerName = $ComputerName
        Credential = $credential
        SessionOption = $sessionOption
        OperationTimeoutSec = $cimConnectTimeoutSeconds
        ErrorAction = "Stop"
    }
    $session = New-CimSession @sessionParameters
    Write-Output ("TIMING|Подключение CIM/DCOM|" + $connectionTimer.ElapsedMilliseconds)

    Write-Output "STAGE|Очистка старых удалённых заданий"
    $staleCleanupTimer = [Diagnostics.Stopwatch]::StartNew()
    Remove-StaleRemoteJobs
    Write-Output ("TIMING|Очистка старых заданий|" + $staleCleanupTimer.ElapsedMilliseconds)
    Write-Output "STAGE|Передача сценария"
    $jobKey = "$jobsRoot\$JobId"
    [void](Invoke-RegistryMethod "CreateKey" @{
        hDefKey = $hklm
        sSubKeyName = $jobKey
    })

    $scriptChunkCount = [int][Math]::Ceiling($encodedScript.Length / [double]$chunkSize)
    Set-RemoteString "ScriptChunks" ([string]$scriptChunkCount)
    Set-RemoteString "MaxOutputCharacters" ([string]$MaxOutputCharacters)
    Set-RemoteString "CreatedUtcTicks" ([string][DateTime]::UtcNow.Ticks)
    Set-RemoteString "ExpiresUtcTicks" ([string][DateTime]::UtcNow.AddSeconds($RemoteTimeoutSeconds + 300).Ticks)
    Set-RemoteString "Status" "Pending"
    for ($index = 0; $index -lt $scriptChunkCount; $index++) {
        $offset = $index * $chunkSize
        $length = [Math]::Min($chunkSize, $encodedScript.Length - $offset)
        Set-RemoteString ("Script{0:D4}" -f $index) $encodedScript.Substring($offset, $length)
    }

    $bootstrapTemplate = @'
$ErrorActionPreference = "Stop"
$jobKey = "__JOB_KEY__"
$registryPath = "__REGISTRY_PATH__"
$chunkSize = 24000
$scriptKeyText = "__SCRIPT_KEY__"
$scriptIVText = "__SCRIPT_IV__"

function Set-JobString([string]$Name, [string]$Value) {
    New-ItemProperty -LiteralPath $registryPath -Name $Name -Value $Value -PropertyType String -Force | Out-Null
}

try {
    Set-JobString "RemoteProcessId" ([string]$PID)
    Set-JobString "RemoteProcessCreatedUtcTicks" ([string][Diagnostics.Process]::GetCurrentProcess().StartTime.ToUniversalTime().Ticks)
    Set-JobString "Stage" "подготовка сценария"
    $job = Get-ItemProperty -LiteralPath $registryPath -ErrorAction Stop
    $scriptChunkCount = [int]$job.ScriptChunks
    $maxOutputCharacters = [int]$job.MaxOutputCharacters
    $encodedBuilder = New-Object Text.StringBuilder
    for ($index = 0; $index -lt $scriptChunkCount; $index++) {
        $name = "Script{0:D4}" -f $index
        [void]$encodedBuilder.Append([string]$job.$name)
    }

    $encryptedScriptBytes = [Convert]::FromBase64String($encodedBuilder.ToString())
    $scriptCipher = [Security.Cryptography.Aes]::Create()
    $scriptCipher.Key = [Convert]::FromBase64String($scriptKeyText)
    $scriptCipher.IV = [Convert]::FromBase64String($scriptIVText)
    $scriptDecryptor = $scriptCipher.CreateDecryptor()
    $scriptBytes = $scriptDecryptor.TransformFinalBlock($encryptedScriptBytes, 0, $encryptedScriptBytes.Length)
    $source = [Text.Encoding]::UTF8.GetString($scriptBytes)
    $scriptDecryptor.Dispose()
    $scriptCipher.Dispose()
    $scriptBytes = $null
    $encryptedScriptBytes = $null
    for ($index = 0; $index -lt $scriptChunkCount; $index++) {
        Remove-ItemProperty -LiteralPath $registryPath -Name ("Script{0:D4}" -f $index) -ErrorAction SilentlyContinue
    }
    Remove-ItemProperty -LiteralPath $registryPath -Name "ScriptChunks" -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $registryPath -Name "MaxOutputCharacters" -ErrorAction SilentlyContinue
    $encodedBuilder = $null
    $job = $null

    $outputBuilder = New-Object Text.StringBuilder
    $remainingCharacters = $maxOutputCharacters
    $outputTruncated = $false
    Set-JobString "Stage" "выполнение сценария"
    & ([ScriptBlock]::Create($source)) *>&1 | ForEach-Object {
        if ($remainingCharacters -le 0) {
            $outputTruncated = $true
            return
        }

        $text = ($_ | Out-String -Width 4096).TrimEnd()
        if ([string]::IsNullOrEmpty($text)) {
            return
        }
        if ($outputBuilder.Length -gt 0) {
            if ($remainingCharacters -le 1) {
                $outputTruncated = $true
                $remainingCharacters = 0
                return
            }
            [void]$outputBuilder.Append([Environment]::NewLine)
            $remainingCharacters -= 1
        }

        if ($text.Length -gt $remainingCharacters) {
            [void]$outputBuilder.Append($text.Substring(0, $remainingCharacters))
            $remainingCharacters = 0
            $outputTruncated = $true
            return
        }
        [void]$outputBuilder.Append($text)
        $remainingCharacters -= $text.Length
    }

    $commandOutput = $outputBuilder.ToString()
    $outputTruncatedText = "0"
    if ($outputTruncated) {
        $outputTruncatedText = "1"
    }
    # Значение создаётся всегда: нормальный короткий вывод не должен
    # интерпретироваться клиентом как ошибка чтения отсутствующего параметра.
    Set-JobString "Stage" "передача результата"
    Set-JobString "OutputTruncated" $outputTruncatedText
    $outputBytes = [Text.Encoding]::UTF8.GetBytes($commandOutput)
    $outputCipher = [Security.Cryptography.Aes]::Create()
    $outputCipher.Key = [Convert]::FromBase64String($scriptKeyText)
    $outputCipher.GenerateIV()
    $outputEncryptor = $outputCipher.CreateEncryptor()
    $encryptedOutputBytes = $outputEncryptor.TransformFinalBlock($outputBytes, 0, $outputBytes.Length)
    Set-JobString "OutputIV" ([Convert]::ToBase64String($outputCipher.IV))
    $encodedOutput = [Convert]::ToBase64String($encryptedOutputBytes)
    $outputEncryptor.Dispose()
    $outputCipher.Dispose()
    $outputBytes = $null
    $encryptedOutputBytes = $null
    $outputChunkCount = [int][Math]::Ceiling($encodedOutput.Length / [double]$chunkSize)
    Set-JobString "OutputChunks" ([string]$outputChunkCount)
    for ($index = 0; $index -lt $outputChunkCount; $index++) {
        $offset = $index * $chunkSize
        $length = [Math]::Min($chunkSize, $encodedOutput.Length - $offset)
        Set-JobString ("Output{0:D4}" -f $index) $encodedOutput.Substring($offset, $length)
    }
    Set-JobString "Status" "OK"
} catch {
    try {
        Set-JobString "ErrorText" ([string]$_.Exception.Message)
        Set-JobString "Status" "ERROR"
    } catch {}
    exit 1
} finally {
    $source = $null
    $scriptKeyText = $null
    $scriptIVText = $null
    $commandOutput = $null
    $encodedOutput = $null
}
'@
    $bootstrapRegistryPath = "Registry::HKEY_LOCAL_MACHINE\$jobKey"
    if ($targetingUserSession) {
        $bootstrapRegistryPath = "Registry::HKEY_CURRENT_USER\Software\1Cproc\Jobs\$JobId"
    }
    $bootstrap = $bootstrapTemplate.Replace("__JOB_KEY__", $jobKey).Replace("__REGISTRY_PATH__", $bootstrapRegistryPath).Replace("__SCRIPT_KEY__", $scriptKeyText).Replace("__SCRIPT_IV__", $scriptIVText)
    $encodedBootstrap = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($bootstrap))
    $commandLine = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $encodedBootstrap"

    $executionTimer = [Diagnostics.Stopwatch]::StartNew()
    Write-Output "STAGE|Запуск удалённого PowerShell"
    if ($targetingUserSession) {
        Set-RemoteString "TargetUserName" $TargetUserName
        Set-RemoteString "TargetSessionID" ([string]$TargetSessionID)
        Set-RemoteString "TaskArguments" ("-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + $encodedBootstrap)
        Set-RemoteString "LaunchStatus" "Pending"
        $launcherTemplate = @'
$ErrorActionPreference = "Stop"
$registryPath = "__LAUNCHER_REGISTRY_PATH__"
$taskName = "__TASK_NAME__"
function Set-LaunchString([string]$Name, [string]$Value) {
    New-ItemProperty -LiteralPath $registryPath -Name $Name -Value $Value -PropertyType String -Force | Out-Null
}
try {
    $job = Get-ItemProperty -LiteralPath $registryPath -ErrorAction Stop
    $targetUserName = [string]$job.TargetUserName
    $targetSessionID = [int]$job.TargetSessionID
    $taskArguments = [string]$job.TaskArguments
    if ([string]::IsNullOrWhiteSpace($targetUserName) -or $targetSessionID -le 0 -or [string]::IsNullOrWhiteSpace($taskArguments)) {
        throw "Не найдены параметры запуска пользовательской сессии"
    }
    $service = New-Object -ComObject "Schedule.Service"
    $service.Connect()
    $root = $service.GetFolder("\")
    $definition = $service.NewTask(0)
    $definition.RegistrationInfo.Description = "Одноразовая команда 1Cproc"
    $definition.Settings.Enabled = $true
    $definition.Settings.AllowDemandStart = $true
    $definition.Settings.StartWhenAvailable = $false
    $definition.Settings.ExecutionTimeLimit = "PT__TIMEOUT_SECONDS__S"
    $definition.Principal.UserId = $targetUserName
    $definition.Principal.LogonType = 3
    $definition.Principal.RunLevel = 0
    $action = $definition.Actions.Create(0)
    $action.Path = Join-Path $env:SystemRoot "System32\WindowsPowerShell\v1.0\powershell.exe"
    $action.Arguments = $taskArguments
    $registered = $root.RegisterTaskDefinition($taskName, $definition, 6, $null, $null, 3, $null)
    $running = $registered.RunEx($null, 4, $targetSessionID, $targetUserName)
    if ($null -eq $running) {
        throw "Планировщик не запустил одноразовую задачу"
    }
    Remove-ItemProperty -LiteralPath $registryPath -Name "TaskArguments" -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $registryPath -Name "TargetUserName" -ErrorAction SilentlyContinue
    Remove-ItemProperty -LiteralPath $registryPath -Name "TargetSessionID" -ErrorAction SilentlyContinue
    Set-LaunchString "LaunchStatus" "Started"
} catch {
    try {
        Set-LaunchString "LaunchErrorText" ([string]$_.Exception.Message)
        Set-LaunchString "LaunchStatus" "Error"
    } catch {}
    exit 1
}
'@
        $launcherRegistryPath = "Registry::HKEY_USERS\$jobKey"
        $launcher = $launcherTemplate.Replace("__LAUNCHER_REGISTRY_PATH__", $launcherRegistryPath).Replace("__TASK_NAME__", $taskName).Replace("__TIMEOUT_SECONDS__", [string]$RemoteTimeoutSeconds)
        $encodedLauncher = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($launcher))
        $launcherCommand = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $encodedLauncher"
        $createResult = Invoke-CimMethod -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -MethodName "Create" -Arguments @{ CommandLine = $launcherCommand } -OperationTimeoutSec $cimOperationTimeoutSeconds -ErrorAction Stop
        if ([uint32]$createResult.ReturnValue -ne 0) {
            throw "Не удалось запустить регистрацию пользовательской задачи, код $($createResult.ReturnValue)"
        }
        $launchDeadline = [DateTime]::UtcNow.AddSeconds(15)
        do {
            $launchStatus = Get-RemoteString "LaunchStatus" -AllowMissing
            if ($launchStatus -eq "Started") {
                break
            }
            if ($launchStatus -eq "Error") {
                $launchError = Get-RemoteString "LaunchErrorText" -AllowMissing
                throw ("Не удалось запустить сценарий в сессии пользователя: " + $launchError)
            }
            Start-Sleep -Milliseconds 250
        } while ([DateTime]::UtcNow -lt $launchDeadline)
        if ($launchStatus -ne "Started") {
            throw "Планировщик не подтвердил запуск сценария в течение 15 секунд"
        }
        Write-Output ("SESSION_EXECUTION|" + $TargetSessionID + "|" + $TargetUserName)
    } else {
        $createParameters = @{
            CimSession = $session
            Namespace = "root/cimv2"
            ClassName = "Win32_Process"
            MethodName = "Create"
            Arguments = @{ CommandLine = $commandLine }
            OperationTimeoutSec = $cimOperationTimeoutSeconds
            ErrorAction = "Stop"
        }
        $createResult = Invoke-CimMethod @createParameters
        if ([uint32]$createResult.ReturnValue -ne 0) {
            throw "Win32_Process.Create завершился с кодом $($createResult.ReturnValue)"
        }
        $remoteProcessId = [uint32]$createResult.ProcessId
        try {
            $createdProcess = Get-CimInstance -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -Filter ("ProcessId=" + $remoteProcessId) -OperationTimeoutSec 5 -ErrorAction SilentlyContinue
            if ($null -ne $createdProcess) {
                Set-RemoteString "RemoteProcessCreatedUtcTicks" ([string]$createdProcess.CreationDate.ToUniversalTime().Ticks)
                Set-RemoteString "RemoteProcessId" ([string]$remoteProcessId)
            }
        } catch {}
    }

    Write-Output "STAGE|Выполнение удалённого PowerShell"
    $deadline = [DateTime]::UtcNow.AddSeconds($RemoteTimeoutSeconds)
    $processMissingSince = $null
    $nextProcessCheck = [DateTime]::UtcNow
    $remoteStatus = $null
    while ([DateTime]::UtcNow -lt $deadline) {
        $remoteStatus = Get-RemoteString "Status" -AllowMissing
        if ($remoteStatus -eq "OK" -or $remoteStatus -eq "ERROR") {
            break
        }

        if (-not $targetingUserSession -and [DateTime]::UtcNow -ge $nextProcessCheck) {
            $nextProcessCheck = [DateTime]::UtcNow.AddSeconds(2)
            $processParameters = @{
                CimSession = $session
                Namespace = "root/cimv2"
                ClassName = "Win32_Process"
                Filter = ("ProcessId=" + $remoteProcessId)
                OperationTimeoutSec = 10
                ErrorAction = "Stop"
            }
            $remoteProcess = Get-CimInstance @processParameters
            if ($null -eq $remoteProcess) {
                if ($null -eq $processMissingSince) {
                    $processMissingSince = [DateTime]::UtcNow
                } elseif (([DateTime]::UtcNow - $processMissingSince).TotalSeconds -ge 2) {
                    $lastStage = Get-RemoteString "Stage" -AllowMissing
                    if ([string]::IsNullOrWhiteSpace($lastStage)) {
                        throw "Удалённый PowerShell не начал работу: процесс создан и сразу завершился. Обычно так себя ведёт целевой компьютер, где запуск PowerShell запрещён политикой или заблокирован средством защиты"
                    }
                    throw ("Удалённый PowerShell был прерван на этапе «" + $lastStage + "» и не вернул результат. Чаще всего процесс останавливает антивирус или EDR на целевом компьютере")
                }
            } else {
                $processMissingSince = $null
            }
        }
        Start-Sleep -Milliseconds 750
    }

    if ($remoteStatus -ne "OK" -and $remoteStatus -ne "ERROR") {
        try {
            if ($targetingUserSession) {
                [void][uint32]::TryParse((Get-RemoteString "RemoteProcessId" -AllowMissing), [ref]$remoteProcessId)
            }
            $processParameters = @{
                CimSession = $session
                Namespace = "root/cimv2"
                ClassName = "Win32_Process"
                Filter = ("ProcessId=" + $remoteProcessId)
                ErrorAction = "SilentlyContinue"
            }
            $remoteProcess = Get-CimInstance @processParameters
            if ($null -ne $remoteProcess) {
                [void](Invoke-CimMethod -InputObject $remoteProcess -MethodName Terminate -ErrorAction SilentlyContinue)
            }
        } catch {}
        throw "Превышено время выполнения удалённого сценария ($RemoteTimeoutSeconds сек.)"
    }

    Write-Output ("TIMING|Выполнение PowerShell|" + $executionTimer.ElapsedMilliseconds)
    if ($remoteStatus -eq "ERROR") {
        $remoteError = Get-RemoteString "ErrorText" -AllowMissing
        if ([string]::IsNullOrWhiteSpace($remoteError)) {
            $remoteError = "Удалённый сценарий завершился с ошибкой без описания"
        }
        throw $remoteError
    }

    Write-Output "STAGE|Получение результата"
    $outputChunkCountText = Get-RemoteString "OutputChunks" -AllowMissing
    if (-not [string]::IsNullOrWhiteSpace($outputChunkCountText)) {
        $outputChunkCount = [int]$outputChunkCountText
        $encodedOutputBuilder = New-Object Text.StringBuilder
        for ($index = 0; $index -lt $outputChunkCount; $index++) {
            $outputName = "Output{0:D4}" -f $index
            $encodedOutputChunk = Get-RemoteString $outputName
            if (-not [string]::IsNullOrEmpty($encodedOutputChunk)) {
                [void]$encodedOutputBuilder.Append($encodedOutputChunk)
            }
            [void](Invoke-RegistryMethod "DeleteValue" @{
                hDefKey = $hklm
                sSubKeyName = $jobKey
                sValueName = $outputName
            })
        }
        [void](Invoke-RegistryMethod "DeleteValue" @{
            hDefKey = $hklm
            sSubKeyName = $jobKey
            sValueName = "OutputChunks"
        })

        $outputIVText = Get-RemoteString "OutputIV"
        $encryptedOutputBytes = [Convert]::FromBase64String($encodedOutputBuilder.ToString())
        $outputCipher = [Security.Cryptography.Aes]::Create()
        $outputCipher.Key = [Convert]::FromBase64String($scriptKeyText)
        $outputCipher.IV = [Convert]::FromBase64String($outputIVText)
        $outputDecryptor = $outputCipher.CreateDecryptor()
        $outputBytes = $outputDecryptor.TransformFinalBlock($encryptedOutputBytes, 0, $encryptedOutputBytes.Length)
        $plainEncodedOutput = [Convert]::ToBase64String($outputBytes)
        for ($offset = 0; $offset -lt $plainEncodedOutput.Length; $offset += $chunkSize) {
            $length = [Math]::Min($chunkSize, $plainEncodedOutput.Length - $offset)
            Write-Output ("COMMAND_OUTPUT|" + $plainEncodedOutput.Substring($offset, $length))
        }
        $outputDecryptor.Dispose()
        $outputCipher.Dispose()
        [void](Invoke-RegistryMethod "DeleteValue" @{
            hDefKey = $hklm
            sSubKeyName = $jobKey
            sValueName = "OutputIV"
        })
    }
    if ((Get-RemoteString "OutputTruncated" -AllowMissing) -eq "1") {
        Write-Output "COMMAND_OUTPUT_TRUNCATED"
    }
    Write-Output "COMMAND_OK"
    exit 0
} catch {
    Write-ControlledError $_.Exception.Message
    exit 1
} finally {
    if ($null -ne $session -and -not [string]::IsNullOrWhiteSpace($jobKey)) {
        if ($targetingUserSession) {
            try {
                $deleteTaskCommand = 'schtasks.exe /Delete /TN "' + $taskName + '" /F'
                [void](Invoke-CimMethod -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -MethodName "Create" -Arguments @{ CommandLine = $deleteTaskCommand } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue)
            } catch {}
        }
        try {
            $deleteParameters = @{
                CimSession = $session
                Namespace = $registryNamespace
                ClassName = "StdRegProv"
                MethodName = "DeleteKey"
                Arguments = @{ hDefKey = $hklm; sSubKeyName = $jobKey }
                OperationTimeoutSec = 10
                ErrorAction = "SilentlyContinue"
            }
            [void](Invoke-CimMethod @deleteParameters)
        } catch {}
    }
    if ($null -ne $session) {
        Remove-CimSession -CimSession $session -ErrorAction SilentlyContinue
    }
    $encodedPassword = $null
    $passwordBytes = $null
    $plainPassword = $null
    $securePassword = $null
    $credential = $null
    $encodedScript = $null
    $bootstrap = $null
    $encodedBootstrap = $null
    $scriptKeyText = $null
    $scriptIVText = $null
}
`

// CleanupCommandScript выполняет аварийную очистку задания, если основной
// локальный PowerShell был остановлен до своего блока finally.
const CleanupCommandScript = `
param(
    [Parameter(Mandatory=$true)][string]$ComputerName,
    [string]$DefaultUser,
    [Parameter(Mandatory=$true)][ValidatePattern('^[a-f0-9]{32}$')][string]$JobId,
    [ValidatePattern('^$|^S-1-5-21-(\d+-){3}\d+$')][string]$TargetUserSID,
    [switch]$CredentialFromStdin
)

$utf8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$ErrorActionPreference = "Stop"
$session = $null
$hklm = [uint32]2147483650
$jobKey = "SOFTWARE\1Cproc\Jobs\$JobId"
$taskName = "1Cproc-" + $JobId
if (-not [string]::IsNullOrWhiteSpace($TargetUserSID)) {
    $hklm = [uint32]2147483651
    $jobKey = $TargetUserSID + "\Software\1Cproc\Jobs\$JobId"
}

try {
    if (-not $CredentialFromStdin) {
        throw "Приложение не передало учётные данные"
    }
    $encodedPassword = [Console]::In.ReadLine()
    if ([string]::IsNullOrWhiteSpace($encodedPassword)) {
        throw "Не указан пароль"
    }
    $passwordBytes = [Convert]::FromBase64String($encodedPassword)
    $plainPassword = [Text.Encoding]::UTF8.GetString($passwordBytes)
    $securePassword = ConvertTo-SecureString -String $plainPassword -AsPlainText -Force
    $credential = New-Object System.Management.Automation.PSCredential -ArgumentList $DefaultUser, $securePassword
    $sessionOption = New-CimSessionOption -Protocol Dcom
    $session = New-CimSession -ComputerName $ComputerName -Credential $credential -SessionOption $sessionOption -OperationTimeoutSec 5

    if (-not [string]::IsNullOrWhiteSpace($TargetUserSID)) {
        try {
            $endTaskCommand = 'schtasks.exe /End /TN "' + $taskName + '"'
            [void](Invoke-CimMethod -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -MethodName "Create" -Arguments @{ CommandLine = $endTaskCommand } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue)
            Start-Sleep -Milliseconds 500
            $deleteTaskCommand = 'schtasks.exe /Delete /TN "' + $taskName + '" /F'
            [void](Invoke-CimMethod -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -MethodName "Create" -Arguments @{ CommandLine = $deleteTaskCommand } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue)
        } catch {}
    }

    $pidResult = Invoke-CimMethod -CimSession $session -Namespace "root/default" -ClassName "StdRegProv" -MethodName "GetStringValue" -Arguments @{
        hDefKey = $hklm
        sSubKeyName = $jobKey
        sValueName = "RemoteProcessId"
    } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue
    $createdResult = Invoke-CimMethod -CimSession $session -Namespace "root/default" -ClassName "StdRegProv" -MethodName "GetStringValue" -Arguments @{
        hDefKey = $hklm
        sSubKeyName = $jobKey
        sValueName = "RemoteProcessCreatedUtcTicks"
    } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue
    $processId = [uint32]0
    $createdTicks = [int64]0
    if ($null -ne $pidResult -and [uint32]$pidResult.ReturnValue -eq 0 -and $null -ne $createdResult -and [uint32]$createdResult.ReturnValue -eq 0 -and [uint32]::TryParse([string]$pidResult.sValue, [ref]$processId) -and $processId -gt 0 -and [int64]::TryParse([string]$createdResult.sValue, [ref]$createdTicks)) {
        $process = Get-CimInstance -CimSession $session -Namespace "root/cimv2" -ClassName "Win32_Process" -Filter ("ProcessId=" + $processId) -OperationTimeoutSec 5 -ErrorAction SilentlyContinue
        if ($null -ne $process -and $process.CreationDate.ToUniversalTime().Ticks -eq $createdTicks) {
            [void](Invoke-CimMethod -InputObject $process -MethodName "Terminate" -OperationTimeoutSec 5 -ErrorAction SilentlyContinue)
        }
    }

    [void](Invoke-CimMethod -CimSession $session -Namespace "root/default" -ClassName "StdRegProv" -MethodName "DeleteKey" -Arguments @{
        hDefKey = $hklm
        sSubKeyName = $jobKey
    } -OperationTimeoutSec 10 -ErrorAction SilentlyContinue)
    Write-Output "CLEANUP_OK"
} catch {
    $reason = $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
    Write-Output ("ERROR|" + $reason)
    exit 1
} finally {
    if ($null -ne $session) {
        Remove-CimSession -CimSession $session -ErrorAction SilentlyContinue
    }
    $encodedPassword = $null
    $passwordBytes = $null
    $plainPassword = $null
    $securePassword = $null
    $credential = $null
}
`
