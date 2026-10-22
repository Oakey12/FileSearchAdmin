//go:build windows

package nativedialog

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	replaceFileWriteThrough = 0x00000001
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var (
	kernel32Replace = syscall.NewLazyDLL("kernel32.dll")
	replaceFileW    = kernel32Replace.NewProc("ReplaceFileW")
	moveFileExW     = kernel32Replace.NewProc("MoveFileExW")
)

// ReplaceFileWithoutBackup атомарно подменяет originalPath файлом
// replacementPath без создания постоянной резервной копии. Сначала
// используется ReplaceFileW, затем совместимый fallback MoveFileExW.
func ReplaceFileWithoutBackup(originalPath, replacementPath string) error {
	originalPtr, err := syscall.UTF16PtrFromString(originalPath)
	if err != nil {
		return err
	}
	replacementPtr, err := syscall.UTF16PtrFromString(replacementPath)
	if err != nil {
		return err
	}
	result, _, replaceErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(originalPtr)),
		uintptr(unsafe.Pointer(replacementPtr)),
		0,
		replaceFileWriteThrough,
		0,
		0,
	)
	runtime.KeepAlive(originalPtr)
	runtime.KeepAlive(replacementPtr)
	if result != 0 {
		return nil
	}

	result, _, moveErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(replacementPtr)),
		uintptr(unsafe.Pointer(originalPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	runtime.KeepAlive(originalPtr)
	runtime.KeepAlive(replacementPtr)
	if result != 0 {
		return nil
	}
	return fmt.Errorf("Windows не заменил исходный документ: ReplaceFileW=%v; MoveFileExW=%v", replaceErr, moveErr)
}
