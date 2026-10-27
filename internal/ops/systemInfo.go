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
	"strings"
)

type systemInfoWarning struct {
	Stage   string `json:"Stage"`
	Message string `json:"Message"`
}

// ScanSystemInfo собирает сводку о целевом компьютере в фоне. Учётные данные
// нужны только для CIM/DCOM-подключения и не сохраняются.
func ScanSystemInfo(
	ctx context.Context,
	targetIP, username, password string,
	logLine func(string),
	onComplete func(constandstruct.SystemInfo, []string, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	if net.ParseIP(targetIP) == nil {
		logLine("Сведения о системе: введите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" {
		logLine("Сведения о системе: введите логин администратора в формате DOMAIN\\username или .\\localadmin.")
		return false
	}
	if password == "" {
		logLine("Сведения о системе: введите пароль. Пароль не сохраняется и не записывается в журнал.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		target := constandstruct.Server{Name: "Сведения о системе", IP: targetIP}
		info, warnings, resultErr := scanSystemInfoOn(operationBaseCtx, target, username, password, logLine)
		logLine("--- Сбор сведений о системе завершён ---")
		if onComplete != nil {
			onComplete(info, warnings, resultErr)
		}
	}()
	return true
}

// scanSystemInfoOn собирает сведения об одном компьютере (без beginOperation,
// со своим тайм-аутом на компьютер) — общее для одиночного сбора и сбора по
// списку. Логирует по строке на компьютер.
func scanSystemInfoOn(
	ctx context.Context,
	target constandstruct.Server,
	username, password string,
	logLine func(string),
) (constandstruct.SystemInfo, []string, error) {
	targetCtx, cancel := context.WithTimeout(ctx, constandstruct.ProgramScanTimeout)
	defer cancel()

	logLine(fmt.Sprintf("=== Сбор сведений о системе на %s ===", target.IP))
	finishStatus := startRemoteStatus(target, constandstruct.ProgramScanTimeout, logLine)

	lines, runErr := remotefunc.RunRemoteScriptWithOutputLimitAndProgress(
		targetCtx, target.IP, username, password,
		psscript.SystemInfoScript, remotefunc.MaxRemoteOutputCharacters,
		remoteProgressLogger(target, logLine),
	)

	info, warnings, parseErr := parseSystemInfo(lines)
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
		logLine(fmt.Sprintf("%s: сведения о системе: %s", target.IP, resultErr.Error()))
	} else {
		logLine(fmt.Sprintf("%s: сведения получены: %s; предупреждений: %d.",
			target.IP, describeSystemInfoTarget(info, target.IP), len(warnings)))
	}
	for _, warning := range warnings {
		logLine(fmt.Sprintf("%s: предупреждение: %s", target.IP, warning))
	}
	return info, warnings, resultErr
}

// ScanSystemInfoOnTargets собирает сведения о системе с каждого компьютера
// списка параллельно. Ответ каждого уходит в onResult сразу, onDone — один раз
// в конце (в т.ч. после отмены). false — сбор не начался (причина в журнале).
func ScanSystemInfoOnTargets(
	ctx context.Context,
	targets []constandstruct.Server,
	user, password string,
	logLine func(string),
	onResult func(constandstruct.SystemInfoResult),
	onDone func(),
) bool {
	if len(targets) == 0 {
		logLine("Сведения о системе: выберите серверы или введите IP вручную.")
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
		logLine(fmt.Sprintf("=== Сбор сведений о системе: компьютеров %d ===", len(targets)))
		runTargetsLimit(operationCtx, targets, constandstruct.MaxParallelSystemInfo, func(target constandstruct.Server) {
			info, _, err := scanSystemInfoOn(operationCtx, target, user, password, logLine)
			result := constandstruct.SystemInfoResult{Name: target.Name, IP: target.IP, Info: info}
			if err != nil {
				result.Error = checkErrorText(operationCtx, err)
			}
			if onResult != nil {
				onResult(result)
			}
		})
		if operationCtx.Err() != nil {
			logLine("Сбор остановлен: " + contextErrorText(operationCtx.Err()))
		}
		logLine("--- Сбор сведений о системе завершён ---")
		if onDone != nil {
			onDone()
		}
	}()
	return true
}

func describeSystemInfoTarget(info constandstruct.SystemInfo, targetIP string) string {
	if strings.TrimSpace(info.ComputerName) == "" {
		return targetIP
	}
	return info.ComputerName
}

// parseSystemInfo разбирает ответ удалённого скрипта. Сводка приходит одной
// Base64-записью, предупреждения — отдельными строками.
func parseSystemInfo(lines []string) (constandstruct.SystemInfo, []string, error) {
	var info constandstruct.SystemInfo
	var warnings []string
	found := false

	for _, line := range lines {
		if payload, ok := strings.CutPrefix(line, "COMMAND_LINE|INFO_WARNING|"); ok {
			data, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return info, warnings, fmt.Errorf("повреждено предупреждение: %w", err)
			}
			var warning systemInfoWarning
			if err := json.Unmarshal(data, &warning); err != nil {
				return info, warnings, fmt.Errorf("не удалось разобрать предупреждение: %w", err)
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

		payload, ok := strings.CutPrefix(line, "COMMAND_LINE|SYSTEM_INFO|")
		if !ok {
			continue
		}
		if found {
			return info, warnings, fmt.Errorf("ответ содержит повторную сводку")
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return info, warnings, fmt.Errorf("повреждена сводка: %w", err)
		}
		if err := json.Unmarshal(data, &info); err != nil {
			return info, warnings, fmt.Errorf("не удалось разобрать сводку: %w", err)
		}
		found = true
	}

	if !found {
		return info, warnings, fmt.Errorf("целевой компьютер не вернул сведения о системе")
	}
	return info, warnings, nil
}

// FormatSystemInfo раскладывает сводку в пары «поле — значение» для таблицы.
// Пустые поля пропускаются: показывать пользователю пустые строки незачем.
func FormatSystemInfo(info constandstruct.SystemInfo) [][2]string {
	domain := info.Domain
	if domain != "" {
		if info.PartOfDomain {
			domain += " (домен)"
		} else {
			domain += " (рабочая группа)"
		}
	}

	processor := info.ProcessorName
	if info.ProcessorCores > 0 || info.ProcessorLogical > 0 {
		processor = strings.TrimSpace(fmt.Sprintf(
			"%s — ядер: %d, потоков: %d", processor, info.ProcessorCores, info.ProcessorLogical,
		))
	}
	if info.ProcessorClockMHz > 0 {
		processor += fmt.Sprintf(", %d МГц", info.ProcessorClockMHz)
	}

	memory := ""
	if info.MemoryTotalGB != "" {
		memory = info.MemoryTotalGB + " ГБ"
		if info.MemoryFreeGB != "" {
			memory += " (свободно " + info.MemoryFreeGB + " ГБ)"
		}
	}

	hardware := strings.TrimSpace(strings.Join([]string{info.Manufacturer, info.Model}, " "))
	osText := info.OSCaption
	if info.OSArchitecture != "" {
		osText = strings.TrimSpace(osText + ", " + info.OSArchitecture)
	}
	osBuild := info.OSVersion
	if info.OSBuild != "" {
		osBuild = strings.TrimSpace(osBuild + " (сборка " + info.OSBuild + ")")
	}

	rows := [][2]string{
		{"Имя компьютера", info.ComputerName},
		{"Домен", domain},
		{"Активный пользователь", info.LoggedOnUser},
		{"Windows", osText},
		{"Версия", osBuild},
		{"Установлена", info.OSInstallDate},
		{"Последняя загрузка", info.LastBootTime},
		{"Время работы", info.Uptime},
		{"Процессор", processor},
		{"Оперативная память", memory},
		{"Оборудование", hardware},
		{"Серийный номер", info.SerialNumber},
		{"IP-адреса", strings.Join(info.IPAddresses, ", ")},
		{"Диски", strings.Join(info.Disks, "; ")},
		// Папки каждая со своей строки: на терминальном сервере их десятки, и
		// склеенные через точку с запятой они нечитаемы.
		{`Папки C:\Users`, strings.Join(info.UserFolders, "\n")},
		// Кто и откуда заходит на компьютер по RDP — тоже по строке на пару
		// «учётная запись — адрес клиента».
		{fmt.Sprintf("Подключения по RDP (%d дн.)", psscript.RDPHistoryDays), strings.Join(info.RDPConnections, "\n")},
	}

	filtered := make([][2]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row[1]) != "" {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
