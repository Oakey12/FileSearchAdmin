package ops

// Тест подключения: по каждому серверу проверяет по шагам то, без чего не
// работает ни одна операция, и пишет в журнал причину отказа простыми словами
// и что исправить. На серверах ничего не меняет.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

var runRemoteDiagnose = remotefunc.RunRemoteDiagnose

// diagnoseServerTimeout — худший случай сценария: порт 3 с, два подключения
// по 20 с, WMI 20 с, процессы 30 с, пробный запуск 20 с — с запасом.
const diagnoseServerTimeout = 150 * time.Second

type diagStep struct {
	status string // ok / fail
	ms     int
	detail string
}

func parseDiag(lines []string) map[string]diagStep {
	steps := map[string]diagStep{}
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "DIAG|")
		if !ok {
			continue
		}
		parts := strings.SplitN(rest, "|", 4)
		if len(parts) < 4 {
			continue
		}
		ms, _ := strconv.Atoi(parts[2])
		steps[parts[0]] = diagStep{status: parts[1], ms: ms, detail: strings.TrimSpace(parts[3])}
	}
	return steps
}

func containsAny(text string, needles ...string) bool {
	text = strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// explainConnectFailure переводит ошибку подключения CIM/DCOM в причину и
// совет. Сначала по коду HRESULT (не зависит от языка Windows), затем по тексту.
func explainConnectFailure(errText, user string) (short, advice string) {
	switch {
	case containsAny(errText, "8007052e", "неверное имя пользователя", "неизвестное имя пользователя", "logon failure", "user name or password is incorrect"):
		return "неверный логин или пароль",
			"проверьте логин и пароль (раскладку и Caps Lock); после нескольких ошибок учётная запись может заблокироваться."
	case containsAny(errText, "80070005", "отказано в доступе", "access is denied"):
		advice := fmt.Sprintf("сервер отверг учётную запись «%s». Проверьте на сервере: 1) входит ли она в локальную группу «Администраторы» (команда: net localgroup Администраторы); "+
			"2) права WMI: wmimgmt.msc → свойства → «Безопасность» → root\\cimv2 → «Удалённая активация»; 3) верен ли пароль.", user)
		if strings.HasPrefix(strings.TrimSpace(user), `.\`) {
			advice += " Вход локальной учётной записью: Windows урезает её права при удалённом входе (UAC) — используйте доменную учётную запись DOMAIN\\admin."
		}
		return "отказано в доступе", advice
	case containsAny(errText, "800703e6", "адресу памяти", "invalid access to memory"):
		return "сбой DCOM/WMI на сервере",
			"ошибка возникает на стороне сервера, логин тут обычно ни при чём. На сервере: «Просмотр событий» → «Система» → источник DistributedCOM (коды 10036, 10016) на время теста; " +
				"проверьте службу «Инструментарий управления Windows» (winmgmt) — часто помогает её перезапуск или перезагрузка сервера."
	case containsAny(errText, "800706ba", "сервер rpc недоступен", "rpc server is unavailable"):
		return "сервер RPC недоступен",
			"порт 135 открыт, но динамические порты RPC закрыты: включите на сервере правила брандмауэра «Инструментарий управления Windows (WMI)»."
	case containsAny(errText, "80070079", "превышен", "timed out", "timeout"):
		return "сервер не ответил",
			"сервер не ответил на подключение за 20 с: перегружен или зависла служба WMI (winmgmt)."
	default:
		return "не удалось подключиться", "текст ошибки выше; если неясно — пришлите строку журнала разработчику."
	}
}

// logConnectionResult пишет итог теста по серверу; problem пустой — сервер
// готов к работе.
func logConnectionResult(target constandstruct.Server, user string, lines []string, logLine func(string)) (problem string) {
	name := target.Name
	if text := remoteError(lines); text != "" {
		logLine(fmt.Sprintf("%s: ошибка теста: %s", name, text))
		return "ошибка теста"
	}
	steps := parseDiag(lines)

	port, ok := steps["port"]
	if !ok {
		logLine(fmt.Sprintf("%s: сервер не вернул результат теста.", name))
		return "нет результата"
	}
	if port.status != "ok" {
		logLine(fmt.Sprintf("%s: НЕТ СВЯЗИ — порт 135 (RPC) не отвечает за 3 с. Сервер выключен, неверный IP или брандмауэр закрывает RPC/WMI.", name))
		return "нет связи"
	}

	plain, privacy := steps["dcom"], steps["dcom_privacy"]
	switch {
	case plain.status == "ok":
		logLine(fmt.Sprintf("%s: подключение CIM/DCOM — есть (%d мс).", name, plain.ms))
	case privacy.status == "ok":
		logLine(fmt.Sprintf("%s: обычное подключение отклонено (%s), а с шифрованием DCOM — есть (%d мс).", name, plain.detail, privacy.ms))
		logLine(fmt.Sprintf("%s: ⚠ сервер требует шифрования DCOM: логин и права в порядке, но операции программы его пока не используют — сообщите разработчику.", name))
		return "нужно шифрование DCOM"
	default:
		short, advice := explainConnectFailure(plain.detail, user)
		logLine(fmt.Sprintf("%s: НЕ ПОДКЛЮЧАЕТСЯ — %s. Ошибка: %s", name, short, plain.detail))
		if privacy.detail != "" && privacy.detail != plain.detail {
			logLine(fmt.Sprintf("%s: с шифрованием DCOM тоже не вышло: %s", name, privacy.detail))
		}
		logLine(fmt.Sprintf("%s: что сделать — %s", name, advice))
		return short
	}

	problem = ""
	if wmi := steps["wmi"]; wmi.status == "ok" {
		logLine(fmt.Sprintf("%s: WMI читается — %s.", name, wmi.detail))
	} else {
		logLine(fmt.Sprintf("%s: подключение есть, но WMI не читается: %s", name, wmi.detail))
		problem = "WMI не читается"
	}
	if processes := steps["processes"]; processes.status == "ok" {
		if processes.detail == "" {
			logLine(fmt.Sprintf("%s: процессы 1С сейчас не запущены.", name))
		} else {
			logLine(fmt.Sprintf("%s: запущена 1С — %s.", name, processes.detail))
		}
	} else if processes.detail != "" {
		logLine(fmt.Sprintf("%s: список процессов не получен: %s", name, processes.detail))
		if problem == "" {
			problem = "нет списка процессов"
		}
	}
	if launch := steps["launch"]; launch.status == "ok" {
		logLine(fmt.Sprintf("%s: запуск команд на сервере — работает.", name))
	} else {
		logLine(fmt.Sprintf("%s: запуск команд запрещён (%s) — не будут работать завершение 1С и оповещения. Учётной записи нужны права администратора на сервере.", name, launch.detail))
		if problem == "" {
			problem = "запуск команд запрещён"
		}
	}
	if problem == "" {
		logLine(fmt.Sprintf("%s: ✓ готов к работе.", name))
	}
	return problem
}

// TestConnection проверяет подключение ко всем выбранным серверам. false —
// проверка не началась (причина в журнале).
func TestConnection(ctx context.Context, targets []constandstruct.Server, user, password string, logLine func(string)) bool {
	if len(targets) == 0 {
		logLine("Тест подключения: выберите серверы или введите IP вручную.")
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
		logLine(fmt.Sprintf("=== Тест подключения: серверов %d, учётная запись «%s». На серверах ничего не меняется. ===", len(targets), user))

		var mu sync.Mutex
		problems := map[string]string{}
		checked := 0
		runTargetsLimit(operationCtx, targets, constandstruct.MaxParallelSystemInfo, func(target constandstruct.Server) {
			serverCtx, stop := context.WithTimeout(operationCtx, diagnoseServerTimeout)
			lines, err := runRemoteDiagnose(serverCtx, target.IP, user, password, remoteProgressLogger(target, logLine))
			stop()
			problem := ""
			if err != nil {
				if logCallError(operationCtx, target, err, logLine) {
					return
				}
				problem = "ошибка теста"
			} else {
				problem = logConnectionResult(target, user, lines, logLine)
			}
			mu.Lock()
			checked++
			if problem != "" {
				problems[target.IP] = problem
			}
			mu.Unlock()
		})

		if operationCtx.Err() != nil {
			logLine("Тест остановлен: " + contextErrorText(operationCtx.Err()))
		}
		summary := fmt.Sprintf("--- Тест подключения завершён: готовы к работе %d из %d", checked-len(problems), len(targets))
		if len(problems) > 0 {
			parts := make([]string, 0, len(problems))
			for _, target := range targets {
				if problem, ok := problems[target.IP]; ok {
					parts = append(parts, target.Name+" — "+problem)
					delete(problems, target.IP)
				}
			}
			summary += "; с проблемами: " + strings.Join(parts, "; ")
		}
		logLine(summary + " ---")
	}()
	return true
}
