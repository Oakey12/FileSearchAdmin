//go:build windows

package remotefunc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	resourceTypeDisk  = 1
	connectTemporary  = 0x00000004
	errorNotConnected = 2250
	errorOpenFiles    = 2401
)

var (
	mprDLL                 = syscall.NewLazyDLL("mpr.dll")
	wNetAddConnection2W    = mprDLL.NewProc("WNetAddConnection2W")
	wNetCancelConnection2W = mprDLL.NewProc("WNetCancelConnection2W")
)

type windowsNetResource struct {
	scope        uint32
	resourceType uint32
	displayType  uint32
	usage        uint32
	localName    *uint16
	remoteName   *uint16
	comment      *uint16
	provider     *uint16
}

// RemoteShare представляет временное SMB-подключение к административному
// ресурсу диска целевого компьютера. Подключение не сохраняется в профиле
// Windows и закрывается только если его создало приложение.
type RemoteShare struct {
	root   string
	owned  bool
	mu     sync.Mutex
	closed bool
}

// ConnectRemoteFileShare подключает административный ресурс диска, на котором
// расположен remotePath, и возвращает эквивалентный UNC-путь.
func ConnectRemoteFileShare(
	ctx context.Context,
	target, username, password, remotePath string,
) (*RemoteShare, string, error) {
	target = strings.TrimSpace(target)
	username = strings.TrimSpace(username)
	remotePath = filepath.Clean(strings.TrimSpace(remotePath))
	if net.ParseIP(target) == nil {
		return nil, "", fmt.Errorf("укажите корректный IP-адрес целевого компьютера")
	}
	if username == "" || password == "" {
		return nil, "", fmt.Errorf("введите логин и пароль администратора целевого компьютера")
	}
	root, uncPath, err := remotePathToAdminUNC(target, remotePath)
	if err != nil {
		return nil, "", err
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, 4*time.Second)
	defer cancelDial()
	connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(target, "445"))
	if err != nil {
		return nil, "", fmt.Errorf("SMB-порт 445 на %s недоступен: %w", target, err)
	}
	_ = connection.Close()

	resolvedUser, err := resolveRemoteUsername(ctx, target, username)
	if err != nil {
		return nil, "", err
	}
	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return nil, "", fmt.Errorf("некорректный сетевой путь: %w", err)
	}
	userPtr, err := syscall.UTF16PtrFromString(resolvedUser)
	if err != nil {
		return nil, "", fmt.Errorf("некорректное имя пользователя: %w", err)
	}
	passwordPtr, err := syscall.UTF16PtrFromString(password)
	if err != nil {
		return nil, "", fmt.Errorf("пароль содержит недопустимый символ: %w", err)
	}
	resource := windowsNetResource{resourceType: resourceTypeDisk, remoteName: rootPtr}
	result, _, _ := wNetAddConnection2W.Call(
		uintptr(unsafe.Pointer(&resource)),
		uintptr(unsafe.Pointer(passwordPtr)),
		uintptr(unsafe.Pointer(userPtr)),
		connectTemporary,
	)
	runtime.KeepAlive(resource)
	runtime.KeepAlive(passwordPtr)
	runtime.KeepAlive(userPtr)

	share := &RemoteShare{root: root, owned: result == 0}
	if result != 0 {
		// Windows возвращает ошибку, если к этому серверу уже есть SMB-сеанс.
		// Используем его только когда административный ресурс действительно
		// доступен; чужое недоступное подключение не отключаем.
		if _, statErr := os.Stat(root); statErr != nil {
			return nil, "", remoteShareError(uint32(result), root)
		}
	}
	if _, err := os.Stat(uncPath); err != nil {
		_ = share.Close()
		return nil, "", fmt.Errorf("удалённый файл недоступен по пути %s: %w", remotePath, err)
	}
	return share, uncPath, nil
}

func remotePathToAdminUNC(target, remotePath string) (string, string, error) {
	remotePath = filepath.Clean(strings.TrimSpace(remotePath))
	volume := filepath.VolumeName(remotePath)
	if len(volume) != 2 || volume[1] != ':' || !filepath.IsAbs(remotePath) {
		return "", "", fmt.Errorf("для внешнего редактора требуется путь на локальном диске целевого компьютера, например C:\\Users\\user\\file.xlsx")
	}
	drive := strings.ToUpper(volume[:1])
	relative := strings.TrimLeft(strings.TrimPrefix(remotePath, volume), `\/`)
	if relative == "" {
		return "", "", fmt.Errorf("выбран путь диска, а не файла")
	}
	root := `\\` + target + `\` + drive + `$`
	uncPath := filepath.Join(root, relative)
	if relativeToRoot, err := filepath.Rel(root, uncPath); err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, `..\`) {
		return "", "", fmt.Errorf("путь файла выходит за пределы административного ресурса")
	}
	return root, uncPath, nil
}

func remoteShareError(code uint32, root string) error {
	switch code {
	case 5:
		return fmt.Errorf("доступ к %s запрещён: учётная запись не имеет прав администратора", root)
	case 53, 67:
		return fmt.Errorf("административный ресурс %s не найден или заблокирован firewall", root)
	case 86, 1326:
		return fmt.Errorf("Windows отклонил логин или пароль для %s", root)
	case 1219:
		return fmt.Errorf("Windows уже подключён к %s с другими учётными данными; закройте существующее SMB-подключение", root)
	default:
		return fmt.Errorf("не удалось подключить %s: ошибка Windows %d", root, code)
	}
}

func (share *RemoteShare) Close() error {
	if share == nil {
		return nil
	}
	share.mu.Lock()
	defer share.mu.Unlock()
	if share.closed || !share.owned {
		share.closed = true
		return nil
	}
	rootPtr, err := syscall.UTF16PtrFromString(share.root)
	if err != nil {
		return err
	}
	result, _, _ := wNetCancelConnection2W.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		0,
		0,
	)
	runtime.KeepAlive(rootPtr)
	if result == errorOpenFiles {
		return fmt.Errorf("документ всё ещё открыт во внешней программе; сохраните и закройте его перед завершением")
	}
	if result != 0 && result != errorNotConnected {
		return fmt.Errorf("не удалось закрыть SMB-подключение: ошибка Windows %d", result)
	}
	share.closed = true
	return nil
}
