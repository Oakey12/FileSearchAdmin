package ops

import (
	"encoding/base64"
	"encoding/json"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseRemoteFolderResult(t *testing.T) {
	file := constandstruct.RemoteFile{
		Name: "settings.json", FullPath: `C:\Config\settings.json`, Extension: ".json",
		Size: 123, LastWriteTimeUTC: "2026-08-31T10:00:00Z", IsText: true,
	}
	line := "COMMAND_LINE|FILE|" + encodeRemoteTestJSON(t, file)
	result, err := parseRemoteFolderResult([]string{line, line, "COMMAND_LINE|FILE_LIST_TRUNCATED|300", "COMMAND_OK"})
	if err != nil {
		t.Fatalf("parseRemoteFolderResult() error = %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].FullPath != file.FullPath || !result.Truncated {
		t.Fatalf("parseRemoteFolderResult() = %#v", result)
	}
}

func TestParseRemoteTextDocument(t *testing.T) {
	wire := struct {
		File           constandstruct.RemoteFile `json:"File"`
		ContentBase64  string                    `json:"ContentBase64"`
		Encoding       string                    `json:"Encoding"`
		OriginalSHA256 string                    `json:"OriginalSHA256"`
	}{
		File:          constandstruct.RemoteFile{Name: "test.txt", FullPath: `C:\Temp\test.txt`, IsText: true},
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("Привет")),
		Encoding:      "utf-8", OriginalSHA256: strings.Repeat("a", 64),
	}
	document, err := parseRemoteTextDocument([]string{
		"COMMAND_LINE|DOCUMENT|" + encodeRemoteTestJSON(t, wire),
		"COMMAND_OK",
	})
	if err != nil {
		t.Fatalf("parseRemoteTextDocument() error = %v", err)
	}
	if document.Content != "Привет" || document.Encoding != "utf-8" {
		t.Fatalf("parseRemoteTextDocument() = %#v", document)
	}
}

func TestParseRemoteSaveResult(t *testing.T) {
	want := constandstruct.RemoteSaveResult{
		File:       constandstruct.RemoteFile{Name: "test.txt", FullPath: `C:\Temp\test.txt`, Size: 10},
		BackupPath: `C:\Temp\test.txt.1cproc-backup-20260831.bak`,
	}
	result, err := parseRemoteSaveResult([]string{
		"COMMAND_LINE|FILE_SAVED|" + encodeRemoteTestJSON(t, want),
		"COMMAND_OK",
	})
	if err != nil || result.BackupPath != want.BackupPath {
		t.Fatalf("parseRemoteSaveResult() = %#v, %v", result, err)
	}
}

func TestRemoteFileScriptsParseInWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверка предназначена для Windows PowerShell")
	}
	document := constandstruct.RemoteTextDocument{
		File:     constandstruct.RemoteFile{FullPath: `C:\Temp\settings.json`},
		Encoding: "utf-8", OriginalSHA256: strings.Repeat("a", 64),
	}
	for name, script := range map[string]string{
		"folder scan": buildFolderScanScript(`C:\Temp`),
		"file read":   buildReadTextFileScript(`C:\Temp\settings.json`),
		"file save":   buildSaveTextFileScript(document, "{}"),
		"file delete": buildDeleteRemoteFileScript(constandstruct.RemoteFile{
			FullPath: `C:\Temp\settings.json`, Size: 2, LastWriteTimeUTC: "2026-08-31T10:00:00Z",
		}),
	} {
		t.Run(name, func(t *testing.T) {
			command := exec.Command(
				"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
				`$source=[Console]::In.ReadToEnd(); $tokens=$null; $errors=$null; [Management.Automation.Language.Parser]::ParseInput($source,[ref]$tokens,[ref]$errors) | Out-Null; if ($errors.Count -gt 0) { $errors | ForEach-Object { $_.Message }; exit 1 }`,
			)
			command.Stdin = strings.NewReader(script)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("PowerShell-скрипт содержит синтаксическую ошибку: %v\n%s", err, output)
			}
		})
	}
}

func TestEditableTextFileAllowlist(t *testing.T) {
	for _, path := range []string{`C:\Temp\test.txt`, `C:\Temp\settings.JSON`, `C:\Temp\script.ps1`} {
		if !isEditableTextFile(path) {
			t.Fatalf("текстовый файл %q отклонён", path)
		}
	}
	for _, path := range []string{`C:\Temp\program.exe`, `C:\Temp\document.docx`, `C:\Temp\archive.zip`} {
		if isEditableTextFile(path) {
			t.Fatalf("бинарный файл %q принят как текстовый", path)
		}
	}
}

func TestSaveScriptDoesNotEmbedPlainDocumentContent(t *testing.T) {
	document := constandstruct.RemoteTextDocument{
		File:     constandstruct.RemoteFile{FullPath: `C:\Temp\test.txt`},
		Encoding: "utf-8", OriginalSHA256: strings.Repeat("a", 64),
	}
	script := buildSaveTextFileScript(document, "SECRET-DOCUMENT-CONTENT")
	if strings.Contains(script, "SECRET-DOCUMENT-CONTENT") {
		t.Fatal("содержимое документа не должно встраиваться в сценарий открытым текстом")
	}
	for _, required := range []string{"SHA256", "File]::Replace", "BackupPath", "FILE_SAVED|"} {
		if !strings.Contains(script, required) {
			t.Fatalf("сценарий сохранения должен содержать %q", required)
		}
	}
}

func TestParseRemoteDeleteResult(t *testing.T) {
	path := `C:\Temp\test.txt`
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	if err := parseRemoteDeleteResult([]string{"COMMAND_LINE|FILE_DELETED|" + payload, "COMMAND_OK"}, path); err != nil {
		t.Fatalf("подтверждение удаления отклонено: %v", err)
	}
	if err := parseRemoteDeleteResult([]string{"COMMAND_OK"}, path); err == nil {
		t.Fatal("ответ без FILE_DELETED принят")
	}
	if err := parseRemoteDeleteResult([]string{"COMMAND_LINE|FILE_DELETED|" + payload, "COMMAND_OK"}, `C:\Temp\other.txt`); err == nil {
		t.Fatal("подтверждение удаления другого файла принято")
	}
}

func TestDeleteScriptChecksScannedMetadata(t *testing.T) {
	file := constandstruct.RemoteFile{
		Name: "SECRET-FILE-NAME.txt", FullPath: `C:\Temp\SECRET-FILE-NAME.txt`,
		Size: 42, LastWriteTimeUTC: "2026-08-31T10:00:00Z",
	}
	script := buildDeleteRemoteFileScript(file)
	for _, required := range []string{"$expectedSize", "$expectedLastWrite", "LastWriteTimeUtc.Ticks", "Remove-Item -LiteralPath", "FILE_DELETED|"} {
		if !strings.Contains(script, required) {
			t.Fatalf("сценарий удаления должен содержать %q", required)
		}
	}
	if strings.Contains(script, file.FullPath) {
		t.Fatal("путь удаляемого файла не должен встраиваться в сценарий открытым текстом")
	}
}

func TestRemoteFileScriptsRoundTripLocally(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("проверка предназначена для Windows PowerShell")
	}
	directory := t.TempDir()
	filePath := filepath.Join(directory, "settings.txt")
	if err := os.WriteFile(filePath, []byte("исходный текст"), 0o600); err != nil {
		t.Fatal(err)
	}

	folderLines := runLocalRemoteFileScript(t, buildFolderScanScript(directory))
	folderResult, err := parseRemoteFolderResult(folderLines)
	if err != nil || len(folderResult.Files) != 1 {
		t.Fatalf("сканирование папки = %#v, %v; строки: %#v", folderResult, err, folderLines)
	}

	readLines := runLocalRemoteFileScript(t, buildReadTextFileScript(filePath))
	document, err := parseRemoteTextDocument(readLines)
	if err != nil || document.Content != "исходный текст" {
		t.Fatalf("чтение документа = %#v, %v", document, err)
	}

	saveLines := runLocalRemoteFileScript(t, buildSaveTextFileScript(document, "изменённый текст"))
	saveResult, err := parseRemoteSaveResult(saveLines)
	if err != nil {
		t.Fatalf("сохранение документа = %#v, %v", saveResult, err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil || string(content) != "изменённый текст" {
		t.Fatalf("сохранённое содержимое = %q, %v", content, err)
	}
	if _, err := os.Stat(saveResult.BackupPath); err != nil {
		t.Fatalf("резервная копия не создана: %v", err)
	}

	deleteLines := runLocalRemoteFileScript(t, buildDeleteRemoteFileScript(saveResult.File))
	if err := parseRemoteDeleteResult(deleteLines, filePath); err != nil {
		t.Fatalf("удаление файла = %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("файл остался после удаления: %v", err)
	}
}

func runLocalRemoteFileScript(t *testing.T, script string) []string {
	t.Helper()
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("локальный PowerShell завершился с ошибкой: %v\n%s", err, output)
	}
	lines := []string{"COMMAND_OK"}
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, "COMMAND_LINE|"+line)
		}
	}
	return lines
}

func encodeRemoteTestJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}
