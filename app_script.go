package main

// Выбор и чтение .ps1-файла для вкладки «Команды». Диалог даёт только путь,
// поэтому файл читается здесь, в Go, с теми же ограничениями и кодировками,
// что и в 1Cproc: UTF-8, UTF-8 BOM, UTF-16 с BOM, до MaxRemoteScriptBytes.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf16"
	"unicode/utf8"

	remotefunc "filesearch-admin/internal/remoteFunc"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScriptFile — прочитанный сценарий: имя для показа и текст для выполнения.
type ScriptFile struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// PickAndReadScript открывает диалог выбора .ps1 и возвращает его содержимое.
// Пустое имя (и пустой текст) — пользователь отменил выбор.
func (a *App) PickAndReadScript() (ScriptFile, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите сценарий PowerShell",
		Filters: []runtime.FileFilter{
			{DisplayName: "Сценарий PowerShell (*.ps1)", Pattern: "*.ps1"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return ScriptFile{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return ScriptFile{}, err
	}
	defer file.Close()

	text, err := readPowerShellScript(file)
	if err != nil {
		return ScriptFile{}, err
	}
	return ScriptFile{Name: baseName(path), Text: text}, nil
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '\\' || path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func readPowerShellScript(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, remotefunc.MaxRemoteScriptBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > remotefunc.MaxRemoteScriptBytes {
		return "", fmt.Errorf("файл превышает допустимый размер %d байт", remotefunc.MaxRemoteScriptBytes)
	}
	if len(data) == 0 {
		return "", errors.New("файл пуст")
	}

	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		data = data[3:]
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], binary.LittleEndian)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], binary.BigEndian)
	}
	if !utf8.Valid(data) {
		return "", errors.New("поддерживаются UTF-8, UTF-8 BOM и UTF-16 с BOM")
	}
	return string(data), nil
}

func decodeUTF16(data []byte, order binary.ByteOrder) (string, error) {
	if len(data)%2 != 0 {
		return "", errors.New("повреждённый UTF-16 файл")
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = order.Uint16(data[index*2:])
	}
	return string(utf16.Decode(units)), nil
}
