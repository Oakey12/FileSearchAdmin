//go:build windows

package nativedialog

import (
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	ofnPathMustExist = 0x00000800
	ofnFileMustExist = 0x00001000
	ofnExplorer      = 0x00080000
	ofnNoChangeDir   = 0x00000008
)

var (
	comdlg32             = syscall.NewLazyDLL("comdlg32.dll")
	getOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	commDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
	user32               = syscall.NewLazyDLL("user32.dll")
	getForegroundWindow  = user32.NewProc("GetForegroundWindow")
)

type openFileName struct {
	structSize      uint32
	owner           uintptr
	instance        uintptr
	filter          *uint16
	customFilter    *uint16
	maxCustomFilter uint32
	filterIndex     uint32
	file            *uint16
	maxFile         uint32
	fileTitle       *uint16
	maxFileTitle    uint32
	initialDir      *uint16
	title           *uint16
	flags           uint32
	fileOffset      uint16
	fileExtension   uint16
	defaultExt      *uint16
	customData      uintptr
	hook            uintptr
	templateName    *uint16
	reserved        unsafe.Pointer
	reservedValue   uint32
	flagsEx         uint32
}

// OpenPowerShellScript открывает системный диалог выбора файла Windows.
// selected=false и err=nil означают, что пользователь нажал «Отмена».
func OpenPowerShellScript() (path string, selected bool, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fileBuffer := make([]uint16, 32768)
	filter := utf16WithNulls("PowerShell scripts (*.ps1)\x00*.ps1\x00All files (*.*)\x00*.*\x00")
	title, err := syscall.UTF16PtrFromString("Выберите сценарий PowerShell")
	if err != nil {
		return "", false, err
	}
	defaultExtension, err := syscall.UTF16PtrFromString("ps1")
	if err != nil {
		return "", false, err
	}
	owner, _, _ := getForegroundWindow.Call()

	request := openFileName{
		owner:       owner,
		filter:      &filter[0],
		filterIndex: 1,
		file:        &fileBuffer[0],
		maxFile:     uint32(len(fileBuffer)),
		title:       title,
		flags:       ofnExplorer | ofnPathMustExist | ofnFileMustExist | ofnNoChangeDir,
		defaultExt:  defaultExtension,
	}
	request.structSize = uint32(unsafe.Sizeof(request))

	result, _, _ := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&request)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(fileBuffer)
	runtime.KeepAlive(title)
	runtime.KeepAlive(defaultExtension)
	if result != 0 {
		return syscall.UTF16ToString(fileBuffer), true, nil
	}

	errorCode, _, _ := commDlgExtendedError.Call()
	if errorCode == 0 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("системный диалог Windows завершился с кодом 0x%X", errorCode)
}

func utf16WithNulls(value string) []uint16 {
	return append(utf16.Encode([]rune(value)), 0)
}
