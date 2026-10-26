package ops

import (
	"context"
	"errors"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"fmt"
	"strings"
	"sync"
)

// CheckApplicationOnTargets проверяет, установлена ли программа на каждом
// компьютере списка. Ответ каждого компьютера уходит в onResult сразу, как
// только он получен, — таблица заполняется по мере проверки. onDone
// вызывается один раз в конце, в том числе после отмены.
//
// В журнал пишется по одной строке на компьютер: при сотнях адресов этапы
// удалённого сценария утопили бы итог.
func CheckApplicationOnTargets(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password, query string,
	logLine func(string),
	onResult func(constandstruct.ApplicationCheck),
	onDone func(),
) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		logLine("Проверка по списку: введите название программы.")
		return false
	}
	if len(targets) == 0 {
		logLine("Проверка по списку: загрузите JSON со списком компьютеров или выберите серверы.")
		return false
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return false
	}
	operationCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		logLine(fmt.Sprintf("=== Проверка «%s» на компьютерах: %d ===", query, len(targets)))

		var countsMu sync.Mutex
		counts := make(map[string]int, 3)
		runTargetsLimit(operationCtx, targets, constandstruct.MaxParallelAppChecks, func(target constandstruct.Server) {
			check := checkApplicationOn(operationCtx, target, user, password, query)
			countsMu.Lock()
			counts[check.Status]++
			countsMu.Unlock()
			logLine(DescribeApplicationCheck(check))
			if onResult != nil {
				onResult(check)
			}
		})

		if operationCtx.Err() != nil {
			logLine("Проверка остановлена: " + contextErrorText(operationCtx.Err()))
		}
		checked := counts[constandstruct.AppCheckInstalled] + counts[constandstruct.AppCheckTraces] +
			counts[constandstruct.AppCheckMissing] + counts[constandstruct.AppCheckError]
		logLine(fmt.Sprintf(
			"--- Проверка завершена: установлена — %d, только файлы — %d, не установлена — %d, ошибок — %d, не проверено — %d ---",
			counts[constandstruct.AppCheckInstalled],
			counts[constandstruct.AppCheckTraces],
			counts[constandstruct.AppCheckMissing],
			counts[constandstruct.AppCheckError],
			len(targets)-checked,
		))
		if onDone != nil {
			onDone()
		}
	}()
	return true
}

// checkApplicationOn проверяет один компьютер. Ошибка подключения — тоже
// результат: в таблице она отличается от «не установлена».
func checkApplicationOn(
	ctx context.Context,
	target constandstruct.Server,
	user, password, query string,
) constandstruct.ApplicationCheck {
	targetCtx, cancel := context.WithTimeout(ctx, constandstruct.ProgramScanTimeout)
	defer cancel()
	found, warnings, err := findApplicationOn(targetCtx, target.IP, user, password, query, nil)
	return BuildApplicationCheck(ctx, target, found, warnings, err)
}

// BuildApplicationCheck сводит ответ сценария поиска к итогу «установлена /
// только файлы / не установлена / ошибка». Общий для проверки одного ПК и
// списка. ctx —
// контекст всей операции: по нему отмена отличается от тайм-аута компьютера.
func BuildApplicationCheck(
	ctx context.Context,
	target constandstruct.Server,
	found []constandstruct.ApplicationMatch,
	warnings []string,
	err error,
) constandstruct.ApplicationCheck {
	check := constandstruct.ApplicationCheck{Name: target.Name, IP: target.IP, Warnings: warnings}
	switch {
	case err != nil:
		check.Status = constandstruct.AppCheckError
		check.Error = checkErrorText(ctx, err)
	case len(found) == 0:
		check.Status = constandstruct.AppCheckMissing
		check.Summary = "Не найдена"
	default:
		check.Matches = found
		check.Owners = SummarizeApplicationOwners(found)
		// «Установлена» — только по реестру, службе или запущенному процессу.
		// Одни папки и файлы — частые остатки после удаления: о них говорим
		// отдельно, чтобы не выдать их за установленную программу.
		main, installed := strongApplicationMatch(found)
		if installed {
			check.Status = constandstruct.AppCheckInstalled
		} else {
			check.Status = constandstruct.AppCheckTraces
		}
		check.Summary = applicationTitle(main)
		if main.Source != "" {
			check.Summary += " (" + main.Source + ")"
		}
	}
	return check
}

// strongApplicationMatch возвращает первую находку, которая означает
// установленную программу (реестр, служба, процесс). Если таких нет — первую
// находку и false.
func strongApplicationMatch(found []constandstruct.ApplicationMatch) (constandstruct.ApplicationMatch, bool) {
	for _, match := range found {
		for _, prefix := range []string{"реестр", "служба", "запущен"} {
			if strings.HasPrefix(match.Source, prefix) {
				return match, true
			}
		}
	}
	return found[0], false
}

func checkErrorText(operationCtx context.Context, err error) string {
	if operationCtx.Err() != nil {
		return contextErrorText(operationCtx.Err())
	}
	// Одиночная проверка получает контекст приложения, который не
	// отменяется: отмену видно только по самой ошибке.
	if errors.Is(err, context.Canceled) {
		return "операция отменена"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if stage := remotefunc.ErrorStage(err); stage != "" {
			return "превышено время ожидания; последний этап: " + stage
		}
		return "превышено время ожидания"
	}
	return strings.TrimSpace(err.Error())
}

// applicationTitle — название и версия без пути: в ячейке таблицы путь лишний.
func applicationTitle(match constandstruct.ApplicationMatch) string {
	if match.Version != "" {
		return match.Name + " " + match.Version
	}
	return match.Name
}

// DescribeApplicationCheck — строка журнала с итогом по одному компьютеру.
func DescribeApplicationCheck(check constandstruct.ApplicationCheck) string {
	name := check.IP
	if check.Name != "" && check.Name != check.IP {
		name = fmt.Sprintf("%s (%s)", check.Name, check.IP)
	}
	switch check.Status {
	case constandstruct.AppCheckInstalled:
		text := fmt.Sprintf("%s: установлена — %s", name, check.Summary)
		if len(check.Owners) > 0 {
			text += "; у кого: " + strings.Join(check.Owners, ", ")
		}
		return text + "."
	case constandstruct.AppCheckTraces:
		return fmt.Sprintf("%s: найдены только файлы — %s; возможно, остатки после удаления.", name, check.Summary)
	case constandstruct.AppCheckMissing:
		return name + ": не установлена."
	default:
		return fmt.Sprintf("%s: ошибка проверки: %s", name, check.Error)
	}
}
