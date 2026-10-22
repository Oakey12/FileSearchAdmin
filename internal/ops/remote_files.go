package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MaxListedRemoteFiles = 300
	MaxEditableFileBytes = 192 * 1024
)

var editableFileExtensions = map[string]struct{}{
	".bat": {}, ".cfg": {}, ".cmd": {}, ".conf": {}, ".css": {}, ".csv": {},
	".env": {}, ".go": {}, ".htm": {}, ".html": {}, ".ini": {}, ".js": {},
	".json": {}, ".log": {}, ".md": {}, ".properties": {}, ".ps1": {},
	".psm1": {}, ".sql": {}, ".tsv": {}, ".txt": {}, ".xml": {}, ".yaml": {},
	".yml": {},
}

// ScanRemoteFolder получает список файлов только на первом уровне указанной
// папки. Содержимое файлов не читается.
func ScanRemoteFolder(
	ctx context.Context,
	targetIP, username, password, folderPath string,
	logLine func(string),
	onComplete func(constandstruct.RemoteFolderResult, error),
) bool {
	targetIP, username, folderPath, err := validateRemoteFileOperation(
		targetIP, username, password, folderPath, false,
	)
	if err != nil {
		logLine("Файлы: " + err.Error())
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()

		target := constandstruct.Server{Name: "Сканирование папки", IP: targetIP}
		logLine(fmt.Sprintf("=== Сканирование папки %q на %s ===", folderPath, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.FileOperationTimeout, logLine)
		lines, runErr := remotefunc.RunRemoteScriptWithProgress(
			operationCtx, targetIP, username, password, buildFolderScanScript(folderPath),
			remoteProgressLogger(target, logLine),
		)

		var result constandstruct.RemoteFolderResult
		resultErr := runErr
		if resultErr == nil {
			result, resultErr = parseRemoteFolderResult(lines)
		}
		finishStatus(resultErr)
		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine("Файлы: ошибка сканирования папки: " + resultErr.Error())
			}
		} else {
			logRemoteWarnings(target, lines, logLine)
			logLine(fmt.Sprintf("Файлы: найдено файлов: %d.", len(result.Files)))
			if result.Truncated {
				logLine(fmt.Sprintf("Файлы: показаны первые %d файлов.", MaxListedRemoteFiles))
			}
		}
		logLine("--- Сканирование папки завершено ---")
		if onComplete != nil {
			onComplete(result, resultErr)
		}
	}()
	return true
}

// ReadRemoteTextFile загружает поддерживаемый текстовый файл для редактора.
func ReadRemoteTextFile(
	ctx context.Context,
	targetIP, username, password string,
	file constandstruct.RemoteFile,
	logLine func(string),
	onComplete func(constandstruct.RemoteTextDocument, error),
) bool {
	targetIP, username, cleanedPath, err := validateRemoteFileOperation(
		targetIP, username, password, file.FullPath, true,
	)
	if err != nil {
		logLine("Редактор: " + err.Error())
		return false
	}
	file.FullPath = cleanedPath
	if !isEditableTextFile(file.FullPath) {
		logLine("Редактор: выбранный тип файла не поддерживает безопасное текстовое редактирование.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()
		target := constandstruct.Server{Name: "Чтение документа", IP: targetIP}
		logLine(fmt.Sprintf("=== Чтение документа %q на %s ===", file.Name, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.FileOperationTimeout, logLine)
		lines, runErr := remotefunc.RunRemoteScriptWithProgress(
			operationCtx, targetIP, username, password, buildReadTextFileScript(file.FullPath),
			remoteProgressLogger(target, logLine),
		)

		var document constandstruct.RemoteTextDocument
		resultErr := runErr
		if resultErr == nil {
			document, resultErr = parseRemoteTextDocument(lines)
		}
		finishStatus(resultErr)
		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine("Редактор: ошибка чтения документа: " + resultErr.Error())
			}
		} else {
			logLine(fmt.Sprintf("Редактор: документ %q загружен, кодировка %s.", file.Name, document.Encoding))
		}
		logLine("--- Чтение документа завершено ---")
		if onComplete != nil {
			onComplete(document, resultErr)
		}
	}()
	return true
}

// SaveRemoteTextFile сохраняет изменённый текст атомарной заменой. Перед
// заменой проверяется SHA-256 исходного файла и создаётся резервная копия.
func SaveRemoteTextFile(
	ctx context.Context,
	targetIP, username, password string,
	document constandstruct.RemoteTextDocument,
	newContent string,
	logLine func(string),
	onComplete func(constandstruct.RemoteSaveResult, error),
) bool {
	targetIP, username, cleanedPath, err := validateRemoteFileOperation(
		targetIP, username, password, document.File.FullPath, true,
	)
	if err != nil {
		logLine("Редактор: " + err.Error())
		return false
	}
	document.File.FullPath = cleanedPath
	if !isEditableTextFile(document.File.FullPath) {
		logLine("Редактор: выбранный тип файла не поддерживает безопасное текстовое редактирование.")
		return false
	}
	if len([]byte(newContent)) > MaxEditableFileBytes {
		logLine(fmt.Sprintf("Редактор: текст превышает допустимый размер %d байт.", MaxEditableFileBytes))
		return false
	}
	if !validDocumentEncoding(document.Encoding) || !isSHA256(document.OriginalSHA256) {
		logLine("Редактор: сведения об исходном документе повреждены; откройте файл повторно.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()
		target := constandstruct.Server{Name: "Сохранение документа", IP: targetIP}
		logLine(fmt.Sprintf("=== Сохранение документа %q на %s ===", document.File.Name, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.FileOperationTimeout, logLine)
		lines, runErr := remotefunc.RunRemoteScriptWithProgress(
			operationCtx,
			targetIP,
			username,
			password,
			buildSaveTextFileScript(document, newContent),
			remoteProgressLogger(target, logLine),
		)

		var result constandstruct.RemoteSaveResult
		resultErr := runErr
		if resultErr == nil {
			result, resultErr = parseRemoteSaveResult(lines)
		}
		finishStatus(resultErr)
		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine("Редактор: ошибка сохранения документа: " + resultErr.Error())
			}
		} else {
			logLine(fmt.Sprintf("Редактор: документ %q сохранён; создана резервная копия.", document.File.Name))
		}
		logLine("--- Сохранение документа завершено ---")
		if onComplete != nil {
			onComplete(result, resultErr)
		}
	}()
	return true
}

// DeleteRemoteFile безвозвратно удаляет выбранный файл. Перед удалением
// повторно проверяются размер и время изменения из результата сканирования,
// чтобы не удалить заменённый другим пользователем файл.
func DeleteRemoteFile(
	ctx context.Context,
	targetIP, username, password string,
	file constandstruct.RemoteFile,
	logLine func(string),
	onComplete func(error),
) bool {
	targetIP, username, cleanedPath, err := validateRemoteFileOperation(
		targetIP, username, password, file.FullPath, true,
	)
	if err != nil {
		logLine("Удаление файла: " + err.Error())
		return false
	}
	file.FullPath = cleanedPath
	file.Name = strings.TrimSpace(file.Name)
	lastWrite, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(file.LastWriteTimeUTC))
	if file.Name == "" || file.Size < 0 || err != nil {
		logLine("Удаление файла: сведения о выбранном файле устарели; выполните сканирование повторно.")
		return false
	}
	file.LastWriteTimeUTC = lastWrite.UTC().Format(time.RFC3339Nano)

	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}
	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()
		target := constandstruct.Server{Name: "Удаление файла", IP: targetIP}
		logLine(fmt.Sprintf("=== Удаление файла %q на %s ===", file.Name, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.FileOperationTimeout, logLine)
		lines, runErr := remotefunc.RunRemoteScriptWithProgress(
			operationCtx,
			targetIP,
			username,
			password,
			buildDeleteRemoteFileScript(file),
			remoteProgressLogger(target, logLine),
		)

		resultErr := runErr
		if resultErr == nil {
			resultErr = parseRemoteDeleteResult(lines, file.FullPath)
		}
		finishStatus(resultErr)
		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine("Удаление файла: " + resultErr.Error())
			}
		} else {
			logLine(fmt.Sprintf("Удаление файла: файл %q удалён с %s.", file.Name, targetIP))
		}
		logLine("--- Удаление файла завершено ---")
		if onComplete != nil {
			onComplete(resultErr)
		}
	}()
	return true
}

func validateRemoteFileOperation(targetIP, username, password, path string, requireFile bool) (string, string, string, error) {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	path = strings.TrimSpace(path)
	if net.ParseIP(targetIP) == nil {
		return "", "", "", fmt.Errorf("введите корректный IP-адрес целевого компьютера")
	}
	if username == "" || password == "" {
		return "", "", "", fmt.Errorf("введите логин и пароль администратора целевого компьютера")
	}
	if path == "" || strings.ContainsRune(path, 0) || len(path) > 1024 || !filepath.IsAbs(path) {
		kind := "папки"
		if requireFile {
			kind = "файла"
		}
		return "", "", "", fmt.Errorf("укажите абсолютный Windows-путь %s", kind)
	}
	return targetIP, username, filepath.Clean(path), nil
}

func isEditableTextFile(path string) bool {
	_, ok := editableFileExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func validDocumentEncoding(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "utf-8", "utf-8-bom", "utf-16le", "utf-16be", "windows-1251":
		return true
	default:
		return false
	}
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

func encodeScriptValue(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func editableExtensionsPowerShell() string {
	extensions := make([]string, 0, len(editableFileExtensions))
	for extension := range editableFileExtensions {
		extensions = append(extensions, quotePowerShellLiteral(extension))
	}
	sort.Strings(extensions)
	return strings.Join(extensions, ",")
}

func buildFolderScanScript(folderPath string) string {
	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$folderPath = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$editableExtensions = @(%s)
if (-not (Test-Path -LiteralPath $folderPath -PathType Container)) {
    throw "Указанная папка не найдена или недоступна."
}
$resolvedFolder = (Get-Item -LiteralPath $folderPath -Force -ErrorAction Stop).FullName
$items = @(Get-ChildItem -LiteralPath $resolvedFolder -File -Force -ErrorAction Stop | Sort-Object Name | Select-Object -First %d)
$truncated = $items.Count -gt %d
foreach ($item in @($items | Select-Object -First %d)) {
    $extension = ([string]$item.Extension).ToLowerInvariant()
    $record = [PSCustomObject]@{
        Name = [string]$item.Name
        FullPath = [string]$item.FullName
        Extension = $extension
        Size = [long]$item.Length
        LastWriteTimeUTC = $item.LastWriteTimeUtc.ToString('o')
        IsText = $editableExtensions -contains $extension
    }
    $json = ConvertTo-Json -InputObject $record -Compress
    $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
    Write-Output ("FILE|" + $payload)
}
if ($truncated) {
    Write-Output "FILE_LIST_TRUNCATED|%d"
}
`, encodeScriptValue(folderPath), editableExtensionsPowerShell(), MaxListedRemoteFiles+1, MaxListedRemoteFiles, MaxListedRemoteFiles, MaxListedRemoteFiles)
}

func buildReadTextFileScript(path string) string {
	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$editableExtensions = @(%s)
if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "Файл не найден или недоступен."
}
$item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
$extension = ([string]$item.Extension).ToLowerInvariant()
if ($editableExtensions -notcontains $extension) {
    throw "Этот тип файла нельзя безопасно открыть в текстовом редакторе."
}
$bytes = [IO.File]::ReadAllBytes($item.FullName)
if ($bytes.Length -gt %d) {
    throw "Файл слишком большой для встроенного редактора."
}

$offset = 0
if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
    $encodingName = 'utf-8-bom'
    $encoding = [Text.UTF8Encoding]::new($false, $true)
    $offset = 3
} elseif ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFF -and $bytes[1] -eq 0xFE) {
    $encodingName = 'utf-16le'
    $encoding = [Text.Encoding]::Unicode
    $offset = 2
} elseif ($bytes.Length -ge 2 -and $bytes[0] -eq 0xFE -and $bytes[1] -eq 0xFF) {
    $encodingName = 'utf-16be'
    $encoding = [Text.Encoding]::BigEndianUnicode
    $offset = 2
} else {
    try {
        $encoding = [Text.UTF8Encoding]::new($false, $true)
        $content = $encoding.GetString($bytes)
        $encodingName = 'utf-8'
    } catch {
        $encoding = [Text.Encoding]::GetEncoding(1251)
        $content = $encoding.GetString($bytes)
        $encodingName = 'windows-1251'
    }
}
if ($offset -gt 0) {
    $content = $encoding.GetString($bytes, $offset, $bytes.Length - $offset)
}
if ($content.IndexOf([char]0) -ge 0) {
    throw "Файл содержит бинарные данные и не может быть отредактирован как текст."
}
$sha = [Security.Cryptography.SHA256]::Create()
try {
    $hash = ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant()
} finally {
    $sha.Dispose()
}
$record = [PSCustomObject]@{
    File = [PSCustomObject]@{
        Name = [string]$item.Name
        FullPath = [string]$item.FullName
        Extension = $extension
        Size = [long]$item.Length
        LastWriteTimeUTC = $item.LastWriteTimeUtc.ToString('o')
        IsText = $true
    }
    ContentBase64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($content))
    Encoding = $encodingName
    OriginalSHA256 = $hash
}
$json = ConvertTo-Json -InputObject $record -Compress -Depth 4
$payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
Write-Output ("DOCUMENT|" + $payload)
`, encodeScriptValue(path), editableExtensionsPowerShell(), MaxEditableFileBytes)
}

func buildSaveTextFileScript(document constandstruct.RemoteTextDocument, newContent string) string {
	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$content = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$encodingName = '%s'
$expectedHash = '%s'
if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "Файл не найден или недоступен."
}
$currentBytes = [IO.File]::ReadAllBytes($path)
$sha = [Security.Cryptography.SHA256]::Create()
try {
    $currentHash = ([BitConverter]::ToString($sha.ComputeHash($currentBytes))).Replace('-', '').ToLowerInvariant()
} finally {
    $sha.Dispose()
}
if ($currentHash -ne $expectedHash) {
    throw "Файл был изменён после открытия. Откройте его повторно, чтобы не потерять чужие изменения."
}

switch ($encodingName) {
    'utf-8' { $encoding = [Text.UTF8Encoding]::new($false); $preamble = [byte[]]@() }
    'utf-8-bom' { $encoding = [Text.UTF8Encoding]::new($true); $preamble = $encoding.GetPreamble() }
    'utf-16le' { $encoding = [Text.Encoding]::Unicode; $preamble = $encoding.GetPreamble() }
    'utf-16be' { $encoding = [Text.Encoding]::BigEndianUnicode; $preamble = $encoding.GetPreamble() }
    'windows-1251' { $encoding = [Text.Encoding]::GetEncoding(1251); $preamble = [byte[]]@() }
    default { throw "Неподдерживаемая кодировка документа." }
}
$body = $encoding.GetBytes($content)
$newBytes = New-Object byte[] ($preamble.Length + $body.Length)
if ($preamble.Length -gt 0) { [Array]::Copy($preamble, 0, $newBytes, 0, $preamble.Length) }
[Array]::Copy($body, 0, $newBytes, $preamble.Length, $body.Length)
if ($newBytes.Length -gt %d) {
    throw "Изменённый документ слишком большой для сохранения."
}

$directory = [IO.Path]::GetDirectoryName($path)
$fileName = [IO.Path]::GetFileName($path)
$tempPath = Join-Path $directory ('.' + $fileName + '.1cproc.' + [Guid]::NewGuid().ToString('N') + '.tmp')
$backupPath = $path + '.1cproc-backup-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff') + '.bak'
try {
    [IO.File]::WriteAllBytes($tempPath, $newBytes)
    [IO.File]::Replace($tempPath, $path, $backupPath, $true)
} finally {
    if (Test-Path -LiteralPath $tempPath) {
        Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    }
}
$item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
$record = [PSCustomObject]@{
    File = [PSCustomObject]@{
        Name = [string]$item.Name
        FullPath = [string]$item.FullName
        Extension = ([string]$item.Extension).ToLowerInvariant()
        Size = [long]$item.Length
        LastWriteTimeUTC = $item.LastWriteTimeUtc.ToString('o')
        IsText = $true
    }
    BackupPath = $backupPath
}
$json = ConvertTo-Json -InputObject $record -Compress -Depth 4
$payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
Write-Output ("FILE_SAVED|" + $payload)
`, encodeScriptValue(document.File.FullPath), encodeScriptValue(newContent), document.Encoding, strings.ToLower(document.OriginalSHA256), MaxEditableFileBytes)
}

func buildDeleteRemoteFileScript(file constandstruct.RemoteFile) string {
	return fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$expectedSize = [long]%d
$expectedLastWriteText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "Файл уже удалён или недоступен. Выполните сканирование повторно."
}
$item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
$expectedLastWrite = [DateTime]::Parse(
    $expectedLastWriteText,
    [Globalization.CultureInfo]::InvariantCulture,
    [Globalization.DateTimeStyles]::RoundtripKind
).ToUniversalTime()
if ([long]$item.Length -ne $expectedSize -or $item.LastWriteTimeUtc.Ticks -ne $expectedLastWrite.Ticks) {
    throw "Файл изменился после сканирования. Выполните сканирование повторно, чтобы не удалить другой файл."
}
Remove-Item -LiteralPath $item.FullName -Force -ErrorAction Stop
if (Test-Path -LiteralPath $path) {
    throw "Windows не подтвердил удаление файла."
}
$payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($path))
Write-Output ("FILE_DELETED|" + $payload)
`, encodeScriptValue(file.FullPath), file.Size, encodeScriptValue(file.LastWriteTimeUTC))
}

func parseRemoteFolderResult(lines []string) (constandstruct.RemoteFolderResult, error) {
	var result constandstruct.RemoteFolderResult
	if text := remoteError(lines); text != "" {
		return result, fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}
	confirmed := false
	seen := make(map[string]struct{})
	for _, line := range lines {
		switch {
		case line == "COMMAND_OK":
			confirmed = true
		case line == "COMMAND_OUTPUT_TRUNCATED":
			result.Truncated = true
		case strings.HasPrefix(line, "COMMAND_LINE|FILE_LIST_TRUNCATED|"):
			result.Truncated = true
		case strings.HasPrefix(line, "COMMAND_LINE|FILE|"):
			var file constandstruct.RemoteFile
			if err := decodeRemoteJSON(strings.TrimPrefix(line, "COMMAND_LINE|FILE|"), &file); err != nil {
				return result, fmt.Errorf("повреждена запись файла: %w", err)
			}
			file.Name = strings.TrimSpace(file.Name)
			file.FullPath = strings.TrimSpace(file.FullPath)
			file.Extension = strings.ToLower(strings.TrimSpace(file.Extension))
			if file.Name == "" || file.FullPath == "" || file.Size < 0 {
				continue
			}
			key := strings.ToLower(file.FullPath)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			file.IsText = isEditableTextFile(file.FullPath)
			result.Files = append(result.Files, file)
		}
	}
	if !confirmed {
		return result, fmt.Errorf("ответ не содержит подтверждения завершения сканирования")
	}
	return result, nil
}

func parseRemoteTextDocument(lines []string) (constandstruct.RemoteTextDocument, error) {
	var result constandstruct.RemoteTextDocument
	if text := remoteError(lines); text != "" {
		return result, fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}
	confirmed := false
	found := false
	for _, line := range lines {
		if line == "COMMAND_OK" {
			confirmed = true
			continue
		}
		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|DOCUMENT|")
		if !ok {
			continue
		}
		var wire struct {
			File           constandstruct.RemoteFile `json:"File"`
			ContentBase64  string                    `json:"ContentBase64"`
			Encoding       string                    `json:"Encoding"`
			OriginalSHA256 string                    `json:"OriginalSHA256"`
		}
		if err := decodeRemoteJSON(payload, &wire); err != nil {
			return result, fmt.Errorf("повреждён ответ документа: %w", err)
		}
		content, err := base64.StdEncoding.DecodeString(wire.ContentBase64)
		if err != nil {
			return result, fmt.Errorf("повреждено содержимое документа: %w", err)
		}
		if len(content) > MaxEditableFileBytes || !validDocumentEncoding(wire.Encoding) || !isSHA256(wire.OriginalSHA256) {
			return result, fmt.Errorf("удалённый компьютер вернул недопустимые сведения о документе")
		}
		result = constandstruct.RemoteTextDocument{
			File: wire.File, Content: string(content), Encoding: strings.ToLower(wire.Encoding),
			OriginalSHA256: strings.ToLower(wire.OriginalSHA256),
		}
		found = true
	}
	if !confirmed || !found {
		return result, fmt.Errorf("ответ не содержит подтверждения чтения документа")
	}
	return result, nil
}

func parseRemoteSaveResult(lines []string) (constandstruct.RemoteSaveResult, error) {
	var result constandstruct.RemoteSaveResult
	if text := remoteError(lines); text != "" {
		return result, fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}
	confirmed := false
	found := false
	for _, line := range lines {
		if line == "COMMAND_OK" {
			confirmed = true
			continue
		}
		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|FILE_SAVED|")
		if !ok {
			continue
		}
		if err := decodeRemoteJSON(payload, &result); err != nil {
			return result, fmt.Errorf("повреждено подтверждение сохранения: %w", err)
		}
		found = true
	}
	if !confirmed || !found || result.File.FullPath == "" || result.BackupPath == "" {
		return result, fmt.Errorf("ответ не содержит подтверждения сохранения документа")
	}
	return result, nil
}

func parseRemoteDeleteResult(lines []string, expectedPath string) error {
	if text := remoteError(lines); text != "" {
		return fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}
	confirmed := false
	deletedPath := ""
	for _, line := range lines {
		if line == "COMMAND_OK" {
			confirmed = true
			continue
		}
		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|FILE_DELETED|")
		if !ok {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return fmt.Errorf("повреждено подтверждение удаления файла: %w", err)
		}
		deletedPath = string(data)
	}
	if !confirmed || deletedPath == "" || !strings.EqualFold(filepath.Clean(deletedPath), filepath.Clean(expectedPath)) {
		return fmt.Errorf("ответ не содержит подтверждения удаления выбранного файла")
	}
	return nil
}

func decodeRemoteJSON(payload string, destination any) error {
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}
