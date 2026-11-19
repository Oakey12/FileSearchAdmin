package ops

import (
	"context"
	"errors"
	auxiliaryfunc "filesearch-admin/internal/auxiliaryFunc"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"fmt"
	"os/user"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	operationStateMu   sync.Mutex
	operationRunning   bool
	operationCancel    context.CancelFunc
	operationWaitGroup sync.WaitGroup
)

// runRemoteWithProgress — точка подмены для тестов: в бою вызывает реальный
// транспорт через PowerShell/CIM, в тестах заменяется заглушкой.
var runRemoteWithProgress = remotefunc.RunRemoteWithProgress

func beginOperation(parent context.Context, logLine func(string)) (context.Context, bool) {
	operationStateMu.Lock()
	defer operationStateMu.Unlock()
	if operationRunning {
		logLine("Дождитесь завершения текущей операции.")
		return nil, false
	}
	operationCtx, cancel := context.WithCancel(parent)
	operationRunning = true
	operationCancel = cancel
	operationWaitGroup.Add(1)
	return operationCtx, true
}

func finishOperation() {
	operationStateMu.Lock()
	cancel := operationCancel
	operationCancel = nil
	operationRunning = false
	operationStateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	operationWaitGroup.Done()
}

func IsOperationRunning() bool {
	operationStateMu.Lock()
	defer operationStateMu.Unlock()
	return operationRunning
}

func CancelCurrentOperation(logLine func(string)) bool {
	operationStateMu.Lock()
	cancel := operationCancel
	running := operationRunning
	operationStateMu.Unlock()
	if !running || cancel == nil {
		return false
	}
	cancel()
	if logLine != nil {
		logLine("Запрошена отмена текущей операции. Выполняется безопасная очистка...")
	}
	return true
}

func WaitForOperations(ctx context.Context) bool {
	done := make(chan struct{})
	go func() {
		operationWaitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func runTargets(ctx context.Context, targets []constandstruct.Server, run func(constandstruct.Server)) {
	runTargetsLimit(ctx, targets, constandstruct.MaxParallelServers, run)
}

// runTargetsLimit обходит цели, держа одновременно не больше limit удалённых
// вызовов. После отмены ctx новые цели не начинаются.
func runTargetsLimit(ctx context.Context, targets []constandstruct.Server, limit int, run func(constandstruct.Server)) {
	if limit < 1 {
		limit = 1
	}
	semaphore := make(chan struct{}, limit)
	var waitGroup sync.WaitGroup
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		waitGroup.Add(1)
		go func(target constandstruct.Server) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			run(target)
		}(target)
	}
	waitGroup.Wait()
}

func contextErrorText(err error) string {
	if errors.Is(err, context.Canceled) {
		return "операция отменена"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "превышено время ожидания"
	}
	return err.Error()
}

// validateCredentials проверяет логин и пароль, пришедшие из интерфейса.
// Возвращает логин без окружающих пробелов. Пароль не изменяется и в журнал
// не попадает.
func validateCredentials(user, password string, logLine func(string)) (string, string, bool) {
	user = strings.TrimSpace(user)
	if user == "" {
		logLine("Введите логин в формате DOMAIN\\username или .\\localadmin.")
		return "", "", false
	}
	if password == "" {
		logLine("Введите пароль. Пароль не сохраняется и не записывается в журнал.")
		return "", "", false
	}
	return user, password, true
}

func remoteError(lines []string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, "ERROR|") {
			return strings.TrimPrefix(line, "ERROR|")
		}
	}
	return ""
}

func logCallError(operationCtx context.Context, target constandstruct.Server, err error, logLine func(string)) bool {
	if err == nil {
		return false
	}
	if operationCtx.Err() != nil {
		logLine("Операция остановлена: " + contextErrorText(operationCtx.Err()))
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if stage := remotefunc.ErrorStage(err); stage != "" {
			logLine(fmt.Sprintf(
				"%s: превышено время ожидания ответа; последний этап: %s.",
				target.Name, stage,
			))
		} else {
			logLine(fmt.Sprintf("%s: превышено время ожидания ответа.", target.Name))
		}
		return false
	}
	if errors.Is(err, context.Canceled) {
		logLine("Операция отменена.")
		return true
	}
	logLine(fmt.Sprintf("%s: ошибка запуска: %v", target.Name, err))
	return false
}

func logRemoteWarnings(
	target constandstruct.Server,
	lines []string,
	logLine func(string),
) {
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 3)

		switch {
		case len(parts) == 3 &&
			parts[0] == "WARN_OWNER":
			logLine(fmt.Sprintf(
				"%s: GetOwner не определил владельца PID=%s "+
					"(лимит 15 секунд): %s",
				target.Name,
				parts[1],
				parts[2],
			))

		case len(parts) >= 2 &&
			parts[0] == "WARN_QUSER":
			logLine(fmt.Sprintf(
				"%s: не удалось прочитать пользователей "+
					"сеансов через quser: %s",
				target.Name,
				strings.Join(parts[1:], "|"),
			))

		case len(parts) == 2 &&
			(parts[0] == "SESSION_LOOKUP" || parts[0] == "SESSION_FALLBACK"):
			logLine(fmt.Sprintf(
				"%s: сопоставление по пользовательским сеансам: "+
					"найдено пользователей %s.",
				target.Name,
				parts[1],
			))

		case len(parts) == 3 && parts[0] == "SESSION_EXECUTION":
			logLine(fmt.Sprintf(
				"%s: сценарий запущен в пользовательской сессии %s (ID %s).",
				target.Name,
				parts[2],
				parts[1],
			))

		case len(parts) == 3 &&
			parts[0] == "TIMING":
			if parts[1] != "quser" {
				logLine(fmt.Sprintf(
					"%s: %s — %s мс.",
					target.Name,
					parts[1],
					parts[2],
				))
			}
		}
	}
}

// runRemoteTerminate — точка подмены для тестов: в бою вызывает единый
// сценарий завершения на сервере.
var runRemoteTerminate = remotefunc.RunRemoteTerminate

// terminateSummary — итог завершения (или теста) по одному серверу.
type terminateSummary struct {
	users   int // пользователей, у которых 1С завершена (или была бы завершена)
	planned int // тест: процессов, которые были бы завершены
	killed  int
	failed  int
	pending int
}

func (summary *terminateSummary) add(other terminateSummary) {
	summary.users += other.users
	summary.planned += other.planned
	summary.killed += other.killed
	summary.failed += other.failed
	summary.pending += other.pending
}

type ownerProcesses struct {
	pids     []int
	sessions []int
	methods  []string // способ завершения для каждого PID, в том же порядке
}

// logTerminateResult переводит ответ сценария завершения в строки журнала:
// по пользователю на строку, отказы — по процессу с причиной.
func logTerminateResult(
	target constandstruct.Server,
	lines []string,
	dryRun bool,
	logLine func(string),
) terminateSummary {
	var summary terminateSummary
	if text := remoteError(lines); text != "" {
		logLine(fmt.Sprintf("%s: ошибка: %s", target.Name, text))
		return summary
	}
	logRemoteWarnings(target, lines, logLine)

	byOwner := make(map[string]*ownerProcesses)
	var owners, variants []string
	addProcess := func(owner, pidText, sessionText, method string) bool {
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			return false
		}
		session, _ := strconv.Atoi(sessionText)
		group := byOwner[owner]
		if group == nil {
			group = &ownerProcesses{}
			byOwner[owner] = group
			owners = append(owners, owner)
		}
		group.pids = append(group.pids, pid)
		group.methods = append(group.methods, method)
		if !containsInt(group.sessions, session) {
			group.sessions = append(group.sessions, session)
		}
		return true
	}

	noneFound, recognized := false, false
	for _, line := range lines {
		parts := strings.Split(line, "|")
		switch {
		case line == "NONE_FOUND":
			noneFound, recognized = true, true
		case len(parts) == 3 && parts[0] == "VARIANT":
			variants = append(variants, fmt.Sprintf("%s ×%s", parts[1], parts[2]))
		case len(parts) >= 2 && parts[0] == "WARN_VARIANT":
			logLine(fmt.Sprintf("%s: не удалось проверить другие процессы 1С: %s", target.Name, strings.Join(parts[1:], "|")))
		case len(parts) >= 3 && parts[0] == "GONE":
			recognized = true
			logLine(fmt.Sprintf("%s: PID %s — %s.", target.Name, parts[1], strings.Join(parts[2:], "|")))
		case len(parts) >= 4 && parts[0] == "PROBE":
			logProbe(target, parts, logLine)
		case len(parts) >= 3 && parts[0] == "STEP_WARN":
			logLine(fmt.Sprintf("%s: способ %s не сработал (%s) — продолжаю резервным способом.", target.Name, parts[1], strings.Join(parts[2:], "|")))
		case len(parts) == 5 && parts[0] == "PLAN":
			if addProcess(parts[4], parts[1], parts[2], "") {
				recognized = true
				summary.planned++
			}
		case len(parts) == 6 && parts[0] == "KILLED":
			if addProcess(parts[4], parts[1], parts[2], parts[5]) {
				recognized = true
				summary.killed++
			}
		case len(parts) >= 4 && parts[0] == "KILL_FAIL":
			recognized = true
			summary.failed++
			logLine(fmt.Sprintf("%s: НЕ завершён PID %s у «%s»: %s.", target.Name, parts[1], parts[2], strings.Join(parts[3:], "|")))
		case len(parts) >= 4 && parts[0] == "KILL_PENDING":
			recognized = true
			summary.pending++
			logLine(fmt.Sprintf(
				"%s: PID %s у «%s» — %s. Обычно это зависший ввод-вывод (база на сетевой папке, драйвер ключа защиты); проверьте повторным поиском.",
				target.Name, parts[1], parts[2], strings.Join(parts[3:], "|"),
			))
		}
	}

	sort.Strings(owners)
	for _, owner := range owners {
		group := byOwner[owner]
		sort.Ints(group.sessions)
		if dryRun {
			logLine(fmt.Sprintf(
				"%s: у «%s» закрылась бы 1С — сессии %s, процессов %d (PID %s).",
				target.Name, owner, joinInts(group.sessions), len(group.pids), joinInts(group.pids),
			))
		} else {
			logLine(fmt.Sprintf(
				"%s: у «%s» 1С завершена — процессов %d (%s).",
				target.Name, owner, len(group.pids), describeMethods(group),
			))
		}
	}
	summary.users = len(owners)

	if noneFound {
		logLine(fmt.Sprintf("%s: процессов %s не найдено — завершать нечего.", target.Name, constandstruct.TargetProcessNames))
	}
	if len(variants) > 0 {
		logLine(fmt.Sprintf(
			"%s: внимание — запущены другие процессы 1С, которые НЕ входят в список завершения: %s.",
			target.Name, strings.Join(variants, ", "),
		))
	}
	if !recognized {
		logLine(fmt.Sprintf("%s: результат операции не распознан.", target.Name))
		return summary
	}
	if noneFound {
		return summary
	}
	if dryRun {
		logLine(fmt.Sprintf("%s: итог теста — закрылась бы у пользователей %d, процессов %d.", target.Name, summary.users, summary.planned))
	} else {
		logLine(fmt.Sprintf(
			"%s: итог — завершено %d, не завершено %d, ожидают выхода %d.",
			target.Name, summary.killed, summary.failed, summary.pending,
		))
	}
	return summary
}

// logProbe — результат пробного запуска taskkill в тесте.
func logProbe(target constandstruct.Server, parts []string, logLine func(string)) {
	detail := ""
	if len(parts) >= 5 {
		detail = strings.Join(parts[4:], "|")
	}
	switch parts[2] {
	case "ok":
		logLine(fmt.Sprintf("%s: проверка запуска %s — работает, отработал за %s мс.", target.Name, parts[1], parts[3]))
	case "slow":
		logLine(fmt.Sprintf(
			"%s: проверка запуска %s — запускается, но %s; при завершении процессы будут закрыты резервным способом (Win32_Process.Terminate).",
			target.Name, parts[1], detail,
		))
	default:
		logLine(fmt.Sprintf(
			"%s: проверка запуска %s — НЕ запускается: %s; при завершении процессы будут закрыты резервным способом (Win32_Process.Terminate).",
			target.Name, parts[1], detail,
		))
	}
}

// describeMethods — способ завершения: один на всех или по каждому PID.
func describeMethods(group *ownerProcesses) string {
	same := true
	for _, method := range group.methods {
		if method != group.methods[0] {
			same = false
			break
		}
	}
	if same {
		return fmt.Sprintf("PID %s; способ: %s", joinInts(group.pids), group.methods[0])
	}
	parts := make([]string, len(group.pids))
	for index, pid := range group.pids {
		parts[index] = fmt.Sprintf("PID %d — %s", pid, group.methods[index])
	}
	return strings.Join(parts, ", ")
}

// terminateWork — что завершать на одном сервере. Пустой pids — все процессы
// 1С на сервере.
type terminateWork struct {
	server constandstruct.Server
	pids   []int
}

// runTerminateOperation — общий конвейер для завершения и теста, по всем
// процессам или по выбранным PID. Тест и бой отличаются только флагом dryRun,
// который доходит до сценария на сервере.
func runTerminateOperation(
	ctx context.Context,
	work []terminateWork,
	user, password string,
	dryRun bool,
	title string,
	logLine func(string),
) bool {
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.OperationTimeout)
		defer cancel()

		if dryRun {
			logLine("=== Тест завершения 1С: тот же алгоритм, что и при завершении, но процессы не закрываются ===")
		}
		byIP := make(map[string]terminateWork, len(work))
		targets := make([]constandstruct.Server, 0, len(work))
		for _, item := range work {
			byIP[item.server.IP] = item
			targets = append(targets, item.server)
		}
		timeout := constandstruct.MutationServerTimeout
		if dryRun {
			timeout = constandstruct.ServerTimeout
		}

		var totalMu sync.Mutex
		var total terminateSummary
		failedServers := 0
		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			item := byIP[target.IP]
			logLine(fmt.Sprintf("=== %s на %s (%s) ===", title, target.Name, target.IP))
			serverCtx, stop := context.WithTimeout(operationCtx, timeout)
			finishStatus := startRemoteStatus(target, timeout, logLine)
			lines, err := runRemoteTerminate(serverCtx, target.IP, user, password, item.pids, dryRun, remoteProgressLogger(target, logLine))
			finishStatus(err)
			stop()
			if err != nil {
				logCallError(operationCtx, target, err, logLine)
				totalMu.Lock()
				failedServers++
				totalMu.Unlock()
				return
			}
			summary := logTerminateResult(target, lines, dryRun, logLine)
			totalMu.Lock()
			total.add(summary)
			if remoteError(lines) != "" {
				failedServers++
			}
			totalMu.Unlock()
		})

		if operationCtx.Err() != nil {
			logLine("Операция остановлена: " + contextErrorText(operationCtx.Err()))
		}
		if dryRun {
			logLine(fmt.Sprintf(
				"--- Тест закрытия завершён: пользователей %d, процессов %d, серверов с ошибкой %d. Ничего не закрыто (это тест). ---",
				total.users, total.planned, failedServers,
			))
		} else {
			logLine(fmt.Sprintf(
				"--- %s окончено: завершено %d, не завершено %d, ожидают выхода %d, серверов с ошибкой %d ---",
				title, total.killed, total.failed, total.pending, failedServers,
			))
		}
	}()
	return true
}

// serverWork — работа «все процессы» на каждом сервере.
func serverWork(targets []constandstruct.Server) []terminateWork {
	work := make([]terminateWork, 0, len(targets))
	for _, target := range targets {
		work = append(work, terminateWork{server: target})
	}
	return work
}

// employeeWork группирует выбранных сотрудников по серверам: на каждый сервер —
// один вызов со всеми их PID.
func employeeWork(employees []constandstruct.EmployeeProcess) []terminateWork {
	byIP := make(map[string]*terminateWork)
	order := make([]string, 0)
	for _, employee := range employees {
		serverIP := strings.TrimSpace(employee.ServerIP)
		if serverIP == "" {
			continue
		}
		item := byIP[serverIP]
		if item == nil {
			item = &terminateWork{server: constandstruct.Server{Name: employee.ServerName, IP: serverIP}}
			byIP[serverIP] = item
			order = append(order, serverIP)
		}
		for _, pid := range employee.PIDs {
			if pid > 0 && !containsInt(item.pids, pid) {
				item.pids = append(item.pids, pid)
			}
		}
	}
	work := make([]terminateWork, 0, len(order))
	for _, ip := range order {
		if item := byIP[ip]; len(item.pids) > 0 {
			sort.Ints(item.pids)
			work = append(work, *item)
		}
	}
	return work
}

func Search(ctx context.Context, targets []constandstruct.Server,
	user, password string, logLine func(string),
) {
	if len(targets) == 0 {
		logLine("Выберите хотя бы один сервер или введите IP вручную.")
		return
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.OperationTimeout)
		defer cancel()

		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			if operationCtx.Err() != nil {
				logLine("Поиск остановлен: " + contextErrorText(operationCtx.Err()))
				return
			}
			logLine(fmt.Sprintf("=== Поиск на %s (%s) ===", target.Name, target.IP))
			serverCtx, stop := context.WithTimeout(operationCtx, constandstruct.ServerTimeout)
			finishStatus := startRemoteStatus(target, constandstruct.ServerTimeout, logLine)
			lines, err := runRemoteWithProgress(serverCtx, target.IP, user, password, "findfast", constandstruct.TargetProcessNames, "", remoteProgressLogger(target, logLine))
			finishStatus(err)
			stop()
			if logCallError(operationCtx, target, err, logLine) {
				return
			}
			if err != nil {
				return
			}
			if text := remoteError(lines); text != "" {
				logLine(fmt.Sprintf("%s: ошибка: %s", target.Name, text))
				return
			}
			logRemoteWarnings(target, lines, logLine)
			processes := auxiliaryfunc.ParseFoundLines(lines)
			if len(processes) == 0 {
				logLine(fmt.Sprintf("%s: открытых 1cv7s.exe не найдено.", target.Name))
				return
			}
			for _, process := range processes {
				logLine(fmt.Sprintf("%s: %s, PID=%d, сессия=%d",
					target.Name, process.Name, process.PID, process.Session))
			}
		})
		logLine("--- Поиск завершён ---")
	}()
}

// FinishProcess завершает все процессы 1С на выбранных серверах.
func FinishProcess(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password string,
	logLine func(string),
) bool {
	if len(targets) == 0 {
		logLine("Выберите хотя бы один сервер или введите IP вручную.")
		return false
	}
	return runTerminateOperation(ctx, serverWork(targets), user, password, false, "Завершение 1С", logLine)
}

// TestFinishProcess — тест завершения 1С на выбранных серверах: тот же
// конвейер, что и FinishProcess, но на сервере вместо завершения выполняется
// пробный запуск taskkill. Пишет в журнал, у каких пользователей 1С закрылась
// бы и готов ли сервер к завершению. Ничего не завершает.
func TestFinishProcess(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password string,
	logLine func(string),
) bool {
	if len(targets) == 0 {
		logLine("Выберите хотя бы один сервер или введите IP вручную.")
		return false
	}
	return runTerminateOperation(ctx, serverWork(targets), user, password, true, "Тест завершения 1С", logLine)
}

func formatNotificationMessage(login, displayName, message string) string {
	login = strings.TrimSpace(login)
	displayName = strings.TrimSpace(displayName)
	sender := login
	if displayName != "" && !strings.EqualFold(displayName, login) {
		sender = fmt.Sprintf("%s (%s)", login, displayName)
	}
	return fmt.Sprintf("От: %s. %s", sender, strings.TrimSpace(message))
}

func currentUserDisplayName() string {
	current, err := user.Current()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(current.Name)
}

func Notify(ctx context.Context, targets []constandstruct.Server,
	user, password, message string, logLine func(string),
) {
	if len(targets) == 0 {
		logLine("Выберите хотя бы один сервер или введите IP вручную.")
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		logLine("Введите текст оповещения.")
		return
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return
	}
	message = formatNotificationMessage(user, currentUserDisplayName(), message)
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.OperationTimeout)
		defer cancel()
		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			logLine(fmt.Sprintf("=== Оповещение на %s (%s) ===", target.Name, target.IP))
			serverCtx, stop := context.WithTimeout(operationCtx, constandstruct.MutationServerTimeout)
			finishStatus := startRemoteStatus(target, constandstruct.MutationServerTimeout, logLine)
			lines, err := runRemoteWithProgress(serverCtx, target.IP, user, password, "notify", "", message, remoteProgressLogger(target, logLine))
			finishStatus(err)
			stop()
			if logCallError(operationCtx, target, err, logLine) {
				return
			}
			if err != nil {
				return
			}
			if text := remoteError(lines); text != "" {
				logLine(fmt.Sprintf("%s: ошибка: %s", target.Name, text))
				return
			}
			logRemoteWarnings(target, lines, logLine)
			notificationSent := slices.ContainsFunc(lines, func(line string) bool {
				return strings.HasPrefix(line, "NOTIFY_OK|")
			})

			if notificationSent {
				logLine(fmt.Sprintf(
					"%s: оповещение отправлено всем пользовательским сессиям.",
					target.Name,
				))
				return
			}

			for _, line := range lines {
				if strings.HasPrefix(line, "NOTIFY_FAIL|") {
					reason := strings.TrimPrefix(line, "NOTIFY_FAIL|")
					logLine(fmt.Sprintf(
						"%s: не удалось отправить оповещение: %s",
						target.Name,
						reason,
					))
					return
				}
			}

			logLine(fmt.Sprintf(
				"%s: сервер не подтвердил отправку оповещения.",
				target.Name,
			))
		})
		logLine("--- Рассылка завершена ---")
	}()
}

// ScanEmployees ищет сотрудников с открытой 1С на целевых серверах и передаёт
// найденный список в onResult. onResult(nil) вызывается в начале — чтобы
// интерфейс очистил прежний список. Серверы и учётные данные приходят из
// интерфейса.
func ScanEmployees(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password string,
	logLine func(string),
	onResult func([]constandstruct.EmployeeProcess),
) {
	if len(targets) == 0 {
		logLine("Выберите хотя бы один сервер или введите IP вручную.")
		return
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return
	}
	if onResult != nil {
		onResult(nil)
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.OperationTimeout)
		defer cancel()
		employees := make(map[string]*constandstruct.EmployeeProcess)
		var employeesMu sync.Mutex

		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			logLine(fmt.Sprintf("=== Сканирование сотрудников на %s (%s) ===", target.Name, target.IP))
			serverCtx, stop := context.WithTimeout(operationCtx, constandstruct.ServerTimeout)
			finishStatus := startRemoteStatus(target, constandstruct.ServerTimeout, logLine)
			lines, err := runRemoteWithProgress(serverCtx, target.IP, user, password, "find", constandstruct.TargetProcessNames, "", remoteProgressLogger(target, logLine))
			finishStatus(err)
			stop()
			if logCallError(operationCtx, target, err, logLine) {
				return
			}
			if err != nil {
				return
			}
			if text := remoteError(lines); text != "" {
				logLine(fmt.Sprintf("%s: ошибка: %s", target.Name, text))
				return
			}
			logRemoteWarnings(target, lines, logLine)
			processes := auxiliaryfunc.ParseFoundLines(lines)
			logLine(fmt.Sprintf("%s: найдено процессов: %d.", target.Name, len(processes)))
			employeesMu.Lock()
			for _, process := range processes {
				owner := strings.TrimSpace(process.Owner)
				if owner == "" || owner == "неизвестно" {
					owner = "неизвестный пользователь"
				}
				key := target.IP + "|" + strings.ToLower(owner)
				if owner == "неизвестный пользователь" {
					key = fmt.Sprintf("%s|%s|%d", target.IP, owner, process.Session)
				}
				employee := employees[key]
				if employee == nil {
					employee = &constandstruct.EmployeeProcess{
						ServerName: target.Name, ServerIP: target.IP, Owner: owner,
					}
					employees[key] = employee
				}
				if !containsInt(employee.PIDs, process.PID) {
					employee.PIDs = append(employee.PIDs, process.PID)
				}
				if !containsInt(employee.Sessions, process.Session) {
					employee.Sessions = append(employee.Sessions, process.Session)
				}
			}
			employeesMu.Unlock()
		})

		found := make([]constandstruct.EmployeeProcess, 0, len(employees))
		for _, employee := range employees {
			sort.Ints(employee.PIDs)
			sort.Ints(employee.Sessions)
			found = append(found, *employee)
		}
		sort.Slice(found, func(i, j int) bool {
			if found[i].ServerName == found[j].ServerName {
				return found[i].Owner < found[j].Owner
			}
			return found[i].ServerName < found[j].ServerName
		})
		if onResult != nil {
			onResult(found)
		}
		logLine(fmt.Sprintf("=== Сканирование завершено: сотрудников %d ===\n", len(found)))
	}()
}

// FinishEmployeeProcesses завершает 1С у выбранных сотрудников (их PID).
func FinishEmployeeProcesses(
	ctx context.Context,
	employees []constandstruct.EmployeeProcess,
	defaultUser, password string,
	logLine func(string),
) bool {
	return finishEmployees(ctx, employees, defaultUser, password, false, logLine)
}

// TestFinishEmployees — тест завершения 1С у выбранных сотрудников: тот же
// конвейер, процессы не завершаются.
func TestFinishEmployees(
	ctx context.Context,
	employees []constandstruct.EmployeeProcess,
	defaultUser, password string,
	logLine func(string),
) bool {
	return finishEmployees(ctx, employees, defaultUser, password, true, logLine)
}

func finishEmployees(
	ctx context.Context,
	employees []constandstruct.EmployeeProcess,
	defaultUser, password string,
	dryRun bool,
	logLine func(string),
) bool {
	if len(employees) == 0 {
		logLine("Выберите хотя бы одного сотрудника.")
		return false
	}
	work := employeeWork(employees)
	if len(work) == 0 {
		logLine("У выбранных сотрудников нет корректных PID.")
		return false
	}
	title := "Завершение 1С у выбранных"
	if dryRun {
		title = "Тест завершения 1С у выбранных"
	}
	return runTerminateOperation(ctx, work, defaultUser, password, dryRun, title, logLine)
}

func startRemoteStatus(target constandstruct.Server, timeout time.Duration, logLine func(string)) func(error) {
	started := time.Now()
	done := make(chan struct{})
	stopped := make(chan struct{})
	logLine(fmt.Sprintf("%s: удалённая операция запущена, ожидается ответ сервера...", target.Name))

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(started).Round(100 * time.Millisecond)
				logLine(fmt.Sprintf("%s: Операция выполняется, ожидание %s из %s...", target.Name, elapsed, timeout))
			}
		}
	}()

	return func(err error) {
		close(done)
		// Дожидаемся остановки status-goroutine: иначе тик таймера мог попасть в
		// журнал уже после сообщения о фактическом завершении команды.
		<-stopped
		elapsed := time.Since(started).Round(100 * time.Millisecond)
		switch {
		case err == nil:
			logLine(fmt.Sprintf("%s: Сервер вернул ответ за %s.", target.Name, elapsed))
		case errors.Is(err, context.DeadlineExceeded):
			logLine(fmt.Sprintf("%s: Ожидание остановлено по тайм-ауту через %s.", target.Name, elapsed))
		case errors.Is(err, context.Canceled):
			logLine(fmt.Sprintf("%s: Ожидание отменено через %s.", target.Name, elapsed))
		default:
			logLine(fmt.Sprintf("%s: Удалённая команда завершилась с ошибкой через %s.", target.Name, elapsed))
		}
	}
}

func remoteProgressLogger(target constandstruct.Server, logLine func(string)) remotefunc.ProgressFunc {
	return func(line string) {
		if stage, ok := strings.CutPrefix(line, "STAGE|"); ok {
			stage = strings.TrimSpace(stage)
			if stage != "" {
				logLine(fmt.Sprintf("%s: этап — %s.", target.Name, stage))
			}
			return
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 || parts[0] != "TIMING" {
			return
		}
		milliseconds, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || milliseconds < 0 {
			return
		}
		logLine(fmt.Sprintf(
			"%s: этап «%s» завершён за %s.",
			target.Name,
			parts[1],
			(time.Duration(milliseconds) * time.Millisecond).Round(time.Millisecond),
		))
	}
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, ", ")
}
