package main

// Самообновление: проверка манифеста на сервере, скачивание с проверкой sha256
// и замена работающего exe. Логика перенесена из FileSearch.

import (
	"context"
	"errors"
	"time"

	"filesearch-admin/internal/update"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateCheckInterval — как часто в фоне проверять наличие новой версии.
const updateCheckInterval = 15 * time.Minute

// UpdateInfo — результат проверки обновлений.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Version   string `json:"version,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// CheckUpdate проверяет latest.json на сервере обновлений.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()

	info := UpdateInfo{Current: update.CurrentVersion}
	release, newer, err := update.CheckLatest(ctx)
	switch {
	case errors.Is(err, update.ErrNoReleases):
		return info, nil
	case errors.Is(err, update.ErrUnavailable):
		return info, errors.New("Сервер обновлений недоступен. Проверьте подключение к сети.")
	case err != nil:
		return info, err
	}

	a.updateMu.Lock()
	if newer {
		a.release = &release
	} else {
		a.release = nil
	}
	a.updateMu.Unlock()

	info.Available = newer
	info.Version = release.Version
	info.Notes = release.Notes
	return info, nil
}

// InstallUpdate скачивает найденную версию, ставит её вместо текущего exe и
// перезапускает программу. Ход скачивания приходит событием "update:progress".
func (a *App) InstallUpdate() error {
	a.updateMu.Lock()
	release := a.release
	a.updateMu.Unlock()
	if release == nil {
		return errors.New("сначала проверьте наличие обновления")
	}

	path, err := update.Download(a.ctx, *release, func(done, total int64) {
		if total > 0 {
			runtime.EventsEmit(a.ctx, "update:progress", float64(done)/float64(total))
		}
	})
	if err != nil {
		return err
	}
	if err := update.Apply(path); err != nil {
		return err
	}
	runtime.Quit(a.ctx)
	return nil
}

// watchUpdates при запуске и далее каждые updateCheckInterval тихо проверяет
// сервер и сообщает интерфейсу событием "update:available".
func (a *App) watchUpdates() {
	for {
		if info, err := a.CheckUpdate(); err == nil {
			runtime.EventsEmit(a.ctx, "update:available", info)
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(updateCheckInterval):
		}
	}
}
