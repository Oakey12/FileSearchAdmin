package main

// Сбор сведений о системе по списку компьютеров параллельно. Результат каждого
// уходит событием "sysinfo:result", конец — событием "sysinfo:done".

import (
	constandstruct "filesearch-admin/internal/constAndStruct"
	"filesearch-admin/internal/ops"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScanSystemInfoAll собирает сведения о системе со всех выбранных компьютеров
// параллельно. false — сбор не начался (причина в журнале).
func (a *App) ScanSystemInfoAll(targets []constandstruct.Server, user, password string) bool {
	started := ops.ScanSystemInfoOnTargets(a.ctx, targets, user, password, a.logLine,
		func(result constandstruct.SystemInfoResult) {
			runtime.EventsEmit(a.ctx, "sysinfo:result", result)
		},
		func() {
			runtime.EventsEmit(a.ctx, "sysinfo:done")
		})
	if !started {
		return false
	}
	a.emitStatus(true)
	go func() {
		ops.WaitForOperations(a.ctx)
		a.emitStatus(false)
	}()
	return true
}
