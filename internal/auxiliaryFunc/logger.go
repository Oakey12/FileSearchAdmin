package auxiliaryfunc

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxLogFileBytes = 10 * 1024 * 1024
	maxLogFiles     = 4 // четыре архива плюс текущий файл
)

// Logger пишет журнал в файл с ротацией и дублирует строки в интерфейс через
// колбэк ui. В отличие от версии на Fyne, колбэк вызывается напрямую: Wails
// runtime.EventsEmit потокобезопасен, отдельная очередь на главный поток не
// нужна. Строки файла и интерфейса идут в одном порядке — оба вывода
// выполняются под одним мьютексом.
type Logger struct {
	mu sync.Mutex

	file       *os.File
	fileLogger *log.Logger
	ui         func(string)
}

// New открывает (создаёт) файл журнала и возвращает логгер. ui может быть nil —
// тогда журнал только файловый.
func New(filePath string, ui func(string)) (*Logger, error) {
	logger := &Logger{ui: ui}

	if err := os.MkdirAll(filepath.Dir(filePath), 0700); err != nil {
		return logger, fmt.Errorf("не удалось создать каталог журнала: %w", err)
	}
	if err := rotateLogFile(filePath); err != nil {
		return logger, err
	}

	file, err := os.OpenFile(
		filePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0600,
	)
	if err != nil {
		return logger, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return logger, err
	}
	if fileInfo.Size() == 0 {
		if _, err = file.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			_ = file.Close()
			return logger, err
		}
	}

	logger.file = file
	logger.fileLogger = log.New(
		file,
		"",
		log.Ldate|log.Ltime,
	)

	return logger, nil
}

// DefaultLogPath — путь к файловому журналу в локальном каталоге пользователя.
func DefaultLogPath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить локальный каталог пользователя: %w", err)
	}
	return filepath.Join(cacheDir, "FileSearchAdmin", "Logs", "FileSearchAdmin.log"), nil
}

func rotateLogFile(filePath string) error {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("не удалось проверить размер журнала: %w", err)
	}
	if info.Size() < maxLogFileBytes {
		return nil
	}

	oldest := fmt.Sprintf("%s.%d", filePath, maxLogFiles)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось удалить старый журнал: %w", err)
	}
	for index := maxLogFiles - 1; index >= 1; index-- {
		from := fmt.Sprintf("%s.%d", filePath, index)
		to := fmt.Sprintf("%s.%d", filePath, index+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("не удалось ротировать журнал: %w", err)
		}
	}
	if err := os.Rename(filePath, filePath+".1"); err != nil {
		return fmt.Errorf("не удалось начать новый журнал: %w", err)
	}
	return nil
}

// LogLine пишет строку и в файл, и в интерфейс.
func (logger *Logger) LogLine(text string) {
	logger.log(text, true)
}

// LogUIOnly показывает потенциально чувствительный результат команды только
// в текущем окне и не сохраняет его в файловом журнале.
func (logger *Logger) LogUIOnly(text string) {
	logger.log(text, false)
}

func (logger *Logger) log(text string, writeToFile bool) {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	if writeToFile && logger.fileLogger != nil {
		logger.fileLogger.Println(text)
	}
	if logger.ui != nil {
		// Тот же мьютекс, что и для файла: экранный журнал получает сообщения
		// строго в том же порядке даже при параллельной работе серверов.
		logger.ui(text)
	}
}

// Close закрывает файл журнала.
func (logger *Logger) Close() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()

	if logger.file == nil {
		return nil
	}

	err := logger.file.Close()

	logger.file = nil
	logger.fileLogger = nil

	return err
}
