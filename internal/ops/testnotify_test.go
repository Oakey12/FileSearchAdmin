package ops

import (
	"context"
	"strings"
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

type notifyCall struct {
	ip      string
	message string
	dryRun  bool
}

func stubNotify(t *testing.T, reply func(notifyCall) []string) *[]notifyCall {
	t.Helper()
	original := runRemoteNotify
	calls := &[]notifyCall{}
	runRemoteNotify = func(ctx context.Context, ip, user, password, message string, dryRun bool, progress remotefunc.ProgressFunc) ([]string, error) {
		call := notifyCall{ip: ip, message: message, dryRun: dryRun}
		*calls = append(*calls, call)
		return reply(call), nil
	}
	t.Cleanup(func() { runRemoteNotify = original })
	return calls
}

// Тестовое сообщение идёт тем же путём с dryRun=true; журнал перечисляет
// получателей и ни разу не говорит «отправлено».
func TestTestNotifyRunsDryPipeline(t *testing.T) {
	calls := stubNotify(t, func(notifyCall) []string {
		return []string{
			"NOTIFY_RECIPIENT|2|ivanov|Active",
			"NOTIFY_RECIPIENT|3|petrov|Диск",
			"NOTIFY_PROBE|ok|420|",
			"NOTIFY_DRY|ok",
		}
	})
	sink, journal := collectJournal()
	targets := []constandstruct.Server{{Name: "TERM2", IP: "192.168.20.31"}}
	if !TestNotify(context.Background(), targets, `CORP\admin`, "пароль", `Закройте "1С"`, sink) {
		t.Fatal("тест не запустился")
	}
	WaitForOperations(context.Background())

	if len(*calls) != 1 || !(*calls)[0].dryRun {
		t.Fatalf("ожидали один вызов с dryRun: %#v", *calls)
	}
	text := journal()
	mustContain(t, text,
		"Закройте '1С'",
		"msg.exe на сервере запускается (420 мс)",
		"✓ сообщение дойдёт — сеансов 2: «ivanov» (сеанс 2), «petrov» (сеанс 3; сеанс отключён",
		"дойдёт на серверах 1 из 1, получателей 2. Ничего не отправлено",
	)
	if strings.Contains(text, "отправлено всем") {
		t.Fatalf("тест не должен сообщать об отправке:\n%s", text)
	}
}

func TestNotifyRealPipelineHasNoDryRun(t *testing.T) {
	calls := stubNotify(t, func(notifyCall) []string { return []string{"NOTIFY_OK|ALL|1"} })
	sink, journal := collectJournal()
	Notify(context.Background(), []constandstruct.Server{{Name: "A", IP: "10.0.0.1"}}, `CORP\admin`, "пароль", "текст", sink)
	WaitForOperations(context.Background())
	if len(*calls) != 1 || (*calls)[0].dryRun {
		t.Fatalf("рассылка должна идти без dryRun: %#v", *calls)
	}
	mustContain(t, journal(), "оповещение отправлено всем пользовательским сессиям")
}

func TestNotifyDryResultMsgExeMissing(t *testing.T) {
	sink, journal := collectJournal()
	summary := logNotifyDryResult(constandstruct.Server{Name: "TS"}, []string{
		"NOTIFY_RECIPIENT|2|ivanov|Active",
		"NOTIFY_PROBE|fail|30|запуск не выполнен: путь не найден (код 9)",
		"NOTIFY_DRY|ok",
	}, sink)
	if summary.ready != 0 || summary.failed != 1 {
		t.Fatalf("итог: %+v", summary)
	}
	mustContain(t, journal(), "✗ сообщение НЕ дойдёт — msg.exe не запускается: msg.exe не найден на сервере")
}

func TestNotifyDryResultNoSessions(t *testing.T) {
	sink, journal := collectJournal()
	summary := logNotifyDryResult(constandstruct.Server{Name: "TS"}, []string{
		"NOTIFY_WARN|список сеансов через quser не получен (доступ запрещён), получатели определены по процессам",
		"NOTIFY_PROBE|ok|100|",
		"NOTIFY_DRY|ok",
	}, sink)
	if summary.ready != 1 || summary.recipients != 0 {
		t.Fatalf("итог: %+v", summary)
	}
	mustContain(t, journal(), "внимание — список сеансов через quser не получен", "получить некому")
}
