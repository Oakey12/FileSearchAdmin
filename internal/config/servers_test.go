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
