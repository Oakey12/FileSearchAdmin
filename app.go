package main

import (
	"context"
)

// AppVersion — текущая версия программы. На этапе Nginx значение задаётся через
// -ldflags "-X main.AppVersion=...".
var AppVersion = "1.0.0"

// App — методы, которые вызывает интерфейс.
type App struct {
	ctx context.Context
}

// NewApp создаёт приложение.
func NewApp() *App {
	return &App{}
}

// startup получает контекст Wails при запуске окна.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Version возвращает текущую версию программы.
func (a *App) Version() string {
	return AppVersion
}
