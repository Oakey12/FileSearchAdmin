package remotefunc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	psscript "filesearch-admin/internal/psScript"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// ProgressFunc получает только служебные строки STAGE и TIMING по мере их
// появления в stdout PowerShell. Пользовательский вывод через неё не передаётся.
type ProgressFunc func(string)

type powerShellOutputCollector struct {
	mu       sync.Mutex
	output   bytes.Buffer
	pending  []byte
	progress ProgressFunc
}

func (collector *powerShellOutputCollector) Write(data []byte) (int, error) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	written, err := collector.output.Write(data)
	collector.pending = append(collector.pending, data...)
	collector.report(collector.takeCompleteLinesLocked(false))
	return written, err
}

func (collector *powerShellOutputCollector) result() []byte {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.report(collector.takeCompleteLinesLocked(true))
	result := append([]byte(nil), collector.output.Bytes()...)
	return result
}

func (collector *powerShellOutputCollector) takeCompleteLinesLocked(flush bool) [][]byte {
	var lines [][]byte
	for {
		index := bytes.IndexByte(collector.pending, '\n')
		if index < 0 {
			break
		}
		lines = append(lines, append([]byte(nil), collector.pending[:index]...))
		collector.pending = collector.pending[index+1:]
	}
	if flush && len(collector.pending) > 0 {
		lines = append(lines, append([]byte(nil), collector.pending...))
		collector.pending = nil
	}
	return lines
}

func (collector *powerShellOutputCollector) report(lines [][]byte) {
	if collector.progress == nil {
		return
	}
	for _, rawLine := range lines {
		line := strings.TrimSpace(decodePowerShellOutput(bytes.TrimSuffix(rawLine, []byte{'\r'})))
		if strings.HasPrefix(line, "STAGE|") || strings.HasPrefix(line, "TIMING|") {
			collector.progress(line)
		}
	}
}

func runPowerShellCommand(cmd *exec.Cmd, progress ProgressFunc) ([]byte, error) {
	if progress == nil {
		return cmd.CombinedOutput()
	}
	collector := &powerShellOutputCollector{progress: progress}
	cmd.Stdout = collector
	cmd.Stderr = collector
	runErr := cmd.Run()
	return collector.result(), runErr
}

type remoteStageError struct {
	cause error
	stage string
}

func (err *remoteStageError) Error() string {
	return fmt.Sprintf("%v; последний этап: %s", err.cause, err.stage)
}

func (err *remoteStageError) Unwrap() error { return err.cause }

// ErrorStage возвращает последний этап удалённой операции, успевший попасть в
// частичный вывод PowerShell до отмены или тайм-аута.
func ErrorStage(err error) string {
	var stageErr *remoteStageError
	if errors.As(err, &stageErr) {
		return stageErr.stage
	}
	return ""
}

var (
	tempScriptsMu sync.Mutex
	tempScripts   = make(map[string]struct{})
)

const (
	MaxRemoteScriptBytes             = 1024 * 1024
	MaxRemoteOutputCharacters        = 512 * 1024
	MaxRemoteOutputCharactersAllowed = 2 * 1024 * 1024
	maxPowerShellLineSize            = 8 * 1024 * 1024
)

func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: constandstruct.CREATE_NO_WINDOW,
	}
}

func registerTempScript(path string) {
	tempScriptsMu.Lock()
	tempScripts[path] = struct{}{}
	tempScriptsMu.Unlock()
}

func removeTempScript(path string) {
	_ = os.Remove(path)
	tempScriptsMu.Lock()
	delete(tempScripts, path)
	tempScriptsMu.Unlock()
}

// CleanupTempScripts удаляет временные скрипты, созданные текущим процессом.
func CleanupTempScripts() {
	tempScriptsMu.Lock()
	paths := make([]string, 0, len(tempScripts))
	for path := range tempScripts {
		paths = append(paths, path)
	}
	tempScriptsMu.Unlock()

	for _, path := range paths {
		removeTempScript(path)
	}

	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("fsadmin-%d-*.ps1", os.Getpid()))
	matches, _ := filepath.Glob(pattern)
	for _, path := range matches {
		removeTempScript(path)
	}
}

// CleanupStaleTempScripts удаляет остатки старых запусков приложения.
func CleanupStaleTempScripts(maxAge time.Duration) {
	if maxAge < 0 {
		return
	}

	pattern := filepath.Join(os.TempDir(), "fsadmin-*.ps1")
	matches, _ := filepath.Glob(pattern)
	now := time.Now()
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) >= maxAge {
			_ = os.Remove(path)
		}
	}
}

func writePowerShellFile(scriptText string) (string, error) {
	file, err := os.CreateTemp("", fmt.Sprintf("fsadmin-%d-*.ps1", os.Getpid()))
	if err != nil {
		return "", fmt.Errorf("не удалось создать временный скрипт: %w", err)
	}

	path := file.Name()
	registerTempScript(path)
	fail := func() {
		_ = file.Close()
		removeTempScript(path)
	}

	// BOM нужен для корректного чтения кириллицы Windows PowerShell 5.1.
	script := append([]byte{0xEF, 0xBB, 0xBF}, []byte(scriptText)...)
	if _, err = file.Write(script); err != nil {
		fail()
		return "", fmt.Errorf("не удалось записать временный скрипт: %w", err)
	}
	if err = file.Close(); err != nil {
		removeTempScript(path)
		return "", fmt.Errorf("не удалось закрыть временный скрипт: %w", err)
	}
	return path, nil
}

func writeRemoteScript() (string, error) {
	return writePowerShellFile(psscript.PsScript)
}

func RunRemote(
	ctx context.Context,
	serverIP, defaultUser, password, action, processNames, message string,
) ([]string, error) {
	return runRemote(ctx, serverIP, defaultUser, password, action, processNames, message, "", nil)
}

func RunRemoteWithProgress(
	ctx context.Context,
	serverIP, defaultUser, password, action, processNames, message string,
	progress ProgressFunc,
) ([]string, error) {
	return runRemote(ctx, serverIP, defaultUser, password, action, processNames, message, "", progress)
}

func RunRemoteKillPIDs(ctx context.Context, serverIP, defaultUser, password string, pids []int) ([]string, error) {
	if len(pids) == 0 {
		return nil, fmt.Errorf("не указаны PID для завершения")
	}

	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			return nil, fmt.Errorf("некорректный PID: %d", pid)
		}
		ids = append(ids, strconv.Itoa(pid))
	}
	return runRemote(
		ctx,
		serverIP,
		defaultUser,
		password,
		"killids",
		constandstruct.TargetProcessNames,
		"",
		strings.Join(ids, ","),
		nil,
	)
}

func RunRemoteKillPIDsWithProgress(
	ctx context.Context,
	serverIP, defaultUser, password string,
	pids []int,
	progress ProgressFunc,
) ([]string, error) {
	if len(pids) == 0 {
		return nil, fmt.Errorf("не указаны PID для завершения")
	}

	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			return nil, fmt.Errorf("некорректный PID: %d", pid)
		}
		ids = append(ids, strconv.Itoa(pid))
	}
	return runRemote(
		ctx,
		serverIP,
		defaultUser,
		password,
		"killids",
		constandstruct.TargetProcessNames,
		"",
		strings.Join(ids, ","),
		progress,
	)
}

// RunRemoteScript выполняет пользовательский PowerShell-сценарий через
// CIM/DCOM и Win32_Process.Create. Учётные данные и текст сценария передаются
// локальному процессу PowerShell через stdin.
func RunRemoteScript(ctx context.Context, serverIP, defaultUser, password, scriptText string) ([]string, error) {
	return RunRemoteScriptWithOutputLimit(
		ctx, serverIP, defaultUser, password, scriptText, MaxRemoteOutputCharacters,
	)
}

func RunRemoteScriptWithProgress(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	progress ProgressFunc,
) ([]string, error) {
	return RunRemoteScriptWithOutputLimitAndProgress(
		ctx, serverIP, defaultUser, password, scriptText, MaxRemoteOutputCharacters, progress,
	)
}

// RunRemoteScriptWithOutputLimit выполняет сценарий с указанным ограничением
// результата. Повышенный лимит предназначен для доверенных встроенных
// сценариев (например, инвентаризации), а не для произвольного пользовательского
// вывода.
func RunRemoteScriptWithOutputLimit(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	maxOutputCharacters int,
) ([]string, error) {
	return runRemoteScriptWithOutputLimit(
		ctx, serverIP, defaultUser, password, scriptText, maxOutputCharacters, nil, nil,
	)
}

func RunRemoteScriptWithOutputLimitAndProgress(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	maxOutputCharacters int,
	progress ProgressFunc,
) ([]string, error) {
	return runRemoteScriptWithOutputLimit(
		ctx, serverIP, defaultUser, password, scriptText, maxOutputCharacters, nil, progress,
	)
}

// RunRemoteScriptInUserSession выполняет сценарий с интерактивным токеном
// выбранного вошедшего пользователя. Административные учётные данные нужны
// только для подключения и регистрации одноразового задания; пароль выбранного
// пользователя не запрашивается и не передаётся.
func RunRemoteScriptInUserSession(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	targetUser constandstruct.RemoteUser,
) ([]string, error) {
	return runRemoteScriptWithOutputLimit(
		ctx,
		serverIP,
		defaultUser,
		password,
		scriptText,
		MaxRemoteOutputCharacters,
		&targetUser,
		nil,
	)
}

func RunRemoteScriptInUserSessionWithProgress(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	targetUser constandstruct.RemoteUser,
	progress ProgressFunc,
) ([]string, error) {
	return runRemoteScriptWithOutputLimit(
		ctx,
		serverIP,
		defaultUser,
		password,
		scriptText,
		MaxRemoteOutputCharacters,
		&targetUser,
		progress,
	)
}

func runRemoteScriptWithOutputLimit(
	ctx context.Context,
	serverIP, defaultUser, password, scriptText string,
	maxOutputCharacters int,
	targetUser *constandstruct.RemoteUser,
	progress ProgressFunc,
) ([]string, error) {
	serverIP = strings.TrimSpace(serverIP)
	defaultUser = strings.TrimSpace(defaultUser)
	if serverIP == "" {
		return nil, fmt.Errorf("не указан адрес сервера")
	}
	if defaultUser == "" {
		return nil, fmt.Errorf("не указан логин")
	}
	if password == "" {
		return nil, fmt.Errorf("не указан пароль")
	}
	if strings.TrimSpace(scriptText) == "" {
		return nil, fmt.Errorf("сценарий PowerShell пуст")
	}
	if len([]byte(scriptText)) > MaxRemoteScriptBytes {
		return nil, fmt.Errorf("сценарий превышает допустимый размер %d байт", MaxRemoteScriptBytes)
	}
	if maxOutputCharacters < 1024 || maxOutputCharacters > MaxRemoteOutputCharactersAllowed {
		return nil, fmt.Errorf(
			"лимит результата должен быть от 1024 до %d символов",
			MaxRemoteOutputCharactersAllowed,
		)
	}
	if targetUser != nil {
		targetUser.SID = strings.TrimSpace(targetUser.SID)
		targetUser.DisplayName = strings.TrimSpace(targetUser.DisplayName)
		targetUser.Username = strings.TrimSpace(targetUser.Username)
		targetUser.Domain = strings.TrimSpace(targetUser.Domain)
		if !strings.HasPrefix(targetUser.SID, "S-1-5-21-") || targetUser.SessionID == 0 {
			return nil, fmt.Errorf("некорректная пользовательская сессия; повторите сканирование сервера")
		}
		if targetUser.DisplayName == "" {
			targetUser.DisplayName = targetUser.Username
			if targetUser.Domain != "" {
				targetUser.DisplayName = targetUser.Domain + `\` + targetUser.Username
			}
		}
		if targetUser.DisplayName == "" || strings.ContainsAny(targetUser.DisplayName, "\r\n\x00") {
			return nil, fmt.Errorf("не удалось определить имя выбранного пользователя")
		}
	}
	resolvedUser, err := resolveRemoteUsername(ctx, serverIP, defaultUser)
	if err != nil {
		return nil, err
	}
	defaultUser = resolvedUser
	jobID, err := newRemoteJobID()
	if err != nil {
		return nil, err
	}

	scriptPath, err := writePowerShellFile(psscript.CommandScript)
	if err != nil {
		return nil, err
	}
	defer removeTempScript(scriptPath)

	args := []string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "RemoteSigned",
		"-File", scriptPath,
		"-ComputerName", serverIP,
		"-DefaultUser", defaultUser,
		"-CredentialAndScriptFromStdin",
		"-JobId", jobID,
		"-RemoteTimeoutSeconds", strconv.Itoa(commandRemoteTimeoutSeconds(ctx)),
		"-MaxOutputCharacters", strconv.Itoa(maxOutputCharacters),
	}
	if targetUser != nil {
		args = append(
			args,
			"-TargetUserSID", targetUser.SID,
			"-TargetUserName", targetUser.DisplayName,
			"-TargetSessionID", strconv.FormatUint(uint64(targetUser.SessionID), 10),
		)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
	cmd.SysProcAttr = hideWindowAttr()
	cmd.Stdin = strings.NewReader(
		encodePasswordForStdin(password) + "\n" +
			base64.StdEncoding.EncodeToString([]byte(scriptText)) + "\n",
	)

	output, runErr := runPowerShellCommand(cmd, progress)
	rawLines, scanErr := scanPowerShellLines(output)
	if contextErr := ctx.Err(); contextErr != nil {
		_ = cleanupRemoteCommandJob(serverIP, defaultUser, password, jobID, targetUser)
		if scanErr == nil {
			if stage := lastRemoteStage(rawLines); stage != "" {
				return rawLines, &remoteStageError{cause: contextErr, stage: stage}
			}
		}
		return rawLines, contextErr
	}
	if scanErr != nil {
		_ = cleanupRemoteCommandJob(serverIP, defaultUser, password, jobID, targetUser)
		return nil, scanErr
	}
	lines := rawLines
	lines, err = decodeRemoteCommandOutput(lines)
	if err != nil {
		_ = cleanupRemoteCommandJob(serverIP, defaultUser, password, jobID, targetUser)
		return nil, err
	}
	result, err := interpretPowerShellResult(lines, runErr)
	if err != nil {
		if stage := lastRemoteStage(lines); stage != "" {
			err = &remoteStageError{cause: err, stage: stage}
		}
	}
	if err != nil {
		if cleanupErr := cleanupRemoteCommandJob(serverIP, defaultUser, password, jobID, targetUser); cleanupErr != nil {
			return nil, fmt.Errorf("%w; аварийная очистка не завершена: %v", err, cleanupErr)
		}
	}
	if progress != nil {
		result = withoutProgressLines(result)
	}
	return result, err
}

func newRemoteJobID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("не удалось создать идентификатор удалённого задания: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func cleanupRemoteCommandJob(
	serverIP, defaultUser, password, jobID string,
	targetUser *constandstruct.RemoteUser,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	scriptPath, err := writePowerShellFile(psscript.CleanupCommandScript)
	if err != nil {
		return err
	}
	defer removeTempScript(scriptPath)

	args := []string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "RemoteSigned",
		"-File", scriptPath,
		"-ComputerName", serverIP,
		"-DefaultUser", defaultUser,
		"-JobId", jobID,
		"-CredentialFromStdin",
	}
	if targetUser != nil {
		args = append(args, "-TargetUserSID", targetUser.SID)
	}
	cmd := exec.CommandContext(cleanupCtx, "powershell.exe", args...)
	cmd.SysProcAttr = hideWindowAttr()
	cmd.Stdin = strings.NewReader(encodePasswordForStdin(password) + "\n")
	output, runErr := cmd.CombinedOutput()
	if err := cleanupCtx.Err(); err != nil {
		return err
	}
	lines, err := scanPowerShellLines(output)
	if err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("PowerShell очистки завершился с ошибкой: %w; вывод: %s", runErr, strings.Join(lines, "; "))
	}
	return nil
}

func commandRemoteTimeoutSeconds(ctx context.Context) int {
	const (
		fallbackSeconds = 110
		cleanupReserve  = 5 * time.Second
		minimumSeconds  = 5
	)
	deadline, ok := ctx.Deadline()
	if !ok {
		return fallbackSeconds
	}
	remaining := time.Until(deadline) - cleanupReserve
	seconds := int(remaining / time.Second)
	if seconds < minimumSeconds {
		return minimumSeconds
	}
	return seconds
}

func runRemote(
	ctx context.Context,
	serverIP, defaultUser, password, action, processNames, message, processIDs string,
	progress ProgressFunc,
) ([]string, error) {
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		return nil, fmt.Errorf("не указан адрес сервера")
	}
	defaultUser = strings.TrimSpace(defaultUser)
	if defaultUser == "" {
		return nil, fmt.Errorf("не указан логин")
	}
	if password == "" {
		return nil, fmt.Errorf("не указан пароль")
	}
	resolvedUser, err := resolveRemoteUsername(ctx, serverIP, defaultUser)
	if err != nil {
		return nil, err
	}
	defaultUser = resolvedUser

	scriptPath, err := writeRemoteScript()
	if err != nil {
		return nil, err
	}
	defer removeTempScript(scriptPath)

	args := []string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "RemoteSigned",
		"-File", scriptPath,
		"-ComputerName", serverIP,
		"-Action", action,
		"-ProcessNames", processNames,
		"-Message", message,
		"-DefaultUser", defaultUser,
		"-CredentialFromStdin",
	}
	if processIDs != "" {
		args = append(args, "-ProcessIds", processIDs)
	}

	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
	cmd.SysProcAttr = hideWindowAttr()
	cmd.Stdin = strings.NewReader(encodePasswordForStdin(password) + "\n")

	output, runErr := runPowerShellCommand(cmd, progress)
	lines, err := scanPowerShellLines(output)
	if err != nil {
		return nil, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		if stage := lastRemoteStage(lines); stage != "" {
			return lines, &remoteStageError{cause: contextErr, stage: stage}
		}
		return lines, contextErr
	}
	result, resultErr := interpretPowerShellResult(lines, runErr)
	if resultErr != nil {
		if stage := lastRemoteStage(lines); stage != "" {
			resultErr = &remoteStageError{cause: resultErr, stage: stage}
		}
	}
	if progress != nil {
		result = withoutProgressLines(result)
	}
	return result, resultErr
}

func withoutProgressLines(lines []string) []string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "STAGE|") || strings.HasPrefix(line, "TIMING|") {
			continue
		}
		filtered = append(filtered, line)
	}
	return filtered
}

func lastRemoteStage(lines []string) string {
	for index := len(lines) - 1; index >= 0; index-- {
		if stage, ok := strings.CutPrefix(lines[index], "STAGE|"); ok {
			return strings.TrimSpace(stage)
		}
	}
	return ""
}

func scanPowerShellLines(output []byte) ([]string, error) {
	var lines []string
	decodedOutput := decodePowerShellOutput(output)
	scanner := bufio.NewScanner(strings.NewReader(decodedOutput))
	scanner.Buffer(make([]byte, 64*1024), maxPowerShellLineSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ошибка чтения вывода PowerShell: %w", err)
	}
	return lines, nil
}

func decodeRemoteCommandOutput(lines []string) ([]string, error) {
	decodedLines := make([]string, 0, len(lines))
	var encodedOutput strings.Builder
	flushOutput := func() error {
		if encodedOutput.Len() == 0 {
			return nil
		}
		decoded, err := base64.StdEncoding.DecodeString(encodedOutput.String())
		encodedOutput.Reset()
		if err != nil {
			return fmt.Errorf("не удалось декодировать вывод удалённой команды: %w", err)
		}
		for _, outputLine := range strings.Split(strings.ReplaceAll(string(decoded), "\r\n", "\n"), "\n") {
			if outputLine != "" {
				decodedLines = append(decodedLines, "COMMAND_LINE|"+outputLine)
			}
		}
		return nil
	}

	for _, line := range lines {
		encoded, ok := strings.CutPrefix(line, "COMMAND_OUTPUT|")
		if ok {
			encodedOutput.WriteString(encoded)
			continue
		}
		if err := flushOutput(); err != nil {
			return nil, err
		}
		decodedLines = append(decodedLines, line)
	}
	if err := flushOutput(); err != nil {
		return nil, err
	}
	return decodedLines, nil
}

func decodePowerShellOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	if utf8.Valid(output) {
		return strings.TrimPrefix(string(output), "\uFEFF")
	}

	decoded, err := charmap.CodePage866.NewDecoder().Bytes(output)
	if err != nil {
		return string(output)
	}
	return string(decoded)
}

func encodePasswordForStdin(password string) string {
	return base64.StdEncoding.EncodeToString([]byte(password))
}

func interpretPowerShellResult(lines []string, runErr error) ([]string, error) {
	if runErr == nil {
		return lines, nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "ERROR|") {
			return lines, nil
		}
	}
	if len(lines) > 0 {
		return nil, fmt.Errorf(
			"PowerShell завершился с ошибкой: %w; вывод: %s",
			runErr,
			strings.Join(lines, "; "),
		)
	}
	return nil, fmt.Errorf("ошибка выполнения PowerShell: %w", runErr)
}
