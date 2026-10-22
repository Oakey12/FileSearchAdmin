package psscript

// RemoteUsersScript выполняется уже на целевом компьютере через
// RunRemoteScript. Прежний вариант перебирал сеансы с машины администратора
// через CIM-сессию: на каждый explorer.exe уходило два вызова по DCOM, и
// семнадцать сеансов растягивались на минуты. Здесь те же вызовы локальные,
// поэтому перебор занимает секунды и распараллеливать нечего.
const RemoteUsersScript = `
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

try {
    $explorers = @(Get-CimInstance -ClassName Win32_Process -Filter "Name='explorer.exe'" -Property ProcessId,SessionId,Name -ErrorAction Stop)
} catch {
    throw ("Поиск пользовательских сеансов не удался: " + $_.Exception.Message)
}

$profilesBySid = @{}
try {
    foreach ($userProfile in @(Get-CimInstance -ClassName Win32_UserProfile -Property SID,LocalPath,Loaded,Special -ErrorAction Stop)) {
        if ($userProfile.Loaded -and -not $userProfile.Special) {
            $profilesBySid[[string]$userProfile.SID] = [string]$userProfile.LocalPath
        }
    }
} catch {
    throw ("Чтение профилей не удалось: " + $_.Exception.Message)
}

$seenSids = @{}
$foundUsers = 0
foreach ($explorer in $explorers) {
    try {
        $ownerSidInfo = Invoke-CimMethod -InputObject $explorer -MethodName GetOwnerSid -ErrorAction Stop
        if ([uint32]$ownerSidInfo.ReturnValue -ne 0) {
            throw ("GetOwnerSid вернул код " + $ownerSidInfo.ReturnValue)
        }
        $sid = ([string]$ownerSidInfo.Sid).Trim()
        if ($sid -notmatch '^S-1-5-21-(\d+-){3}\d+$' -or $seenSids.ContainsKey($sid)) {
            continue
        }
        if (-not $profilesBySid.ContainsKey($sid)) {
            throw "для активной сессии не найден загруженный профиль Win32_UserProfile"
        }

        $ownerInfo = Invoke-CimMethod -InputObject $explorer -MethodName GetOwner -ErrorAction Stop
        if ([uint32]$ownerInfo.ReturnValue -ne 0 -or [string]::IsNullOrWhiteSpace([string]$ownerInfo.User)) {
            throw ("GetOwner вернул код " + $ownerInfo.ReturnValue)
        }
        $username = ([string]$ownerInfo.User).Trim()
        $domain = ([string]$ownerInfo.Domain).Trim()
        $displayName = $username
        if (-not [string]::IsNullOrWhiteSpace($domain)) {
            $displayName = $domain + '\' + $username
        }

        $record = [PSCustomObject]@{
            DisplayName = Limit-Text $displayName
            Username = Limit-Text $username
            Domain = Limit-Text $domain
            SID = $sid
            ProfilePath = Limit-Text $profilesBySid[$sid]
            SessionID = [uint32]$explorer.SessionId
        }
        $json = ConvertTo-Json -InputObject $record -Compress
        Write-Output ("REMOTE_USER|" + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json)))
        $seenSids[$sid] = $true
        $foundUsers++
    } catch {
        $reason = $_.Exception.Message.Replace("|", "/").Replace([char]13, " ").Replace([char]10, " ")
        Write-Output ("WARN_REMOTE_USER|" + $explorer.ProcessId + "|" + $explorer.SessionId + "|" + $reason)
    }
}

if ($explorers.Count -gt 0 -and $foundUsers -eq 0) {
    throw "Обнаружены пользовательские сессии, но не удалось определить ни одного владельца и путь его профиля"
}
Write-Output "USERS_OK"
`
