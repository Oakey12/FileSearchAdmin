package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	constandstruct "filesearch-admin/internal/constAndStruct"
)

func TestLogRemoteWarnings(t *testing.T) {
	var logs []string
	logRemoteWarnings(constandstruct.Server{Name: "Сервер 2"}, []string{
		"WARN_OWNER|1086764|тайм-аут", "SESSION_LOOKUP|22",
		"WARN_QUSER|Отказано в доступе", "TIMING|Поиск процессов|418",
	}, func(line string) { logs = append(logs, line) })
	if len(logs) != 4 {
		t.Fatalf("получено сообщений: %d", len(logs))
	}
	for i, fragment := range []string{"PID=1086764", "22", "Отказано", "418 мс"} {
		if !strings.Contains(logs[i], fragment) {
			t.Fatalf("%q не содержит %q", logs[i], fragment)
		}
	}
}

func TestRunTargetsHonorsParallelLimit(t *testing.T) {
	targets := make([]constandstruct.Server, constandstruct.MaxParallelServers+3)
	started := make(chan struct{}, len(targets))
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runTargets(context.Background(), targets, func(constandstruct.Server) { started <- struct{}{}; <-release })
		close(done)
	}()
	for i := 0; i < constandstruct.MaxParallelServers; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("задачи не запустились")
		}
	}
	select {
	case <-started:
		t.Fatal("превышен лимит параллельности")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runTargets не завершился")
	}
}

func TestFormatNotificationMessage(t *testing.T) {
	tests := []struct {
		name        string
		login       string
		displayName string
		message     string
		want        string
	}{
		{
			name:        "login and name",
			login:       `SANTEAM\1108`,
			displayName: "Иван Иванов",
			message:     "Закройте 1С",
			want:        `От: SANTEAM\1108 (Иван Иванов). Закройте 1С`,
		},
		{
			name:    "login only",
			login:   `SANTEAM\1108`,
			message: "Закройте 1С",
			want:    `От: SANTEAM\1108. Закройте 1С`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatNotificationMessage(test.login, test.displayName, test.message); got != test.want {
				t.Fatalf("formatNotificationMessage() = %q, want %q", got, test.want)
			}
		})
	}
}
