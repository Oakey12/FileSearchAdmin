package auxiliaryfunc

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// LogUIOnly показывает чувствительный вывод только в окне и не пишет его в файл.
func TestLoggerUIOnlyDoesNotWriteSensitiveOutputToFile(t *testing.T) {
	var file bytes.Buffer
	var got []string
	logger := &Logger{
		fileLogger: log.New(&file, "", 0),
		ui:         func(line string) { got = append(got, line) },
	}

	logger.LogUIOnly("секретный результат")

	if file.Len() != 0 {
		t.Fatalf("чувствительный вывод попал в файл: %q", file.String())
	}
	if !reflect.DeepEqual(got, []string{"секретный результат"}) {
		t.Fatalf("неожиданный UI-журнал: %#v", got)
	}
}

func TestRotateLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FileSearchAdmin.log")
	data := make([]byte, maxLogFileBytes)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := rotateLogFile(path); err != nil {
		t.Fatalf("rotateLogFile: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("ротированный журнал не найден: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("исходный журнал должен быть перемещён, ошибка: %v", err)
	}
}

// Файловый и экранный журнал получают строки в одном и том же порядке даже при
// параллельной работе нескольких серверов.
func TestLoggerFileAndUIOrderMatchUnderConcurrency(t *testing.T) {
	var file bytes.Buffer
	var got []string
	logger := &Logger{
		fileLogger: log.New(&file, "", 0),
		ui:         func(line string) { got = append(got, line) },
	}

	var waitGroup sync.WaitGroup
	for index := 0; index < 100; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			logger.LogLine(fmt.Sprintf("событие-%d", index))
		}(index)
	}
	waitGroup.Wait()

	fileLines := strings.Split(strings.TrimSpace(file.String()), "\n")
	if !reflect.DeepEqual(fileLines, got) {
		t.Fatalf("порядок UI не совпадает с файлом:\nfile=%#v\nui=%#v", fileLines, got)
	}
	if len(got) != 100 {
		t.Fatalf("получено %d строк вместо 100", len(got))
	}
}

// Logger без UI-колбэка только пишет в файл и не паникует.
func TestLoggerNilUIWritesFileOnly(t *testing.T) {
	var file bytes.Buffer
	logger := &Logger{fileLogger: log.New(&file, "", 0)}
	logger.LogLine("строка")
	if !strings.Contains(file.String(), "строка") {
		t.Fatalf("строка не записана в файл: %q", file.String())
	}
}
