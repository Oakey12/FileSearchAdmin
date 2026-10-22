package remotefunc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunRemoteScriptIntegration(t *testing.T) {
	if os.Getenv("ONECPROC_RUN_REMOTE_TESTS") != "1" {
		t.Skip("задайте ONECPROC_RUN_REMOTE_TESTS=1 для проверки на тестовом сервере")
	}
	server := os.Getenv("ONECPROC_TEST_SERVER")
	user := os.Getenv("ONECPROC_TEST_USER")
	password := os.Getenv("ONECPROC_TEST_PASSWORD")
	if server == "" || user == "" || password == "" {
		t.Fatal("для интеграционного теста нужны ONECPROC_TEST_SERVER, ONECPROC_TEST_USER и ONECPROC_TEST_PASSWORD")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	lines, err := RunRemoteScript(ctx, server, user, password, `"CODEX_REMOTE_OK:$env:COMPUTERNAME"`)
	if err != nil {
		t.Fatalf("RunRemoteScript() error = %v", err)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "COMMAND_LINE|CODEX_REMOTE_OK:") {
			return
		}
	}
	t.Fatalf("удалённый маркер не найден: %#v", lines)
}
