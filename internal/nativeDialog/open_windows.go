//go:build windows

package nativedialog

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const shellShowNormal = 1

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = shell32.NewProc("ShellExecuteW")
)

// OpenWithDefaultApplication открывает файл системной ассоциацией Windows.
// Для .docx/.xlsx/.pptx это обычно Word, Excel или PowerPoint.
func OpenWithDefaultApplication(path string) error {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("некорректный путь документа: %w", err)
	}
	verbPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	result, _, _ := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		0,
		shellShowNormal,
	)
	runtime.KeepAlive(pathPtr)
	runtime.KeepAlive(verbPtr)
	if result > 32 {
		return nil
	}
	switch result {
	case 2:
		return fmt.Errorf("файл не найден")
	case 3:
		return fmt.Errorf("папка документа не найдена")
	case 5:
		return fmt.Errorf("Windows отказал в доступе к документу")
	case 31:
		return fmt.Errorf("для этого формата не установлено приложение")
	default:
		return fmt.Errorf("Windows не открыл документ: код ShellExecute %d", result)
	}
}
