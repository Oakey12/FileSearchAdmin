package ops

import (
	"context"
	"strings"
	"sync"
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

// Пароль передаётся транспорту, но никогда не попадает в журнал.
func TestNotifyDoesNotLeakPasswordToJournal(t *testing.T) {
	const secret = "СуперСекретныйПароль123"

	original := runRemoteWithProgress
	var gotPassword string
	var once sync.Mutex
	runRemoteWithProgress = func(ctx context.Context, ip, user, password, action, arg, payload string, progress remotefunc.ProgressFunc) ([]string, error) {
		once.Lock()
		gotPassword = password
		once.Unlock()
		return []string{"NOTIFY_OK|ALL|123"}, nil
	}
	defer func() { runRemoteWithProgress = original }()

	var mu sync.Mutex
	var lines []string
	sink := func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() }

	targets := []constandstruct.Server{{Name: "Сервер 1", IP: "192.168.0.1"}}
	Notify(context.Background(), targets, `DOMAIN\admin`, secret, "Закройте 1С", sink)
	WaitForOperations(context.Background())

	once.Lock()
	if gotPassword != secret {
		once.Unlock()
		t.Fatalf("транспорт не получил пароль: %q", gotPassword)
	}
	once.Unlock()

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, secret) {
		t.Fatalf("пароль попал в журнал:\n%s", joined)
	}
	if !strings.Contains(joined, "оповещение отправлено") {
		t.Fatalf("нет подтверждения отправки оповещения:\n%s", joined)
	}
}

// Пустой логин отклоняется в журнал, операция не запускается.
func TestNotifyRejectsEmptyCredentials(t *testing.T) {
	var lines []string
	sink := func(line string) { lines = append(lines, line) }
	targets := []constandstruct.Server{{Name: "Сервер 1", IP: "192.168.0.1"}}

	Notify(context.Background(), targets, "   ", "пароль", "сообщение", sink)

	if len(lines) != 1 || !strings.Contains(lines[0], "логин") {
		t.Fatalf("ожидали одну ошибку про логин, получили: %#v", lines)
	}
}

// Пустой список серверов отклоняется до чтения учётных данных.
func TestNotifyRejectsEmptyTargets(t *testing.T) {
	var lines []string
	sink := func(line string) { lines = append(lines, line) }

	Notify(context.Background(), nil, `DOMAIN\admin`, "пароль", "сообщение", sink)

	if len(lines) != 1 || !strings.Contains(lines[0], "сервер") {
		t.Fatalf("ожидали одну ошибку про сервер, получили: %#v", lines)
	}
}
