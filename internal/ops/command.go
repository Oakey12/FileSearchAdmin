package ops

import (
	"context"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"strings"
)

// ExecutePowerShellScriptOnTarget проверяет ввод и запускает один сценарий
// на одном целевом компьютере.
func ExecutePowerShellScriptOnTarget(
	ctx context.Context,
	targetIP string,
	user, password string,
	scriptText, sourceDescription string,
	logLine, outputLine func(string),
) bool {
	return executePowerShellScript(
		ctx,
		targetIP,
		user,
		password,
		scriptText,
		sourceDescription,
		nil,
		logLine,
		outputLine,
	)
}

// ExecutePowerShellScriptInUserSession запускает сценарий от интерактивного
// токена выбранного пользователя. Это позволяет работать с его HKCU,
// USERPROFILE, сетевыми подключениями и другими данными конкретной сессии.
func ExecutePowerShellScriptInUserSession(
	ctx context.Context,
	targetIP string,
	user, password string,
	scriptText, sourceDescription string,
	targetUser constandstruct.RemoteUser,
	logLine, outputLine func(string),
) bool {
	return executePowerShellScript(
		ctx,
		targetIP,
		user,
		password,
		scriptText,
		sourceDescription,
		&targetUser,
		logLine,
		outputLine,
	)
}

func executePowerShellScript(
	ctx context.Context,
	targetIP string,
	user, password string,
	scriptText, sourceDescription string,
	targetUser *constandstruct.RemoteUser,
	logLine, outputLine func(string),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	if net.ParseIP(targetIP) == nil {
		logLine("Введите корректный IP-адрес целевого компьютера.")
		return false
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return false
	}
	if strings.TrimSpace(scriptText) == "" {
		logLine("Введите команду PowerShell или прикрепите непустой .ps1-файл.")
		return false
	}
	if len([]byte(scriptText)) > remotefunc.MaxRemoteScriptBytes {
		logLine(fmt.Sprintf("Сценарий слишком большой: максимум %d байт.", remotefunc.MaxRemoteScriptBytes))
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.OperationTimeout)
		defer cancel()

		target := constandstruct.Server{Name: targetIP, IP: targetIP}
		if targetUser == nil {
			logLine(fmt.Sprintf("=== Запуск PowerShell на %s; источник: %s ===", targetIP, sourceDescription))
		} else {
			logLine(fmt.Sprintf(
				"=== Запуск PowerShell на %s в сессии %s (ID %d); источник: %s ===",
				targetIP,
				targetUser.DisplayName,
				targetUser.SessionID,
				sourceDescription,
			))
		}
		runScriptOnTarget(operationCtx, target, user, password, scriptText, targetUser, logLine, outputLine)
		logLine("--- Выполнение PowerShell завершено ---")
	}()
	return true
}

// RunScriptOnTargets запускает один сценарий PowerShell на каждом компьютере
// списка (от учётных данных администратора) параллельно. Вывод каждого
// компьютера сразу уходит в журнал. false — выполнение не началось (причина в
// журнале). Сессия пользователя для списка не поддерживается: запуск идёт на
// самом компьютере.
func RunScriptOnTargets(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password string,
	scriptText, sourceDescription string,
	logLine, outputLine func(string),
) bool {
	if len(targets) == 0 {
		logLine("Выполнение по списку: выберите серверы или введите IP вручную.")
		return false
	}
	user, password, ok := validateCredentials(user, password, logLine)
	if !ok {
		return false
	}
	if strings.TrimSpace(scriptText) == "" {
		logLine("Введите команду PowerShell или прикрепите непустой .ps1-файл.")
		return false
	}
	if len([]byte(scriptText)) > remotefunc.MaxRemoteScriptBytes {
		logLine(fmt.Sprintf("Сценарий слишком большой: максимум %d байт.", remotefunc.MaxRemoteScriptBytes))
		return false
	}
	operationCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		logLine(fmt.Sprintf(
			"=== Запуск PowerShell на компьютерах: %d; источник: %s ===",
			len(targets),
			sourceDescription,
		))
		runTargetsLimit(operationCtx, targets, constandstruct.MaxParallelServers, func(target constandstruct.Server) {
			runScriptOnTarget(operationCtx, target, user, password, scriptText, nil, logLine, outputLine)
		})
		if operationCtx.Err() != nil {
			logLine("Выполнение остановлено: " + contextErrorText(operationCtx.Err()))
		}
		logLine("--- Выполнение PowerShell завершено ---")
	}()
	return true
}

// runScriptOnTarget выполняет сценарий на одном компьютере (без beginOperation,
// со своим тайм-аутом на компьютер) — общее для одиночного запуска и запуска по
// списку. targetUser != nil запускает сценарий в сессии пользователя.
func runScriptOnTarget(
	operationCtx context.Context,
	target constandstruct.Server,
	user, password, scriptText string,
	targetUser *constandstruct.RemoteUser,
	logLine, outputLine func(string),
) {
	serverCtx, stop := context.WithTimeout(operationCtx, constandstruct.MutationServerTimeout)
	defer stop()
	finishStatus := startRemoteStatus(target, constandstruct.MutationServerTimeout, logLine)
	var lines []string
	var err error
	if targetUser == nil {
		lines, err = remotefunc.RunRemoteScriptWithProgress(
			serverCtx, target.IP, user, password, scriptText, remoteProgressLogger(target, logLine),
		)
	} else {
		lines, err = remotefunc.RunRemoteScriptInUserSessionWithProgress(
			serverCtx, target.IP, user, password, scriptText, *targetUser, remoteProgressLogger(target, logLine),
		)
	}
	statusErr := err
	if statusErr == nil {
		if text := remoteError(lines); text != "" {
			statusErr = fmt.Errorf("%s", text)
		}
	}
	finishStatus(statusErr)
	if !logCallError(operationCtx, target, err, logLine) && err == nil {
		logPowerShellResult(target, lines, logLine, outputLine)
	}
}

func logPowerShellResult(
	target constandstruct.Server,
	lines []string,
	logLine, outputLine func(string),
) {
	if outputLine == nil {
		outputLine = logLine
	}
	if text := remoteError(lines); text != "" {
		logLine(fmt.Sprintf("%s: ошибка PowerShell: %s", target.Name, text))
		return
	}

	logRemoteWarnings(target, lines, logLine)
	hasOutput := false
	succeeded := false
	for _, line := range lines {
		switch {
		case line == "COMMAND_OK":
			succeeded = true
		case line == "COMMAND_OUTPUT_TRUNCATED":
			logLine(fmt.Sprintf("%s: вывод команды ограничен первыми %d символами.", target.Name, remotefunc.MaxRemoteOutputCharacters))
		case strings.HasPrefix(line, "COMMAND_LINE|"):
			hasOutput = true
			outputLine(fmt.Sprintf("%s > %s", target.Name, strings.TrimPrefix(line, "COMMAND_LINE|")))
		}
	}
	if succeeded && !hasOutput {
		logLine(fmt.Sprintf("%s: команда выполнена без текстового вывода.", target.Name))
	} else if succeeded {
		logLine(fmt.Sprintf("%s: команда выполнена успешно.", target.Name))
	} else {
		logLine(fmt.Sprintf("%s: ответ не содержит подтверждения выполнения.", target.Name))
	}
}
