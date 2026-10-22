package ops

import (
	"context"
	"errors"
	"fmt"
	auxiliaryfunc "filesearch-admin/internal/auxiliaryFunc"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
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
	limit := constandstruct.MaxParallelServers
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

func logKillResult(target constandstruct.Server, lines []string, logLine func(string)) {
	if text := remoteError(lines); text != "" {
		logLine(fmt.Sprintf("%s: ошибка: %s", target.Name, text))
		return
	}
	logRemoteWarnings(target, lines, logLine)

	if slices.Contains(lines, "NONE_FOUND") {
		logLine(fmt.Sprintf("%s: процессов 1cv7s.exe не найдено.", target.Name))
		return
	}

	recognized := false
	for _, line := range lines {
		parts := strings.Split(line, "|")
		switch {
		case len(parts) == 5 && parts[0] == "KILLED":
			recognized = true
			if parts[4] == "не проверялся" {
				logLine(fmt.Sprintf("%s: завершён %s, PID=%s; владелец повторно не запрашивался.", target.Name, parts[3], parts[1]))
			} else {
				logLine(fmt.Sprintf("%s: завершён %s, PID=%s, пользователь=%s.", target.Name, parts[3], parts[1], parts[4]))
			}
		case len(parts) >= 3 && parts[0] == "KILL_FAIL":
			recognized = true
			logLine(fmt.Sprintf("%s: PID=%s не завершён: %s", target.Name, parts[1], strings.Join(parts[2:], "|")))
		}
	}
	if !recognized {
		logLine(fmt.Sprintf("%s: результат операции не распознан.", target.Name))
	}
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
		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			logLine(fmt.Sprintf("=== Завершение всех 1cv7s.exe на %s (%s) ===", target.Name, target.IP))
			serverCtx, stop := context.WithTimeout(operationCtx, constandstruct.MutationServerTimeout)
			finishStatus := startRemoteStatus(target, constandstruct.MutationServerTimeout, logLine)
			lines, err := runRemoteWithProgress(serverCtx, target.IP, user, password, "kill", constandstruct.TargetProcessNames, "", remoteProgressLogger(target, logLine))
			finishStatus(err)
			stop()
			if logCallError(operationCtx, target, err, logLine) {
				return
			}
			if err == nil {
				logKillResult(target, lines, logLine)
			}
		})
		logLine("--- Завершение процессов окончено ---")
	}()
	return true
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

func FinishEmployeeProcesses(
	ctx context.Context,
	employees []constandstruct.EmployeeProcess,
	defaultUser, password string,
	logLine func(string),
) bool {
	if len(employees) == 0 {
		logLine("Выберите хотя бы одного сотрудника.")
		return false
	}

	type serverWork struct {
		server constandstruct.Server
		pids   []int
	}

	workByIP := make(map[string]*serverWork)
	order := make([]string, 0)

	for _, employee := range employees {
		serverIP := strings.TrimSpace(employee.ServerIP)
		if serverIP == "" {
			continue
		}

		work := workByIP[serverIP]
		if work == nil {
			work = &serverWork{
				server: constandstruct.Server{
					Name: employee.ServerName,
					IP:   serverIP,
				},
			}
			workByIP[serverIP] = work
			order = append(order, serverIP)
		}

		for _, pid := range employee.PIDs {
			if pid > 0 && !containsInt(work.pids, pid) {
				work.pids = append(work.pids, pid)
			}
		}
	}

	validOrder := make([]string, 0, len(order))
	for _, ip := range order {
		if len(workByIP[ip].pids) > 0 {
			sort.Ints(workByIP[ip].pids)
			validOrder = append(validOrder, ip)
		}
	}

	if len(validOrder) == 0 {
		logLine("У выбранных сотрудников нет корректных PID.")
		return false
	}
	defaultUser = strings.TrimSpace(defaultUser)
	if defaultUser == "" {
		logLine("Введите логин в формате DOMAIN\\username или .\\localadmin.")
		return false
	}
	if password == "" {
		logLine("Введите пароль. Пароль не сохраняется и не записывается в журнал.")
		return false
	}

	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()

		operationCtx, cancel := context.WithTimeout(
			operationBaseCtx,
			constandstruct.OperationTimeout,
		)
		defer cancel()

		targets := make([]constandstruct.Server, 0, len(validOrder))
		for _, ip := range validOrder {
			targets = append(targets, workByIP[ip].server)
		}

		runTargets(operationCtx, targets, func(target constandstruct.Server) {
			if operationCtx.Err() != nil {
				logLine(
					"Операция остановлена: " +
						contextErrorText(operationCtx.Err()),
				)
				return
			}

			work := workByIP[target.IP]

			logLine(fmt.Sprintf(
				"=== Завершение выбранных процессов на %s (%s) ===",
				work.server.Name,
				target.IP,
			))

			serverCtx, stop := context.WithTimeout(
				operationCtx,
				constandstruct.MutationServerTimeout,
			)

			finishStatus := startRemoteStatus(
				work.server,
				constandstruct.MutationServerTimeout,
				logLine,
			)

			lines, err := remotefunc.RunRemoteKillPIDsWithProgress(
				serverCtx,
				target.IP,
				defaultUser,
				password,
				work.pids,
				remoteProgressLogger(work.server, logLine),
			)

			finishStatus(err)
			stop()

			if logCallError(
				operationCtx,
				work.server,
				err,
				logLine,
			) {
				return
			}

			if err == nil {
				logKillResult(work.server, lines, logLine)
			}
		})

		logLine(
			"--- Завершение выбранных процессов окончено ---",
		)
	}()

	return true
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
