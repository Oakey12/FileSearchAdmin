//go:build windows

package nativedialog

import (
	"testing"
	"unsafe"
)

func TestPowerShellFilterHasDoubleNullTerminator(t *testing.T) {
	filter := utf16WithNulls("PowerShell scripts (*.ps1)\x00*.ps1\x00")
	if len(filter) < 2 || filter[len(filter)-1] != 0 || filter[len(filter)-2] != 0 {
		t.Fatalf("фильтр не завершён двойным NUL: %#v", filter)
	}
}

func TestOpenFileNameStructureSize(t *testing.T) {
	want := uintptr(152)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 88
	}
	if got := unsafe.Sizeof(openFileName{}); got != want {
		t.Fatalf("размер OPENFILENAMEW = %d, want %d", got, want)
	}
}
