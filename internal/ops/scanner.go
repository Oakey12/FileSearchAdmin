package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	psscript "filesearch-admin/internal/psScript"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const installedProgramScanRetryDelay = 1500 * time.Millisecond

type installedProgramScanWarning struct {
	Stage   string `json:"Stage"`
	Message string `json:"Message"`
}

type installedProgramScriptRunner func(
	context.Context, string, string, string, string, int,
) ([]string, error)

// ScanInstalledPrograms запускает инвентаризацию программ в фоне. Учётные
// данные используются только для CIM/DCOM-подключения и не сохраняются.
func ScanInstalledPrograms(
	ctx context.Context,
	targetIP, username, password string,
	logLine func(string),
	onComplete func([]constandstruct.InstalledProgram, []string, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	if net.ParseIP(targetIP) == nil {
		logLine("Сканер: введите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" {
		logLine("Сканер: введите логин администратора в формате DOMAIN\\username или .\\localadmin.")
		return false
	}
	if password == "" {
		logLine("Сканер: введите пароль. Пароль не сохраняется и не записывается в журнал.")
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

		target := constandstruct.Server{Name: "Сканер", IP: targetIP}
		logLine(fmt.Sprintf("=== Инвентаризация программ на %s ===", targetIP))
		finishStatus := startRemoteStatus(target, constandstruct.ProgramScanTimeout, logLine)
		runner := func(
			runCtx context.Context,
			serverIP, defaultUser, runPassword, scriptText string,
			maxOutputCharacters int,
		) ([]string, error) {
			return remotefunc.RunRemoteScriptWithOutputLimitAndProgress(
				runCtx,
				serverIP,
				defaultUser,
				runPassword,
				scriptText,
				maxOutputCharacters,
				remoteProgressLogger(target, logLine),
			)
		}
		programs, warnings, lines, resultErr := runInstalledProgramScan(
			operationCtx,
			targetIP,
			username,
			password,
			logLine,
			runner,
		)
		finishStatus(resultErr)
		logInstalledProgramWarnings(target, warnings, logLine)

		if resultErr != nil {
			if operationCtx.Err() != nil {
				logCallError(operationCtx, target, resultErr, logLine)
			} else {
				logLine(fmt.Sprintf("Сканер: ошибка получения списка программ: %v", resultErr))
			}
		} else {
			logRemoteWarnings(target, lines, logLine)
			logLine(fmt.Sprintf(
				"Сканер: найдено программ: %d; предупреждений: %d.",
				len(programs), len(warnings),
			))
		}
		logLine("--- Инвентаризация программ завершена ---")

		if onComplete != nil {
			onComplete(programs, warnings, resultErr)
		}
	}()
	return true
}

func runInstalledProgramScan(
	ctx context.Context,
	targetIP, username, password string,
	logLine func(string),
	run installedProgramScriptRunner,
) ([]constandstruct.InstalledProgram, []string, []string, error) {
	var lastLines []string
	var lastWarnings []string
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		lines, runErr := run(
			ctx,
			targetIP,
			username,
			password,
			psscript.InstalledProgramsScript,
			remotefunc.MaxRemoteOutputCharactersAllowed,
		)
		lastLines = lines
		if runErr == nil {
			programs, warnings, parseErr := parseInstalledPrograms(lines)
			lastWarnings = warnings
			if parseErr == nil {
				return programs, warnings, lines, nil
			}
			lastErr = parseErr
		} else {
			lastErr = runErr
		}
		if ctx.Err() != nil {
			return nil, lastWarnings, lastLines, ctx.Err()
		}
		if attempt == 2 || !isTransientInstalledProgramScanError(lastErr) {
			break
		}
		logLine(fmt.Sprintf(
			"Сканер: временный сбой подключения (%v). Повторная попытка 2 из 2 через %s...",
			lastErr,
			installedProgramScanRetryDelay,
		))
		timer := time.NewTimer(installedProgramScanRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, lastWarnings, lastLines, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastWarnings, lastLines, lastErr
}

func isTransientInstalledProgramScanError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"timeout", "timed out", "time-out", "истечени", "время ожидания", "тайм-аут",
		"rpc server is unavailable", "сервер rpc недоступен", "0x800706ba", "0x8001011f",
		"network path was not found", "сетевой путь не найден", "call was canceled", "вызов отменен",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func parseInstalledPrograms(lines []string) ([]constandstruct.InstalledProgram, []string, error) {
	if text := remoteError(lines); text != "" {
		return nil, nil, fmt.Errorf("удалённый компьютер вернул ошибку: %s", text)
	}

	programs := make([]constandstruct.InstalledProgram, 0)
	warnings := make([]string, 0)
	seen := make(map[string]struct{})
	confirmed := false
	summarySeen := false
	scannedSources := 0
	failedSources := 0
	for _, line := range lines {
		if line == "COMMAND_OUTPUT_TRUNCATED" {
			return nil, nil, fmt.Errorf("ответ сканера был обрезан; полный список программ не получен")
		}
		if line == "COMMAND_OK" {
			confirmed = true
			continue
		}
		if summary, ok := strings.CutPrefix(line, "COMMAND_LINE|SCAN_SUMMARY|"); ok {
			if summarySeen {
				return nil, nil, fmt.Errorf("ответ содержит повторную сводку сканирования")
			}
			parts := strings.Split(summary, "|")
			if len(parts) != 2 {
				return nil, nil, fmt.Errorf("повреждена сводка сканирования")
			}
			var err error
			scannedSources, err = strconv.Atoi(parts[0])
			if err != nil || scannedSources < 0 {
				return nil, nil, fmt.Errorf("повреждено количество прочитанных веток")
			}
			failedSources, err = strconv.Atoi(parts[1])
			if err != nil || failedSources < 0 {
				return nil, nil, fmt.Errorf("повреждено количество ошибочных веток")
			}
			summarySeen = true
			continue
		}
		if payload, ok := strings.CutPrefix(line, "COMMAND_LINE|SCAN_WARNING|"); ok {
			data, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return nil, nil, fmt.Errorf("повреждено предупреждение сканера: %w", err)
			}
			var warning installedProgramScanWarning
			if err := json.Unmarshal(data, &warning); err != nil {
				return nil, nil, fmt.Errorf("не удалось разобрать предупреждение сканера: %w", err)
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
		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|PROGRAM|")
		if !ok {
			continue
		}

		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("повреждена запись программы: %w", err)
		}
		var program constandstruct.InstalledProgram
		if err := json.Unmarshal(data, &program); err != nil {
			return nil, nil, fmt.Errorf("не удалось разобрать запись программы: %w", err)
		}
		program.Name = strings.TrimSpace(program.Name)
		program.Version = strings.TrimSpace(program.Version)
		program.Publisher = strings.TrimSpace(program.Publisher)
		program.InstallDate = strings.TrimSpace(program.InstallDate)
		program.Architecture = strings.TrimSpace(program.Architecture)
		program.Owner = strings.TrimSpace(program.Owner)
		program.RegistryPath = strings.TrimSpace(program.RegistryPath)
		program.UninstallDescription = strings.TrimSpace(program.UninstallDescription)
		if program.Name == "" {
			continue
		}

		key := strings.ToLower(program.RegistryPath)
		if key == "" {
			key = strings.ToLower(strings.Join([]string{
				program.Name,
				program.Version,
				program.Publisher,
				program.Architecture,
			}, "\x00"))
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		programs = append(programs, program)
	}
	if !confirmed {
		return nil, nil, fmt.Errorf("ответ не содержит подтверждения завершения сканирования")
	}
	if !summarySeen {
		return nil, nil, fmt.Errorf("ответ не содержит сводки о прочитанных ветках реестра")
	}
	if scannedSources == 0 {
		return nil, warnings, fmt.Errorf("не удалось прочитать ни одной ветки установленных программ")
	}
	if failedSources > len(warnings) {
		warnings = append(warnings, fmt.Sprintf(
			"не удалось прочитать веток реестра: %d", failedSources,
		))
	}

	sort.SliceStable(programs, func(left, right int) bool {
		leftName := strings.ToLower(programs[left].Name)
		rightName := strings.ToLower(programs[right].Name)
		if leftName == rightName {
			return programs[left].Version < programs[right].Version
		}
		return leftName < rightName
	})
	return programs, warnings, nil
}

func logInstalledProgramWarnings(
	target constandstruct.Server,
	warnings []string,
	logLine func(string),
) {
	for _, warning := range warnings {
		logLine(fmt.Sprintf("%s: неполное сканирование: %s", target.Name, warning))
	}
}
