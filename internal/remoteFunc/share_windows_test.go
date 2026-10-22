//go:build windows

package remotefunc

import "testing"

func TestRemotePathToAdminUNC(t *testing.T) {
	root, path, err := remotePathToAdminUNC("192.168.20.31", `C:\Users\employee\Documents\report.xlsx`)
	if err != nil {
		t.Fatalf("remotePathToAdminUNC() error = %v", err)
	}
	if root != `\\192.168.20.31\C$` {
		t.Fatalf("root = %q", root)
	}
	if path != `\\192.168.20.31\C$\Users\employee\Documents\report.xlsx` {
		t.Fatalf("path = %q", path)
	}
}

func TestRemotePathToAdminUNCRejectsUnsafePath(t *testing.T) {
	for _, path := range []string{`Documents\report.xlsx`, `\\fileserver\share\report.xlsx`, `C:\`} {
		if _, _, err := remotePathToAdminUNC("192.168.20.31", path); err == nil {
			t.Fatalf("remotePathToAdminUNC(%q) должен вернуть ошибку", path)
		}
	}
}
