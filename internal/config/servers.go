package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ServersFileName    = "servers.json"
	ServersFileEnvName = "FSADMIN_SERVERS_FILE"
	ServersURLEnvName  = "FSADMIN_SERVERS_URL"

	// DefaultServersURL держит список там же, где лежат обновления. У
	// пользователя остаётся только EXE: файл рядом с приложением не нужен.
	DefaultServersURL = "http://192.168.150.100:1212/filesearch-admin/servers.json"

	// Первый запуск без сохранённой копии ждёт сеть, поэтому лимит короткий:
	// в локальной сети ответ приходит мгновенно, а недоступный сервер не должен
	// задерживать появление окна.
	serversRequestTimeout = 5 * time.Second
	maxServersSize        = 1 << 20
	serversCacheFileName  = "servers-cache.json"
)

type serversConfig struct {
	Servers []constandstruct.Server `json:"servers"`
}

// LoadServers забирает список серверов с сервера обновлений. Локальный файл
// рядом с приложением не нужен: он остался только как запасной вариант для
// старых установок. Переменная FSADMIN_SERVERS_FILE по-прежнему имеет
// наивысший приоритет и позволяет работать с собственным файлом.
//
// Второе значение описывает источник и попадает в журнал.
func LoadServers() ([]constandstruct.Server, string, error) {
	if configured := strings.TrimSpace(os.Getenv(ServersFileEnvName)); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return nil, "", fmt.Errorf("не удалось определить путь из %s: %w", ServersFileEnvName, err)
		}
		servers, err := loadServersFile(absolute)
		if err != nil {
			return nil, absolute, err
		}
		return servers, absolute, nil
	}

	address := strings.TrimSpace(os.Getenv(ServersURLEnvName))
	if address == "" {
		address = DefaultServersURL
	}

	// Сохранённая копия отдаётся сразу, без обращения к сети: запуск не должен
	// ждать сервер, который может быть недоступен — именно в такие моменты
	// приложением и пользуются. Свежий список подтягивается в фоне и достаётся
	// следующему запуску.
	if cached, cachePath, cacheErr := readServersCache(); cacheErr == nil {
		go func() {
			if servers, err := fetchServers(address); err == nil {
				writeServersCache(servers)
			}
		}()
		return cached, cachePath + " (сохранённая копия; обновляется в фоне)", nil
	}

	servers, remoteErr := fetchServers(address)
	if remoteErr == nil {
		writeServersCache(servers)
		return servers, address, nil
	}

	if legacy, path, legacyErr := loadLegacyServersFile(); legacyErr == nil {
		return legacy, fmt.Sprintf("%s (локальный файл; %s недоступен: %v)", path, address, remoteErr), nil
	}

	return nil, "", fmt.Errorf(
		"не удалось получить список серверов с %s: %w; сохранённой копии и локального %s тоже нет",
		address, remoteErr, ServersFileName,
	)
}

func fetchServers(address string) ([]constandstruct.Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), serversRequestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("некорректный адрес %s: %w", address, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxServersSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxServersSize {
		return nil, fmt.Errorf("ответ превышает допустимый размер %d байт", maxServersSize)
	}

	servers, err := ParseServers(data)
	if err != nil {
		return nil, fmt.Errorf("некорректный ответ %s: %w", address, err)
	}
	return servers, nil
}

func loadServersFile(path string) ([]constandstruct.Server, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	servers, err := ParseServers(data)
	if err != nil {
		return nil, fmt.Errorf("некорректный файл %s: %w", path, err)
	}
	return servers, nil
}

// loadLegacyServersFile ищет servers.json рядом с приложением и в рабочей
// папке — порядок, который действовал до переноса списка на сервер.
func loadLegacyServersFile() ([]constandstruct.Server, string, error) {
	candidates := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), ServersFileName))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDirectory, ServersFileName))
	}

	for _, path := range candidates {
		servers, err := loadServersFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, path, err
		}
		return servers, path, nil
	}
	return nil, "", fmt.Errorf("%s не найден", ServersFileName)
}

func serversCachePath() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "FileSearchAdmin", serversCacheFileName), nil
}

func writeServersCache(servers []constandstruct.Server) {
	path, err := serversCachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(serversConfig{Servers: servers})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func readServersCache() ([]constandstruct.Server, string, error) {
	path, err := serversCachePath()
	if err != nil {
		return nil, "", err
	}
	servers, err := loadServersFile(path)
	if err != nil {
		return nil, path, err
	}
	return servers, path, nil
}

func ParseServers(data []byte) ([]constandstruct.Server, error) {
	var config serversConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("ошибка JSON: %w", err)
	}
	if len(config.Servers) == 0 {
		return nil, errors.New("список servers пуст")
	}

	seenAddresses := make(map[string]struct{}, len(config.Servers))
	servers := make([]constandstruct.Server, 0, len(config.Servers))
	for index, server := range config.Servers {
		server.Name = strings.TrimSpace(server.Name)
		server.IP = strings.TrimSpace(server.IP)
		if server.Name == "" {
			return nil, fmt.Errorf("servers[%d]: не указано имя", index)
		}
		if net.ParseIP(server.IP) == nil && !validHostname(server.IP) {
			return nil, fmt.Errorf("servers[%d]: некорректный IP-адрес или DNS-имя %q", index, server.IP)
		}
		key := strings.ToLower(server.IP)
		if _, exists := seenAddresses[key]; exists {
			return nil, fmt.Errorf("servers[%d]: адрес %q указан повторно", index, server.IP)
		}
		seenAddresses[key] = struct{}{}
		servers = append(servers, server)
	}
	return servers, nil
}

func validHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	value = strings.TrimSuffix(value, ".")
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
