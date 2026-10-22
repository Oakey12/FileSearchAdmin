package remotefunc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveRemoteUsernameLeavesQualifiedAccount(t *testing.T) {
	for _, username := range []string{`DOMAIN\admin`, `PC-01\admin`, `admin@example.local`} {
		got, err := resolveRemoteUsername(context.Background(), "192.0.2.1", username)
		if err != nil || got != username {
			t.Fatalf("resolveRemoteUsername(%q) = %q, %v", username, got, err)
		}
	}
}

func TestTargetComputerNameCacheExpires(t *testing.T) {
	now := time.Unix(1_000, 0)
	target := "192.0.2.55"
	cacheTargetComputerName(target, "OFFICE-PC", now)

	if got, ok := cachedTargetComputerName(target, now.Add(time.Minute)); !ok || got != "OFFICE-PC" {
		t.Fatalf("cachedTargetComputerName() = %q, %v", got, ok)
	}
	if got, ok := cachedTargetComputerName(target, now.Add(targetComputerNameCacheTTL)); ok || got != "" {
		t.Fatalf("просроченная запись не удалена: %q, %v", got, ok)
	}
}

func TestResolveRemoteUsernameUsesExplicitTargetName(t *testing.T) {
	got, err := resolveRemoteUsername(context.Background(), "office-pc-15.example.local", `.\localadmin`)
	if err != nil {
		t.Fatalf("resolveRemoteUsername() error = %v", err)
	}
	if got != `OFFICE-PC-15\localadmin` {
		t.Fatalf("resolveRemoteUsername() = %q", got)
	}
}

func TestResolveRemoteUsernameRejectsEmptyLocalAccount(t *testing.T) {
	_, err := resolveRemoteUsername(context.Background(), "PC-01", `.\`)
	if err == nil || !strings.Contains(err.Error(), "локального пользователя") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestComputerNameFromNBTStat(t *testing.T) {
	output := []byte(`
       NetBIOS Remote Machine Name Table
       Name               Type         Status
    ---------------------------------------------
    OFFICE-PC-15   <00>  UNIQUE      Registered
    WORKGROUP      <00>  GROUP       Registered
    OFFICE-PC-15   <20>  UNIQUE      Registered
`)
	if got := computerNameFromNBTStat(output); got != "OFFICE-PC-15" {
		t.Fatalf("computerNameFromNBTStat() = %q", got)
	}
}
