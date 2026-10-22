package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testServersJSON = `{"servers":[{"name":"Сервер 1","ip":"192.168.20.31"}]}`

// isolateCache уводит кеш списка серверов во временную папку, чтобы тест не
// трогал рабочий каталог пользователя.
func isolateCache(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("LocalAppData", directory)
	t.Setenv("XDG_CACHE_HOME", directory)
	t.Setenv(ServersFileEnvName, "")
	return directory
}

func TestLoadServersTakesListFromUpdateServer(t *testing.T) {
	cacheDirectory := isolateCache(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(testServersJSON))
	}))
	defer server.Close()
	t.Setenv(ServersURLEnvName, server.URL)

	servers, source, err := LoadServers()
	if err != nil {
		t.Fatalf("LoadServers() вернул ошибку: %v", err)
	}
	if len(servers) != 1 || servers[0].IP != "192.168.20.31" {
		t.Fatalf("получен список %#v, ожидался один сервер 192.168.20.31", servers)
	}
	if source != server.URL {
		t.Fatalf("источник = %q, ожидался %q", source, server.URL)
	}

	cacheFile := filepath.Join(cacheDirectory, "FileSearchAdmin", serversCacheFileName)
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("список с сервера должен сохраняться в кеш %s: %v", cacheFile, err)
	}
}

func TestLoadServersUsesCacheWhenServerUnavailable(t *testing.T) {
	isolateCache(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(testServersJSON))
	}))
	address := server.URL
	t.Setenv(ServersURLEnvName, address)
	if _, _, err := LoadServers(); err != nil {
		t.Fatalf("подготовка кеша не удалась: %v", err)
	}
	server.Close()

	servers, source, err := LoadServers()
	if err != nil {
		t.Fatalf("при недоступном сервере список должен браться из кеша, получено: %v", err)
	}
	if len(servers) != 1 || servers[0].IP != "192.168.20.31" {
		t.Fatalf("из кеша получен список %#v", servers)
	}
	if !strings.Contains(source, "сохранённая копия") {
		t.Fatalf("источник = %q, ожидалось упоминание сохранённой копии", source)
	}
}

func TestLoadServersPrefersExplicitFile(t *testing.T) {
	isolateCache(t)
	path := filepath.Join(t.TempDir(), ServersFileName)
	if err := os.WriteFile(path, []byte(testServersJSON), 0o600); err != nil {
		t.Fatalf("не удалось подготовить файл: %v", err)
	}
	t.Setenv(ServersFileEnvName, path)
	t.Setenv(ServersURLEnvName, "http://127.0.0.1:1/недоступно")

	servers, source, err := LoadServers()
	if err != nil {
		t.Fatalf("LoadServers() вернул ошибку: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("получен список %#v", servers)
	}
	if source != path {
		t.Fatalf("источник = %q, ожидался %q", source, path)
	}
}
