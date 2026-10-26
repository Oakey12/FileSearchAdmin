package main

// Проверка программы по списку компьютеров: загрузка JSON со списком,
// сохранение образца и сама проверка. Ответ каждого компьютера уходит событием
// "appcheck:result", конец проверки — событием "appcheck:done".

import (
	_ "embed"
	"fmt"
	"os"

	constandstruct "filesearch-admin/internal/constAndStruct"
	"filesearch-admin/internal/config"
	"filesearch-admin/internal/ops"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed docs/computers.example.json
var computersExample []byte

// computersFileLimit — больше этого JSON со списком адресов не бывает.
const computersFileLimit = 4 << 20

// ImportComputers открывает диалог выбора JSON со списком компьютеров и
// возвращает разобранный список. Пустой список и nil-ошибка — выбор отменён.
func (a *App) ImportComputers() ([]constandstruct.Server, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите JSON со списком компьютеров",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
			{DisplayName: "Все файлы", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}
	if info.Size() > computersFileLimit {
		return nil, fmt.Errorf("файл слишком большой: %d байт", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}
	computers, err := config.ParseComputers(data)
	if err != nil {
		return nil, err
	}
	a.logLine(fmt.Sprintf("Загружен список компьютеров для проверки: %d.", len(computers)))
	return computers, nil
}

// SaveComputersExample сохраняет образец JSON со списком компьютеров туда,
// куда укажет пользователь. Пустой путь — сохранение отменено.
func (a *App) SaveComputersExample() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Сохранить образец JSON со списком компьютеров",
		DefaultFilename: "computers.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, computersExample, 0o644); err != nil {
		return "", fmt.Errorf("не удалось сохранить образец: %w", err)
	}
	a.logLine("Образец списка компьютеров сохранён: " + path)
	return path, nil
}

// CheckApplication проверяет, установлена ли программа на каждом компьютере
// списка. false — проверка не началась (причина в журнале).
func (a *App) CheckApplication(targets []constandstruct.Server, user, password, query string) bool {
	started := ops.CheckApplicationOnTargets(a.ctx, targets, user, password, query, a.logLine,
		func(check constandstruct.ApplicationCheck) {
			runtime.EventsEmit(a.ctx, "appcheck:result", check)
		},
		func() {
			runtime.EventsEmit(a.ctx, "appcheck:done")
		})
	if !started {
		// Отклонённый старт не трогает индикатор уже идущей операции.
		return false
	}
	a.emitStatus(true)
	go func() {
		ops.WaitForOperations(a.ctx)
		a.emitStatus(false)
	}()
	return true
}
