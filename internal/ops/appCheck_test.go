package ops

import (
	"context"
	"errors"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckApplicationOnTargetsReportsEveryComputer(t *testing.T) {
	original := runRemoteScript
	defer func() { runRemoteScript = original }()
	runRemoteScript = func(_ context.Context, address, _, _, _ string, _ int, _ remotefunc.ProgressFunc) ([]string, error) {
		switch address {
		case "10.0.0.1":
			return []string{
				appLine("COMMAND_LINE|APP_MATCH|", `{"Source":"реестр","Owner":"Весь компьютер","Name":"TrueConf Client","Version":"8.5"}`),
				"COMMAND_LINE|APP_DONE|1",
			}, nil
		case "10.0.0.2":
			return []string{
				appLine("COMMAND_LINE|APP_WARNING|", `{"Stage":"Профили без активного сеанса","Message":"личные установки не видны у: CORP\\ivanov"}`),
				"COMMAND_LINE|APP_DONE|0",
			}, nil
		default:
			return nil, errors.New("сервер RPC недоступен")
		}
	}

	targets := []constandstruct.Server{
		{Name: "Бухгалтерия", IP: "10.0.0.1"},
		{Name: "10.0.0.2", IP: "10.0.0.2"},
		{Name: "Склад", IP: "10.0.0.3"},
	}
	var mu sync.Mutex
	results := make(map[string]constandstruct.ApplicationCheck)
	var journal []string
	done := make(chan struct{})

	started := CheckApplicationOnTargets(context.Background(), targets, `CORP\admin`, "secret", " TrueConf ",
		func(line string) { mu.Lock(); journal = append(journal, line); mu.Unlock() },
		func(check constandstruct.ApplicationCheck) { mu.Lock(); results[check.IP] = check; mu.Unlock() },
		func() { close(done) },
	)
	if !started {
		t.Fatal("проверка не запустилась")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("проверка не завершилась")
	}
	WaitForOperations(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(results) != 3 {
		t.Fatalf("получено результатов %d, ожидалось 3: %#v", len(results), results)
	}
	installed := results["10.0.0.1"]
	if installed.Status != constandstruct.AppCheckInstalled || installed.Summary != "TrueConf Client 8.5" ||
		installed.Name != "Бухгалтерия" || len(installed.Owners) != 1 || installed.Owners[0] != "Весь компьютер" {
		t.Fatalf("установленная программа описана неверно: %#v", installed)
	}
	missing := results["10.0.0.2"]
	if missing.Status != constandstruct.AppCheckMissing || len(missing.Warnings) != 1 {
		t.Fatalf("отсутствие программы описано неверно: %#v", missing)
	}
	failed := results["10.0.0.3"]
	if failed.Status != constandstruct.AppCheckError || !strings.Contains(failed.Error, "RPC") {
		t.Fatalf("ошибка подключения описана неверно: %#v", failed)
	}

	summary := journal[len(journal)-1]
	if !strings.Contains(summary, "установлена — 1, не установлена — 1, ошибок — 1, не проверено — 0") {
		t.Fatalf("итог в журнале неверен: %q", summary)
	}
	for _, line := range journal {
		if strings.Contains(line, "secret") {
			t.Fatalf("пароль попал в журнал: %q", line)
		}
	}
}

func TestCheckApplicationOnTargetsRejectsIncompleteInput(t *testing.T) {
	targets := []constandstruct.Server{{Name: "ПК", IP: "10.0.0.1"}}
	noop := func(string) {}
	if CheckApplicationOnTargets(context.Background(), targets, `CORP\admin`, "secret", "  ", noop, nil, nil) {
		t.Fatal("проверка запустилась без названия программы")
	}
	if CheckApplicationOnTargets(context.Background(), nil, `CORP\admin`, "secret", "TrueConf", noop, nil, nil) {
		t.Fatal("проверка запустилась без списка компьютеров")
	}
	if CheckApplicationOnTargets(context.Background(), targets, `CORP\admin`, "", "TrueConf", noop, nil, nil) {
		t.Fatal("проверка запустилась без пароля")
	}
}

func TestDescribeApplicationCheckNamesComputerOnce(t *testing.T) {
	line := DescribeApplicationCheck(constandstruct.ApplicationCheck{Name: "10.0.0.5", IP: "10.0.0.5", Status: constandstruct.AppCheckMissing})
	if line != "10.0.0.5: не установлена." {
		t.Fatalf("строка журнала: %q", line)
	}
}
