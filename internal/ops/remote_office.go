package ops

import (
	"context"
	"fmt"
	"io"
	constandstruct "filesearch-admin/internal/constAndStruct"
	nativedialog "filesearch-admin/internal/nativeDialog"
	remotefunc "filesearch-admin/internal/remoteFunc"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var externalDocumentExtensions = map[string]struct{}{
	".accdb": {}, ".doc": {}, ".docm": {}, ".docx": {}, ".dot": {},
	".dotm": {}, ".dotx": {}, ".mpp": {}, ".odp": {}, ".ods": {},
	".odt": {}, ".pdf": {}, ".pot": {}, ".potm": {}, ".potx": {},
	".pps": {}, ".ppsm": {}, ".ppsx": {}, ".ppt": {}, ".pptm": {},
	".pptx": {}, ".pub": {}, ".rtf": {}, ".vsd": {}, ".vsdx": {},
	".xls": {}, ".xlsb": {}, ".xlsm": {}, ".xlsx": {}, ".xlt": {},
	".xltm": {}, ".xltx": {},
}

var macroEnabledDocumentExtensions = map[string]struct{}{
	".docm": {}, ".dotm": {}, ".potm": {}, ".ppsm": {}, ".pptm": {},
	".xlsb": {}, ".xlsm": {}, ".xltm": {},
}

var internalStructuredDocumentExtensions = map[string]struct{}{
	".docx": {}, ".xlsx": {},
}

var externalDocumentSessionCount atomic.Int64

// RemoteOfficeSession удерживает временное SMB-подключение, пока документ
// открыт в Word/Excel или другом штатном приложении Windows.
type RemoteOfficeSession struct {
	File       constandstruct.RemoteFile
	UNCPath    string
	BackupPath string
	share      *remotefunc.RemoteShare
	mu         sync.Mutex
	closed     bool
}

func CanEditExternalDocument(path string) bool {
	_, ok := externalDocumentExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func CanEditInternalDocument(path string) bool {
	_, ok := internalStructuredDocumentExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func IsMacroEnabledDocument(path string) bool {
	_, ok := macroEnabledDocumentExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func (session *RemoteOfficeSession) OpenAgain() error {
	if session == nil || session.UNCPath == "" {
		return fmt.Errorf("сеанс внешнего редактора не создан")
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return fmt.Errorf("подключение к документу уже закрыто")
	}
	path := session.UNCPath
	session.mu.Unlock()
	return nativedialog.OpenWithDefaultApplication(path)
}

func (session *RemoteOfficeSession) Close() error {
	if session == nil || session.share == nil {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil
	}
	if err := session.share.Close(); err != nil {
		return err
	}
	session.closed = true
	externalDocumentSessionCount.Add(-1)
	return nil
}

func HasOpenExternalDocumentSessions() bool {
	return externalDocumentSessionCount.Load() > 0
}

// OpenRemoteInternalDocument создаёт SMB-сеанс для встроенного редактора
// DOCX/XLSX. Постоянная резервная копия не создаётся: редактор сохраняет
// изменения непосредственно в исходный документ.
func OpenRemoteInternalDocument(
	ctx context.Context,
	targetIP, username, password string,
	file constandstruct.RemoteFile,
	logLine func(string),
	onComplete func(*RemoteOfficeSession, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	file.FullPath = filepath.Clean(strings.TrimSpace(file.FullPath))
	if net.ParseIP(targetIP) == nil {
		logLine("Редактор Office: укажите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" || password == "" {
		logLine("Редактор Office: введите логин и пароль администратора целевого компьютера.")
		return false
	}
	if !CanEditInternalDocument(file.FullPath) {
		logLine("Редактор Office: встроенный редактор поддерживает только DOCX и XLSX.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}
	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()
		logLine(fmt.Sprintf("=== Загрузка документа Office %q на %s ===", file.Name, targetIP))
		share, uncPath, err := remotefunc.ConnectRemoteFileShare(
			operationCtx, targetIP, username, password, file.FullPath,
		)
		var session *RemoteOfficeSession
		if err == nil {
			_, err = verifyRemoteDocumentSnapshot(uncPath, file)
		}
		if err == nil {
			session = &RemoteOfficeSession{File: file, UNCPath: uncPath, share: share}
			externalDocumentSessionCount.Add(1)
			logLine(fmt.Sprintf("Редактор Office: исходный документ %q подключён без создания резервной копии.", file.Name))
		} else if share != nil {
			_ = share.Close()
		}
		if err != nil {
			logLine("Редактор Office: ошибка загрузки: " + err.Error())
		}
		logLine("--- Загрузка документа Office завершена ---")
		if onComplete != nil {
			onComplete(session, err)
		}
	}()
	return true
}

// OpenRemoteOfficeDocument подключает административный SMB-ресурс целевого
// диска, проверяет снимок файла, создаёт резервную копию и открывает оригинал
// штатным приложением Windows. Office сохраняет изменения прямо в удалённый
// профиль; содержимое документа не проходит через журнал приложения.
func OpenRemoteOfficeDocument(
	ctx context.Context,
	targetIP, username, password string,
	file constandstruct.RemoteFile,
	logLine func(string),
	onComplete func(*RemoteOfficeSession, error),
) bool {
	targetIP = strings.TrimSpace(targetIP)
	username = strings.TrimSpace(username)
	file.FullPath = filepath.Clean(strings.TrimSpace(file.FullPath))
	if net.ParseIP(targetIP) == nil {
		logLine("Внешний редактор: укажите корректный IP-адрес целевого компьютера.")
		return false
	}
	if username == "" || password == "" {
		logLine("Внешний редактор: введите логин и пароль администратора целевого компьютера.")
		return false
	}
	if !CanEditExternalDocument(file.FullPath) {
		logLine("Внешний редактор: формат выбранного документа не поддерживается.")
		return false
	}
	operationBaseCtx, started := beginOperation(ctx, logLine)
	if !started {
		return false
	}

	go func() {
		defer finishOperation()
		operationCtx, cancel := context.WithTimeout(operationBaseCtx, constandstruct.FileOperationTimeout)
		defer cancel()
		logLine(fmt.Sprintf("=== Открытие внешнего документа %q на %s ===", file.Name, targetIP))
		var session *RemoteOfficeSession
		var resultErr error
		share, uncPath, err := remotefunc.ConnectRemoteFileShare(
			operationCtx,
			targetIP,
			username,
			password,
			file.FullPath,
		)
		if err != nil {
			resultErr = err
		} else {
			backupPath, backupErr := prepareRemoteDocumentBackup(operationCtx, uncPath, file)
			if backupErr != nil {
				_ = share.Close()
				resultErr = backupErr
			} else if openErr := nativedialog.OpenWithDefaultApplication(uncPath); openErr != nil {
				_ = os.Remove(backupPath)
				_ = share.Close()
				resultErr = openErr
			} else {
				session = &RemoteOfficeSession{
					File:       file,
					UNCPath:    uncPath,
					BackupPath: backupPath,
					share:      share,
				}
				externalDocumentSessionCount.Add(1)
				logLine(fmt.Sprintf(
					"Внешний редактор: документ %q открыт; резервная копия создана рядом с оригиналом.",
					file.Name,
				))
			}
		}
		if resultErr != nil {
			logLine("Внешний редактор: ошибка открытия: " + resultErr.Error())
		}
		logLine("--- Открытие внешнего документа завершено ---")
		if onComplete != nil {
			onComplete(session, resultErr)
		}
	}()
	return true
}

func prepareRemoteDocumentBackup(
	ctx context.Context,
	uncPath string,
	file constandstruct.RemoteFile,
) (string, error) {
	before, err := verifyRemoteDocumentSnapshot(uncPath, file)
	if err != nil {
		return "", err
	}

	backupPath := fmt.Sprintf(
		"%s.1cproc-backup-%s.bak",
		uncPath,
		time.Now().UTC().Format("20060102150405.000000000"),
	)
	if err := copyFileWithContext(ctx, uncPath, backupPath); err != nil {
		return "", fmt.Errorf("не удалось создать резервную копию документа: %w", err)
	}
	after, err := os.Stat(uncPath)
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("документ изменился во время резервного копирования; повторите операцию")
	}
	return backupPath, nil
}

func verifyRemoteDocumentSnapshot(uncPath string, file constandstruct.RemoteFile) (os.FileInfo, error) {
	info, err := os.Stat(uncPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать удалённый документ: %w", err)
	}
	if info.IsDir() || info.Size() != file.Size {
		return nil, fmt.Errorf("документ изменился после сканирования; просканируйте папку повторно")
	}
	if expectedTime, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(file.LastWriteTimeUTC)); parseErr == nil {
		difference := info.ModTime().UTC().Sub(expectedTime.UTC())
		if difference < 0 {
			difference = -difference
		}
		if difference > 2*time.Second {
			return nil, fmt.Errorf("время изменения документа не совпадает с результатом сканирования; повторите сканирование")
		}
	}
	return info, nil
}

func copyFileWithContext(ctx context.Context, sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		_ = destination.Close()
		if !completed {
			_ = os.Remove(destinationPath)
		}
	}()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written := 0
			for written < count {
				chunk, writeErr := destination.Write(buffer[written:count])
				if writeErr != nil {
					return writeErr
				}
				written += chunk
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	completed = true
	return nil
}
