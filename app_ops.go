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

// ScanUsers возвращает активных пользователей целевого сервера.
func (a *App) ScanUsers(ip, user, password string) ([]constandstruct.RemoteUser, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		users []constandstruct.RemoteUser
		err   error
	}
	ch := make(chan result, 1)
	started := ops.ScanRemoteUsers(a.ctx, ip, user, password, a.logLine,
		func(users []constandstruct.RemoteUser, err error) { ch <- result{users, err} })
	if !started {
		return nil, errOperationNotStarted
	}
	r := <-ch
	return r.users, r.err
}

// ScanPrograms возвращает установленные программы целевого компьютера.
func (a *App) ScanPrograms(ip, user, password string) ([]constandstruct.InstalledProgram, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		programs []constandstruct.InstalledProgram
		err      error
	}
	ch := make(chan result, 1)
	started := ops.ScanInstalledPrograms(a.ctx, ip, user, password, a.logLine,
		func(programs []constandstruct.InstalledProgram, warnings []string, err error) {
			ch <- result{programs, err}
		})
	if !started {
		return nil, errOperationNotStarted
	}
	r := <-ch
	return r.programs, r.err
}

// UninstallProgram удаляет выбранную программу.
func (a *App) UninstallProgram(ip, user, password string, program constandstruct.InstalledProgram) error {
	a.emitStatus(true)
	defer a.emitStatus(false)
	ch := make(chan error, 1)
	started := ops.UninstallInstalledProgram(a.ctx, ip, user, password, program, a.logLine,
		func(err error) { ch <- err })
	if !started {
		return errOperationNotStarted
	}
	return <-ch
}

// ScanFolder возвращает список файлов в папке (по пути).
func (a *App) ScanFolder(ip, user, password, folderPath string) (constandstruct.RemoteFolderResult, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		folder constandstruct.RemoteFolderResult
		err    error
	}
	ch := make(chan result, 1)
	started := ops.ScanRemoteFolder(a.ctx, ip, user, password, folderPath, a.logLine,
		func(folder constandstruct.RemoteFolderResult, err error) { ch <- result{folder, err} })
	if !started {
		return constandstruct.RemoteFolderResult{}, errOperationNotStarted
	}
	r := <-ch
	return r.folder, r.err
}

// ReadRemoteFile читает текстовый файл для редактирования.
func (a *App) ReadRemoteFile(ip, user, password string, file constandstruct.RemoteFile) (constandstruct.RemoteTextDocument, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		doc constandstruct.RemoteTextDocument
		err error
	}
	ch := make(chan result, 1)
	started := ops.ReadRemoteTextFile(a.ctx, ip, user, password, file, a.logLine,
		func(doc constandstruct.RemoteTextDocument, err error) { ch <- result{doc, err} })
	if !started {
		return constandstruct.RemoteTextDocument{}, errOperationNotStarted
	}
	r := <-ch
	return r.doc, r.err
}

// SaveRemoteFile сохраняет изменённый текст файла (с резервной копией).
func (a *App) SaveRemoteFile(ip, user, password string, document constandstruct.RemoteTextDocument, newContent string) (constandstruct.RemoteSaveResult, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		saved constandstruct.RemoteSaveResult
		err   error
	}
	ch := make(chan result, 1)
	started := ops.SaveRemoteTextFile(a.ctx, ip, user, password, document, newContent, a.logLine,
		func(saved constandstruct.RemoteSaveResult, err error) { ch <- result{saved, err} })
	if !started {
		return constandstruct.RemoteSaveResult{}, errOperationNotStarted
	}
	r := <-ch
	return r.saved, r.err
}

// DeleteRemoteFile удаляет выбранный файл.
func (a *App) DeleteRemoteFile(ip, user, password string, file constandstruct.RemoteFile) error {
	a.emitStatus(true)
	defer a.emitStatus(false)
	ch := make(chan error, 1)
	started := ops.DeleteRemoteFile(a.ctx, ip, user, password, file, a.logLine,
		func(err error) { ch <- err })
	if !started {
		return errOperationNotStarted
	}
	return <-ch
}

// ScanSystemInfo собирает сведения о целевом компьютере.
func (a *App) ScanSystemInfo(ip, user, password string) (constandstruct.SystemInfo, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		info constandstruct.SystemInfo
		err  error
	}
	ch := make(chan result, 1)
	started := ops.ScanSystemInfo(a.ctx, ip, user, password, a.logLine,
		func(info constandstruct.SystemInfo, warnings []string, err error) { ch <- result{info, err} })
	if !started {
		return constandstruct.SystemInfo{}, errOperationNotStarted
	}
	r := <-ch
	return r.info, r.err
}

// FindApplication ищет приложение по имени (реестр, процессы, файлы).
func (a *App) FindApplication(ip, user, password, query string) ([]constandstruct.ApplicationMatch, error) {
	a.emitStatus(true)
	defer a.emitStatus(false)
	type result struct {
		matches []constandstruct.ApplicationMatch
		err     error
	}
	ch := make(chan result, 1)
	started := ops.FindApplication(a.ctx, ip, user, password, query, a.logLine,
		func(matches []constandstruct.ApplicationMatch, warnings []string, err error) {
			ch <- result{matches, err}
		})
	if !started {
		return nil, errOperationNotStarted
	}
	r := <-ch
	return r.matches, r.err
}

// FormatSystemInfo раскладывает сведения о системе в пары «поле/значение» для
// показа и копирования.
func (a *App) FormatSystemInfo(info constandstruct.SystemInfo) [][2]string {
	return ops.FormatSystemInfo(info)
}

