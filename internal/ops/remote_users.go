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
	"sort"
	"strings"
)

const MaxRemoteUsers = 200

// ScanRemoteUsers обнаруживает активные интерактивные сеансы тем же прямым
// CIM/DCOM-транспортом, который используется для поиска и завершения 1cv7s.
// Путь профиля берётся из Win32_UserProfile на целевом сервере.
func ScanRemoteUsers(
	ctx context.Context,
	targetIP, username, password string,
	logLine func(string),
	onComplete func([]constandstruct.RemoteUser, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	if net.ParseIP(targetIP) == nil {
		logLine("Пользователи: введите корректный IP-адрес целевого сервера.")
		return false
	}
	if username == "" || password == "" {
		logLine("Пользователи: введите логин и пароль администратора целевого сервера.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.RemoteUserScanTimeout)
		defer cancel()
		target := constandstruct.Server{Name: "Сканирование пользователей", IP: targetIP}
		logLine(fmt.Sprintf("=== Сканирование активных пользователей на %s ===", targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.RemoteUserScanTimeout, logLine)
		// Сценарий переносится на целевой компьютер: там вызовы CIM локальные.
		// Прежний разбор сеансов через CIM-сессию делал по два обращения DCOM
		// на каждый explorer.exe и растягивал сканирование на минуты.
		lines, runErr := remotefunc.RunRemoteScriptWithOutputLimitAndProgress(
			operationCtx,
			targetIP,
			username,
			password,
			psscript.RemoteUsersScript,
			remotefunc.MaxRemoteOutputCharacters,
			remoteProgressLogger(target, logLine),
		)

		var users []constandstruct.RemoteUser
		resultErr := runErr
		if resultErr == nil {
			users, resultErr = parseRemoteUsers(lines)
		}
		logRemoteUserWarnings(target, lines, logLine)
		finishStatus(resultErr)
		if resultErr != nil {
			if runErr != nil {
				logCallError(operationCtx, target, runErr, logLine)
			} else {
				logLine("Пользователи: ошибка сканирования: " + resultErr.Error())
			}
		} else {
			logLine(fmt.Sprintf("Пользователи: найдено активных пользователей: %d.", len(users)))
		}
		logLine("--- Сканирование пользователей завершено ---")
		if onComplete != nil {
			onComplete(users, resultErr)
		}
	}()
	return true
}

func parseRemoteUsers(lines []string) ([]constandstruct.RemoteUser, error) {
	if text := remoteError(lines); text != "" {
		return nil, fmt.Errorf("удалённый сервер вернул ошибку: %s", text)
	}
	confirmed := false
	seen := make(map[string]struct{})
	users := make([]constandstruct.RemoteUser, 0)
	for _, line := range lines {
		if line == "USERS_OK" || line == "COMMAND_OK" {
			confirmed = true
			continue
		}
		payload, ok := strings.CutPrefix(line, "REMOTE_USER|")
		if !ok {
			payload, ok = strings.CutPrefix(line, "COMMAND_LINE|REMOTE_USER|")
		}
		if !ok {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("повреждена запись пользователя: %w", err)
		}
		var user constandstruct.RemoteUser
		if err := json.Unmarshal(data, &user); err != nil {
			return nil, fmt.Errorf("не удалось разобрать запись пользователя: %w", err)
		}
		user.DisplayName = strings.TrimSpace(user.DisplayName)
		user.Username = strings.TrimSpace(user.Username)
		user.Domain = strings.TrimSpace(user.Domain)
		user.SID = strings.TrimSpace(user.SID)
		user.ProfilePath = strings.TrimSpace(user.ProfilePath)
		if user.DisplayName == "" || user.Username == "" || user.ProfilePath == "" || !strings.HasPrefix(user.SID, "S-1-5-21-") {
			continue
		}
		key := strings.ToLower(user.SID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		users = append(users, user)
	}
	if !confirmed {
		return nil, fmt.Errorf("ответ не содержит подтверждения сканирования пользователей")
	}
	sort.SliceStable(users, func(left, right int) bool {
		return strings.ToLower(users[left].DisplayName) < strings.ToLower(users[right].DisplayName)
	})
	if len(users) > MaxRemoteUsers {
		users = users[:MaxRemoteUsers]
	}
	return users, nil
}

func logRemoteUserWarnings(target constandstruct.Server, lines []string, logLine func(string)) {
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 || parts[0] != "WARN_REMOTE_USER" {
			continue
		}
		logLine(fmt.Sprintf(
			"%s: не удалось определить пользователя PID=%s, сессия=%s: %s",
			target.Name,
			parts[1],
			parts[2],
			parts[3],
		))
	}
}
