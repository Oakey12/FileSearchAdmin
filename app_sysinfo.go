package main

// Сбор сведений о системе по списку компьютеров параллельно. Результат каждого
// уходит событием "sysinfo:result", конец — событием "sysinfo:done". Ответы
// последнего сбора хранятся здесь же, чтобы выгрузить их в Excel.

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	constandstruct "filesearch-admin/internal/constAndStruct"
	"filesearch-admin/internal/ops"
	"filesearch-admin/internal/xlsx"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// sysInfoBatch — ответы одного сбора. Порядок выгрузки — порядок компьютеров
// в списке (JSON), а не порядок прихода ответов.
type sysInfoBatch struct {
	mu      sync.Mutex
	order   map[string]int
	results map[string]constandstruct.SystemInfoResult
}

func newSysInfoBatch(targets []constandstruct.Server) *sysInfoBatch {
	b := &sysInfoBatch{
		order:   make(map[string]int, len(targets)),
		results: make(map[string]constandstruct.SystemInfoResult, len(targets)),
	}
	for i, t := range targets {
		if _, seen := b.order[t.IP]; !seen {
			b.order[t.IP] = i
		}
	}
	return b
}

func (b *sysInfoBatch) add(result constandstruct.SystemInfoResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.results[result.IP] = result
}

func (b *sysInfoBatch) snapshot() []constandstruct.SystemInfoResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := make([]constandstruct.SystemInfoResult, 0, len(b.results))
	for _, r := range b.results {
		list = append(list, r)
	}
	sort.SliceStable(list, func(i, j int) bool {
		oi, iok := b.order[list[i].IP]
		oj, jok := b.order[list[j].IP]
		if iok != jok {
			return iok
		}
		if oi != oj {
			return oi < oj
		}
		return list[i].IP < list[j].IP
	})
	return list
}

// sysInfoStore — текущий (последний начавшийся) сбор.
type sysInfoStore struct {
	mu      sync.Mutex
	current *sysInfoBatch
}

func (s *sysInfoStore) set(batch *sysInfoBatch) {
	s.mu.Lock()
	s.current = batch
	s.mu.Unlock()
}

func (s *sysInfoStore) snapshot() []constandstruct.SystemInfoResult {
	s.mu.Lock()
	batch := s.current
	s.mu.Unlock()
	if batch == nil {
		return nil
	}
	return batch.snapshot()
}

// ScanSystemInfoAll собирает сведения о системе со всех выбранных компьютеров
// параллельно. false — сбор не начался (причина в журнале).
func (a *App) ScanSystemInfoAll(targets []constandstruct.Server, user, password string) bool {
	// Ответы копятся в новой партии; текущей она становится, только если сбор
	// начался, — иначе прежние сведения остаются доступны для выгрузки.
	batch := newSysInfoBatch(targets)
	started := ops.ScanSystemInfoOnTargets(a.ctx, targets, user, password, a.logLine,
		func(result constandstruct.SystemInfoResult) {
			batch.add(result)
			runtime.EventsEmit(a.ctx, "sysinfo:result", result)
		},
		func() {
			runtime.EventsEmit(a.ctx, "sysinfo:done")
		})
	if !started {
		return false
	}
	a.sysInfo.set(batch)
	a.emitStatus(true)
	go func() {
		ops.WaitForOperations(a.ctx)
		a.emitStatus(false)
	}()
	return true
}

// ExportSystemInfo сохраняет сведения последнего сбора в таблицу Excel туда,
// куда укажет пользователь. Пустой путь — сохранение отменено.
func (a *App) ExportSystemInfo() (string, error) {
	results := a.sysInfo.snapshot()
	if len(results) == 0 {
		return "", fmt.Errorf("нет сведений для выгрузки: сначала соберите их")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Выгрузить сведения о системе в Excel",
		DefaultFilename: "Сведения о системе " + time.Now().Format("2006-01-02 15-04") + ".xlsx",
		Filters: []runtime.FileFilter{
			{DisplayName: "Книга Excel (*.xlsx)", Pattern: "*.xlsx"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := writeSystemInfoXLSX(path, results); err != nil {
		return "", err
	}
	a.logLine(fmt.Sprintf("Сведения о системе выгружены в Excel: %s (компьютеров: %d).", path, len(results)))
	return path, nil
}

func writeSystemInfoXLSX(path string, results []constandstruct.SystemInfoResult) error {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, ops.SystemInfoSheet(results)); err != nil {
		return fmt.Errorf("не удалось сформировать таблицу: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("не удалось сохранить файл (возможно, он открыт в Excel): %w", err)
	}
	return nil
}
