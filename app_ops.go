package main

// Связываемые методы администрирования. Интерфейс вызывает их напрямую.
// Долгие операции (оповещение, поиск, команды) стримят строки журнала событием
// "journal:line" и статус событием "operation:status"; сканы, возвращающие
// данные, блокируются до результата и возвращают его вызову.

import (
	"context"
	"errors"

	constandstruct "filesearch-admin/internal/constAndStruct"
	"filesearch-admin/internal/config"
	"filesearch-admin/internal/ops"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var errOperationNotStarted = errors.New("операция не запущена — проверьте журнал")

// ServerList загружает список серверов (сеть → кэш → ошибка). Источник списка
// (сеть или кэш) уходит в журнал.
func (a *App) ServerList() ([]constandstruct.Server, error) {
	servers, source, err := config.LoadServers()
	if source != "" {
		a.logLine(source)
	}
	return servers, err
}

// IsBusy сообщает, выполняется ли сейчас операция.
func (a *App) IsBusy() bool {
	return ops.IsOperationRunning()
}

// NotifyTemplates возвращает готовые шаблоны оповещений.
func (a *App) NotifyTemplates() []constandstruct.MessageTemplate {
	return constandstruct.MessageTemplates
}

// CancelOperation запрашивает отмену текущей операции.
func (a *App) CancelOperation() {
	ops.CancelCurrentOperation(a.logLine)
}

// --- Стримящие операции ------------------------------------------------

// stream запускает долгую операцию и переводит статус в «идёт», а по завершении
// всех операций — обратно в «свободно».
func (a *App) stream(run func()) {
	a.emitStatus(true)
	run()
	go func() {
		ops.WaitForOperations(context.Background())
		a.emitStatus(false)
	}()
}

// Notify рассылает оповещение (msg.exe) на целевые серверы.
func (a *App) Notify(targets []constandstruct.Server, user, password, message string) {
	a.stream(func() { ops.Notify(a.ctx, targets, user, password, message, a.logLine) })
}

// SearchProcesses ищет открытые 1cv7s.exe на целевых серверах.
func (a *App) SearchProcesses(targets []constandstruct.Server, user, password string) {
	a.stream(func() { ops.Search(a.ctx, targets, user, password, a.logLine) })
}

// FinishAll завершает все 1cv7s.exe на целевых серверах.
func (a *App) FinishAll(targets []constandstruct.Server, user, password string) {
	a.stream(func() { ops.FinishProcess(a.ctx, targets, user, password, a.logLine) })
}

// ScanEmployees ищет сотрудников с открытой 1С; результат уходит событием
// "scan:employees".
func (a *App) ScanEmployees(targets []constandstruct.Server, user, password string) {
	a.stream(func() {
		ops.ScanEmployees(a.ctx, targets, user, password, a.logLine,
			func(found []constandstruct.EmployeeProcess) {
				runtime.EventsEmit(a.ctx, "scan:employees", found)
			})
	})
}

// FinishEmployees завершает 1С у выбранных сотрудников.
func (a *App) FinishEmployees(employees []constandstruct.EmployeeProcess, user, password string) {
	a.stream(func() { ops.FinishEmployeeProcesses(a.ctx, employees, user, password, a.logLine) })
}

// RunCommand выполняет PowerShell-сценарий на целевом компьютере.
func (a *App) RunCommand(ip, user, password, script, source string) {
	a.stream(func() {
		ops.ExecutePowerShellScriptOnTarget(a.ctx, ip, user, password, script, source, a.logLine, a.outputLine)
	})
}

// RunCommandInSession выполняет сценарий в сессии выбранного пользователя.
func (a *App) RunCommandInSession(ip, user, password, script, source string, targetUser constandstruct.RemoteUser) {
	a.stream(func() {
		ops.ExecutePowerShellScriptInUserSession(a.ctx, ip, user, password, script, source, targetUser, a.logLine, a.outputLine)
	})
}

// --- Сканы, возвращающие данные ---------------------------------------

// runBlocking запускает операцию, возвращающую данные, и блокируется до
// результата. Событие статуса шлётся ТОЛЬКО когда операция действительно
// началась: отклонённый старт (операция занята) не гасит индикатор и кнопку
// «Отменить» у уже идущей операции.
func runBlocking[T any](a *App, start func(done func(T, error)) bool) (T, error) {
	var zero T
	type result struct {
		value T
		err   error
	}
	ch := make(chan result, 1)
	started := start(func(value T, err error) { ch <- result{value, err} })
	if !started {
		return zero, errOperationNotStarted
	}
	a.emitStatus(true)
	defer a.emitStatus(false)
	r := <-ch
	return r.value, r.err
}

// ScanUsers возвращает активных пользователей целевого сервера.
func (a *App) ScanUsers(ip, user, password string) ([]constandstruct.RemoteUser, error) {
	return runBlocking(a, func(done func([]constandstruct.RemoteUser, error)) bool {
		return ops.ScanRemoteUsers(a.ctx, ip, user, password, a.logLine, done)
	})
}

// ScanPrograms возвращает установленные программы целевого компьютера.
func (a *App) ScanPrograms(ip, user, password string) ([]constandstruct.InstalledProgram, error) {
	return runBlocking(a, func(done func([]constandstruct.InstalledProgram, error)) bool {
		return ops.ScanInstalledPrograms(a.ctx, ip, user, password, a.logLine,
			func(programs []constandstruct.InstalledProgram, warnings []string, err error) { done(programs, err) })
	})
}

// UninstallProgram удаляет выбранную программу.
func (a *App) UninstallProgram(ip, user, password string, program constandstruct.InstalledProgram) error {
	_, err := runBlocking(a, func(done func(struct{}, error)) bool {
		return ops.UninstallInstalledProgram(a.ctx, ip, user, password, program, a.logLine,
			func(err error) { done(struct{}{}, err) })
	})
	return err
}

// ScanFolder возвращает список файлов в папке (по пути).
func (a *App) ScanFolder(ip, user, password, folderPath string) (constandstruct.RemoteFolderResult, error) {
	return runBlocking(a, func(done func(constandstruct.RemoteFolderResult, error)) bool {
		return ops.ScanRemoteFolder(a.ctx, ip, user, password, folderPath, a.logLine, done)
	})
}

// ReadRemoteFile читает текстовый файл для редактирования.
func (a *App) ReadRemoteFile(ip, user, password string, file constandstruct.RemoteFile) (constandstruct.RemoteTextDocument, error) {
	return runBlocking(a, func(done func(constandstruct.RemoteTextDocument, error)) bool {
		return ops.ReadRemoteTextFile(a.ctx, ip, user, password, file, a.logLine, done)
	})
}

// SaveRemoteFile сохраняет изменённый текст файла (с резервной копией).
func (a *App) SaveRemoteFile(ip, user, password string, document constandstruct.RemoteTextDocument, newContent string) (constandstruct.RemoteSaveResult, error) {
	return runBlocking(a, func(done func(constandstruct.RemoteSaveResult, error)) bool {
		return ops.SaveRemoteTextFile(a.ctx, ip, user, password, document, newContent, a.logLine, done)
	})
}

// DeleteRemoteFile удаляет выбранный файл.
func (a *App) DeleteRemoteFile(ip, user, password string, file constandstruct.RemoteFile) error {
	_, err := runBlocking(a, func(done func(struct{}, error)) bool {
		return ops.DeleteRemoteFile(a.ctx, ip, user, password, file, a.logLine,
			func(err error) { done(struct{}{}, err) })
	})
	return err
}

// ScanSystemInfo собирает сведения о целевом компьютере.
func (a *App) ScanSystemInfo(ip, user, password string) (constandstruct.SystemInfo, error) {
	return runBlocking(a, func(done func(constandstruct.SystemInfo, error)) bool {
		return ops.ScanSystemInfo(a.ctx, ip, user, password, a.logLine,
			func(info constandstruct.SystemInfo, warnings []string, err error) { done(info, err) })
	})
}

// FindApplication ищет приложение по имени (реестр, процессы, файлы).
func (a *App) FindApplication(ip, user, password, query string) ([]constandstruct.ApplicationMatch, error) {
	return runBlocking(a, func(done func([]constandstruct.ApplicationMatch, error)) bool {
		return ops.FindApplication(a.ctx, ip, user, password, query, a.logLine,
			func(matches []constandstruct.ApplicationMatch, warnings []string, err error) { done(matches, err) })
	})
}

// FormatSystemInfo раскладывает сведения о системе в пары «поле/значение» для
// показа и копирования.
func (a *App) FormatSystemInfo(info constandstruct.SystemInfo) [][2]string {
	return ops.FormatSystemInfo(info)
}
