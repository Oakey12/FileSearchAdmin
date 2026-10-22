package remotefunc

import (
	"os"
	"testing"
	"time"
)

func TestCleanupTempScripts(t *testing.T) {
	path, err := writeRemoteScript()
	if err != nil {
		t.Fatalf("writeRemoteScript: %v", err)
	}

	CleanupTempScripts()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary script still exists: %s, error: %v", path, err)
	}
}

func TestCleanupStaleTempScripts(t *testing.T) {
	file, err := os.CreateTemp("", "fsadmin-stale-test-*.ps1")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := file.Name()
	if err = file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	oldTime := time.Now().Add(-time.Hour)
	if err = os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	CleanupStaleTempScripts(30 * time.Minute)
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale temporary script still exists: %s, error: %v", path, err)
	}
}
