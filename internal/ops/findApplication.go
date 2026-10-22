package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	psscript "filesearch-admin/internal/psScript"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"strconv"
	"strings"
)

type applicationFindWarning struct {
	Stage   string `json:"Stage"`
	Message string `json:"Message"`
}

// FindApplication ищет приложение на целевом компьютере по имени или первым
// символам. Учётные данные нужны только для CIM/DCOM-подключения.
func FindApplication(
	ctx context.Context,
	targetIP, username, password, query string,
	logLine func(string),
	onComplete func([]constandstruct.ApplicationMatch, []string, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	query = strings.TrimSpace(query)
	if query == "" {
		logLine("Поиск приложения: введите имя программы или первые символы названия.")
		return false
	}
	if net.ParseIP(targetIP) == nil {
		logLine("Поиск приложения: введите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" {
		logLine("Поиск приложения: введите логин администратора в формате DOMAIN\\username или .\\localadmin.")
		return false
	}
	if password == "" {
		logLine("Поиск приложения: введите пароль. Пароль не записывается в журнал.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.ProgramScanTimeout)
		defer cancel()

		target := constandstruct.Server{Name: "Поиск приложения", IP: targetIP}
		logLine(fmt.Sprintf("=== Поиск «%s» на %s ===", query, targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.ProgramScanTimeout, logLine)

		lines, runErr := remotefunc.RunRemoteScriptWithOutputLimitAndProgress(
			operationCtx,
			targetIP,
			username,
			password,
			psscript.FindApplicationScript(query),
			remotefunc.MaxRemoteOutputCharacters,
			remoteProgressLogger(target, logLine),
		)

		found, warnings, parseErr := parseApplicationMatches(lines)
		resultErr := runErr
		if resultErr == nil {
			if text := remoteError(lines); text != "" {
				resultErr = fmt.Errorf("%s", text)
			} else {
				resultErr = parseErr
			}
		}
		finishStatus(resultErr)

		if resultErr != nil {
			logLine("Поиск приложения: " + resultErr.Error())
		} else if len(found) == 0 {
			logLine(fmt.Sprintf("Поиск «%s» на %s: не установлено.", query, targetIP))
		} else {
			logLine(fmt.Sprintf(
				"Поиск «%s» на %s: установлено — %s; совпадений: %d.",
				query, targetIP, DescribeApplicationMatch(found[0]), len(found),
			))
		}
		for _, warning := range warnings {
			logLine("Поиск приложения: предупреждение: " + warning)
		}
		logLine("--- Поиск приложения завершён ---")

		if onComplete != nil {
			onComplete(found, warnings, resultErr)
		}
	}()
	return true
}

// parseApplicationMatches разбирает ответ сценария поиска. Записи приходят
// Base64, итог — строкой APP_DONE с их количеством.
func parseApplicationMatches(lines []string) ([]constandstruct.ApplicationMatch, []string, error) {
	var found []constandstruct.ApplicationMatch
	var warnings []string
	completed := false

	for _, line := range lines {
		if payload, ok := strings.CutPrefix(line, "COMMAND_LINE|APP_WARNING|"); ok {
			data, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return nil, warnings, fmt.Errorf("повреждено предупреждение поиска: %w", err)
			}
			var warning applicationFindWarning
			if err := json.Unmarshal(data, &warning); err != nil {
				return nil, warnings, fmt.Errorf("не удалось разобрать предупреждение поиска: %w", err)
			}
			warning.Stage = strings.TrimSpace(warning.Stage)
			warning.Message = strings.TrimSpace(warning.Message)
			if warning.Stage == "" {
				warning.Stage = "неизвестный раздел"
			}
			if warning.Message == "" {
				warning.Message = "причина не указана"
			}
			warnings = append(warnings, warning.Stage+": "+warning.Message)
			continue
		}

		if total, ok := strings.CutPrefix(line, "COMMAND_LINE|APP_DONE|"); ok {
			count, err := strconv.Atoi(strings.TrimSpace(total))
			if err != nil || count < 0 {
				return nil, warnings, fmt.Errorf("повреждён итог поиска")
			}
			completed = true
			continue
		}

		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|APP_MATCH|")
		if !ok {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, warnings, fmt.Errorf("повреждена запись найденной программы: %w", err)
		}
		var match constandstruct.ApplicationMatch
		if err := json.Unmarshal(data, &match); err != nil {
			return nil, warnings, fmt.Errorf("не удалось разобрать запись найденной программы: %w", err)
		}
		match.Source = strings.TrimSpace(match.Source)
		match.Owner = strings.TrimSpace(match.Owner)
		match.Name = strings.TrimSpace(match.Name)
		match.Version = strings.TrimSpace(match.Version)
		match.Path = strings.TrimSpace(match.Path)
		if match.Name == "" {
			continue
		}
		found = append(found, match)
	}

	if !completed {
		return nil, warnings, fmt.Errorf("целевой компьютер не завершил поиск")
	}
	return found, warnings, nil
}

// DescribeApplicationMatch собирает строку для журнала и окна: название,
// версия, где именно найдено и путь.
func DescribeApplicationMatch(match constandstruct.ApplicationMatch) string {
	text := match.Name
	if match.Version != "" {
		text += " " + match.Version
	}
	// Владелец здесь важнее источника: на терминальном сервере администратор
	// выясняет не «где нашлось», а «у кого стоит».
	if match.Owner != "" {
		text += " — " + match.Owner
	}
	if match.Source != "" {
		text += " (" + match.Source + ")"
	}
	if match.Path != "" {
		text += ": " + match.Path
	}
	return text
}

// SummarizeApplicationOwners сводит находки к списку владельцев. На
// терминальном сервере одна программа даёт десятки совпадений — по записи
// реестра и по нескольким файлам в каждом профиле, — а администратору нужен
// ответ на другой вопрос: у кого она стоит.
func SummarizeApplicationOwners(matches []constandstruct.ApplicationMatch) []string {
	seen := make(map[string]struct{}, len(matches))
	owners := make([]string, 0, len(matches))
	for _, match := range matches {
		owner := strings.TrimSpace(match.Owner)
		if owner == "" {
			owner = "владелец не определён"
		}
		if _, exists := seen[owner]; exists {
			continue
		}
		seen[owner] = struct{}{}
		owners = append(owners, owner)
	}
	return owners
}
