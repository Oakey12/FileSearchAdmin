package main

import (
	"context"

	auxiliaryfunc "filesearch-admin/internal/auxiliaryFunc"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppVersion — текущая версия программы. На этапе Nginx значение задаётся через
// -ldflags "-X main.AppVersion=...".
var AppVersion = "1.0.0"

// App — методы, которые вызывает интерфейс.
type App struct {
	ctx    context.Context
	logger *auxiliaryfunc.Logger
}

// NewApp создаёт приложение.
func NewApp() *App {
	return &App{}
}

// startup получает контекст Wails и открывает файловый журнал. Строки журнала
// одновременно пишутся в файл (с ротацией) и уходят в интерфейс событием
// "journal:line".
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if path, err := auxiliaryfunc.DefaultLogPath(); err == nil {
		a.logger, _ = auxiliaryfunc.New(path, func(line string) {
			runtime.EventsEmit(a.ctx, "journal:line", line)
		})
	}
}

// shutdown закрывает журнал при выходе.
func (a *App) shutdown(ctx context.Context) {
	if a.logger != nil {
		_ = a.logger.Close()
	}
}

// Version возвращает текущую версию программы.
func (a *App) Version() string {
	return AppVersion
}

// logLine пишет строку в файловый журнал и в интерфейс.
func (a *App) logLine(line string) {
	if a.logger != nil {
		a.logger.LogLine(line)
		return
	}
	runtime.EventsEmit(a.ctx, "journal:line", line)
}

// outputLine показывает потенциально чувствительный вывод команды только в
// интерфейсе, не записывая его в файловый журнал.
func (a *App) outputLine(line string) {
	if a.logger != nil {
		a.logger.LogUIOnly(line)
		return
	}
	runtime.EventsEmit(a.ctx, "journal:line", line)
}

// emitStatus сообщает интерфейсу, идёт ли сейчас операция.
func (a *App) emitStatus(running bool) {
	runtime.EventsEmit(a.ctx, "operation:status", operationStatus{Running: running})
}

type operationStatus struct {
	Running bool `json:"running"`
}
