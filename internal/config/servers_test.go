package config

import (
	"strings"
	"testing"
)

func TestParseServers(t *testing.T) {
	servers, err := ParseServers([]byte(`{
		"servers": [
			{"name": "Сервер 1", "ip": "192.168.20.31"},
			{"name": "Сервер 2", "ip": "192.168.20.30"}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseServers() error = %v", err)
	}
	if len(servers) != 2 || servers[1].IP != "192.168.20.30" {
		t.Fatalf("ParseServers() = %#v", servers)
	}
}

func TestParseServersAcceptsDNSName(t *testing.T) {
	servers, err := ParseServers([]byte(`{"servers":[{"name":"Сервер","ip":"app-01.example.local"}]}`))
	if err != nil {
		t.Fatalf("ParseServers() error = %v", err)
	}
	if servers[0].IP != "app-01.example.local" {
		t.Fatalf("ParseServers() = %#v", servers)
	}
}

// Если имя не указано (известен только IP) — именем становится адрес.
func TestParseServersNameOptional(t *testing.T) {
	servers, err := ParseServers([]byte(`{"servers":[{"ip":"192.168.20.31"},{"name":"","ip":"192.168.20.32"}]}`))
	if err != nil {
		t.Fatalf("ParseServers() error = %v", err)
	}
	if len(servers) != 2 || servers[0].Name != "192.168.20.31" || servers[1].Name != "192.168.20.32" {
		t.Fatalf("ParseServers() = %#v", servers)
	}
}

// Файл с UTF-8 BOM (как сохраняет Блокнот/Out-File в Windows) должен читаться.
func TestParseServersAcceptsUTF8BOM(t *testing.T) {
	data := append([]byte("\xEF\xBB\xBF"), []byte(`{"servers":[{"name":"Сервер 1","ip":"192.168.20.31"}]}`)...)
	servers, err := ParseServers(data)
	if err != nil {
		t.Fatalf("ParseServers() с BOM error = %v", err)
	}
	if len(servers) != 1 || servers[0].IP != "192.168.20.31" {
		t.Fatalf("ParseServers() = %#v", servers)
	}
}

func TestParseServersRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "empty", json: `{"servers":[]}`, want: "пуст"},
		{name: "invalid address", json: `{"servers":[{"name":"Сервер","ip":"bad_name!"}]}`, want: "IP-адрес"},
		{name: "duplicate", json: `{"servers":[{"name":"A","ip":"192.168.1.1"},{"name":"B","ip":"192.168.1.1"}]}`, want: "повторно"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseServers([]byte(test.json))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseServers() error = %v, want fragment %q", err, test.want)
			}
		})
	}
}
