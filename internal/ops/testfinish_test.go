package ops

import (
	"context"
	"strings"
	"sync"
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

type terminateCall struct {
	ip     string
	pids   []int
	dryRun bool
}

// stubTerminate подменяет вызов сценария завершения: запоминает вызовы и
// отвечает заданными строками.
func stubTerminate(t *testing.T, reply func(call terminateCall) []string) *[]terminateCall {
	t.Helper()
	original := runRemoteTerminate
	var mu sync.Mutex
	calls := &[]terminateCall{}
	runRemoteTerminate = func(ctx context.Context, ip, user, password string, pids []int, dryRun bool, progress remotefunc.ProgressFunc) ([]string, error) {
		call := terminateCall{ip: ip, pids: append([]int(nil), pids...), dryRun: dryRun}
		mu.Lock()
		*calls = append(*calls, call)
		mu.Unlock()
		return reply(call), nil
	}
	t.Cleanup(func() { runRemoteTerminate = original })
	return calls
}

func collectJournal() (func(string), func() string) {
	var mu sync.Mutex
	var lines []string
	sink := func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() }
	joined := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(lines, "\n") }
	return sink, joined
}

// Тест проходит тот же конвейер, что и бой, но с флагом dryRun: до сервера
// доходит именно тестовый режим, журнал группирует по пользователям.
func TestTestFinishRunsDryPipeline(t *testing.T) {
	calls := stubTerminate(t, func(terminateCall) []string {
		return []string{
			`PROBE|taskkill|ok|850|`,
			`PLAN|1000|2|1cv7s.exe|DOMAIN\ivanov`,
			`PLAN|1001|2|1cv7s.exe|DOMAIN\ivanov`,
			`PLAN|1002|3|1cv7s.exe|DOMAIN\petrov`,
		}
	})
	sink, journal := collectJournal()

	targets := []constandstruct.Server{{Name: "Сервер 1", IP: "192.168.0.1"}}
	if !TestFinishProcess(context.Background(), targets, `DOMAIN\admin`, "пароль", sink) {
		t.Fatal("операция не запустилась")
	}
	WaitForOperations(context.Background())

	if len(*calls) != 1 || !(*calls)[0].dryRun || len((*calls)[0].pids) != 0 {
		t.Fatalf("ожидали один тестовый вызов по всем процессам, получили %#v", *calls)
	}
	text := journal()
	for _, want := range []string{
		`у «DOMAIN\ivanov» закрылась бы 1С — сессии 2, процессов 2 (PID 1000, 1001)`,
		`у «DOMAIN\petrov» закрылась бы 1С`,
		"проверка запуска taskkill — работает, отработал за 850 мс",
		"пользователей 2, процессов 3",
		"Ничего не закрыто",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("в журнале нет %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "завершена") {
		t.Fatalf("тест не должен сообщать о завершении:\n%s", text)
	}
}

// Боевое завершение передаёт dryRun=false.
func TestFinishProcessRunsRealPipeline(t *testing.T) {
	calls := stubTerminate(t, func(terminateCall) []string {
		return []string{`KILLED|1000|2|1cv7s.exe|DOMAIN\ivanov|taskkill`}
	})
	sink, journal := collectJournal()

	targets := []constandstruct.Server{{Name: "Сервер 1", IP: "192.168.0.1"}}
	FinishProcess(context.Background(), targets, `DOMAIN\admin`, "пароль", sink)
	WaitForOperations(context.Background())

	if len(*calls) != 1 || (*calls)[0].dryRun {
		t.Fatalf("боевое завершение должно идти без dryRun: %#v", *calls)
	}
	if !strings.Contains(journal(), "завершено 1, не завершено 0, ожидают выхода 0") {
		t.Fatalf("нет итога завершения:\n%s", journal())
	}
}

// Выбранные сотрудники группируются по серверам: один вызов на сервер со всеми
// их PID, без повторов; тест у выбранных тоже идёт с dryRun.
func TestTestFinishEmployeesGroupsBySever(t *testing.T) {
	calls := stubTerminate(t, func(call terminateCall) []string { return []string{`GONE|1|уже не выполняется`} })
	sink, _ := collectJournal()

	employees := []constandstruct.EmployeeProcess{
		{ServerName: "A", ServerIP: "10.0.0.1", Owner: "ivanov", PIDs: []int{30, 10}},
		{ServerName: "A", ServerIP: "10.0.0.1", Owner: "petrov", PIDs: []int{10, 20}},
		{ServerName: "B", ServerIP: "10.0.0.2", Owner: "sidorov", PIDs: []int{5}},
	}
	if !TestFinishEmployees(context.Background(), employees, `DOMAIN\admin`, "пароль", sink) {
		t.Fatal("операция не запустилась")
	}
	WaitForOperations(context.Background())

	byIP := map[string]terminateCall{}
	for _, call := range *calls {
		byIP[call.ip] = call
	}
	if len(byIP) != 2 {
		t.Fatalf("ожидали по одному вызову на сервер, получили %#v", *calls)
	}
	if got := joinInts(byIP["10.0.0.1"].pids); got != "10, 20, 30" || !byIP["10.0.0.1"].dryRun {
		t.Fatalf("сервер A: PID %q, dryRun=%v", got, byIP["10.0.0.1"].dryRun)
	}
	if got := joinInts(byIP["10.0.0.2"].pids); got != "5" {
		t.Fatalf("сервер B: PID %q", got)
	}
}

// Разбор боевого ответа: группы по пользователям со способами, отказы с
// причинами, «принято, но не вышел» отдельно.
func TestLogTerminateResultReal(t *testing.T) {
	sink, journal := collectJournal()
	target := constandstruct.Server{Name: "TS1", IP: "10.0.0.1"}
	summary := logTerminateResult(target, []string{
		`STEP_WARN|taskkill|запуск не выполнен: доступ запрещён (код 2)`,
		`KILLED|100|2|1cv7s.exe|DOMAIN\ivanov|taskkill`,
		`KILLED|101|2|1cv7s.exe|DOMAIN\ivanov|Terminate`,
		`KILLED|200|3|1cv7s.exe|DOMAIN\petrov|Terminate`,
		`KILL_FAIL|300|DOMAIN\sidorov|доступ запрещён (код 2)`,
		`KILL_PENDING|400|DOMAIN\kozlov|завершение принято, но процесс не вышел за 8 с`,
	}, false, sink)

	if summary.killed != 3 || summary.failed != 1 || summary.pending != 1 || summary.users != 2 {
		t.Fatalf("неверный итог: %+v", summary)
	}
	text := journal()
	for _, want := range []string{
		"способ taskkill не сработал (запуск не выполнен: доступ запрещён (код 2))",
		`у «DOMAIN\ivanov» 1С завершена — процессов 2 (PID 100 — taskkill, PID 101 — Terminate)`,
		`у «DOMAIN\petrov» 1С завершена — процессов 1 (PID 200; способ: Terminate)`,
		`НЕ завершён PID 300 у «DOMAIN\sidorov»: доступ запрещён (код 2)`,
		`PID 400 у «DOMAIN\kozlov» — завершение принято`,
		"итог — завершено 3, не завершено 1, ожидают выхода 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("в журнале нет %q:\n%s", want, text)
		}
	}
}

// Если основной процесс 1С не найден, но запущены другие её варианты, журнал
// прямо говорит, что они не входят в список завершения.
func TestLogTerminateResultWarnsAboutOtherOneCVariants(t *testing.T) {
	sink, journal := collectJournal()
	logTerminateResult(constandstruct.Server{Name: "TS2"}, []string{
		"VARIANT|1cv7.exe|3",
		"NONE_FOUND",
	}, true, sink)

	text := journal()
	for _, want := range []string{
		"процессов " + constandstruct.TargetProcessNames + " не найдено",
		"НЕ входят в список завершения: 1cv7.exe ×3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("в журнале нет %q:\n%s", want, text)
		}
	}
}

// Провал пробного запуска в тесте объясняет, что будет использован резервный способ.
func TestLogTerminateResultProbeFailure(t *testing.T) {
	sink, journal := collectJournal()
	logTerminateResult(constandstruct.Server{Name: "TS3"}, []string{
		"PROBE|taskkill|fail|0|запуск не выполнен: доступ запрещён (код 2)",
		`PLAN|7|1|1cv7s.exe|DOMAIN\ivanov`,
	}, true, sink)

	if text := journal(); !strings.Contains(text, "НЕ запускается: запуск не выполнен: доступ запрещён (код 2)") ||
		!strings.Contains(text, "резервным способом") {
		t.Fatalf("нет объяснения провала проверки:\n%s", text)
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
