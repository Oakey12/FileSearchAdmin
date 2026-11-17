package ops

import (
	"context"
	"strings"
	"sync"
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

// Сухой прогон закрытия использует безопасное действие "find", группирует по
// пользователям и ничего не завершает.
func TestTestFinishPreviewsWithoutKilling(t *testing.T) {
	original := runRemoteWithProgress
	var actionMu sync.Mutex
	var gotAction string
	runRemoteWithProgress = func(ctx context.Context, ip, user, password, action, arg, payload string, progress remotefunc.ProgressFunc) ([]string, error) {
		actionMu.Lock()
		gotAction = action
		actionMu.Unlock()
		return []string{
			`FOUND|1000|2|1cv7s.exe|DOMAIN\ivanov`,
			`FOUND|1001|2|1cv7s.exe|DOMAIN\ivanov`,
			`FOUND|1002|3|1cv7s.exe|DOMAIN\petrov`,
		}, nil
	}
	defer func() { runRemoteWithProgress = original }()

	var mu sync.Mutex
	var lines []string
	sink := func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() }

	targets := []constandstruct.Server{{Name: "Сервер 1", IP: "192.168.0.1"}}
	if !TestFinishProcess(context.Background(), targets, `DOMAIN\admin`, "пароль", sink) {
		t.Fatal("операция не запустилась")
	}
	WaitForOperations(context.Background())

	actionMu.Lock()
	action := gotAction
	actionMu.Unlock()
	if action != "find" {
		t.Fatalf("тест должен использовать безопасное действие find, а не %q", action)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ivanov", "petrov", "закрылась бы 1С", "пользователей 2, процессов 3", "Ничего не закрыто"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("в журнале нет %q:\n%s", want, joined)
		}
	}
	// Сухой прогон не должен сообщать о фактическом завершении процессов.
	if strings.Contains(joined, "KILLED") || strings.Contains(joined, "завершён ") {
		t.Fatalf("сухой прогон не должен завершать процессы:\n%s", joined)
	}
}

// Пустой список серверов отклоняется до запуска операции.
func TestTestFinishRejectsEmptyTargets(t *testing.T) {
	var lines []string
	sink := func(line string) { lines = append(lines, line) }

	if TestFinishProcess(context.Background(), nil, `DOMAIN\admin`, "пароль", sink) {
		t.Fatal("с пустым списком серверов операция не должна запускаться")
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "сервер") {
		t.Fatalf("ожидали одну ошибку про сервер, получили: %#v", lines)
	}
}
