package ops

import (
	"encoding/base64"
	"encoding/json"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"strings"
	"testing"
)

func TestParseRemoteUsers(t *testing.T) {
	ivan := encodedRemoteUserLine(t, constandstruct.RemoteUser{
		DisplayName: `DOMAIN\ivan`, Username: "ivan", Domain: "DOMAIN",
		SID: `S-1-5-21-1-2-3-1001`, ProfilePath: `C:\Users\ivan`, SessionID: 2,
	})
	anna := encodedRemoteUserLine(t, constandstruct.RemoteUser{
		DisplayName: `DOMAIN\anna`, Username: "anna", Domain: "DOMAIN",
		SID: `S-1-5-21-1-2-3-1002`, ProfilePath: `C:\Users\anna`, SessionID: 3,
	})
	users, err := parseRemoteUsers([]string{ivan, anna, ivan, "USERS_OK"})
	if err != nil {
		t.Fatalf("parseRemoteUsers() error = %v", err)
	}
	if len(users) != 2 || users[0].Username != "anna" || users[1].Username != "ivan" {
		t.Fatalf("parseRemoteUsers() = %#v", users)
	}
}

func TestParseRemoteUsersRequiresConfirmation(t *testing.T) {
	if _, err := parseRemoteUsers(nil); err == nil || !strings.Contains(err.Error(), "подтверждения") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestParseRemoteUsersAcceptsLegacyCommandTransport(t *testing.T) {
	line := encodedRemoteUserLine(t, constandstruct.RemoteUser{
		DisplayName: `DOMAIN\ivan`, Username: "ivan", Domain: "DOMAIN",
		SID: `S-1-5-21-1-2-3-1001`, ProfilePath: `C:\Users\ivan`, SessionID: 2,
	})
	users, err := parseRemoteUsers([]string{"COMMAND_LINE|" + line, "COMMAND_OK"})
	if err != nil || len(users) != 1 || users[0].Username != "ivan" {
		t.Fatalf("parseRemoteUsers() = %#v, %v", users, err)
	}
}

func TestLogRemoteUserWarnings(t *testing.T) {
	var messages []string
	logRemoteUserWarnings(
		constandstruct.Server{Name: "Сервер 2", IP: "192.168.20.30"},
		[]string{"WARN_REMOTE_USER|123|4|GetOwnerSid timeout"},
		func(message string) { messages = append(messages, message) },
	)
	if len(messages) != 1 || !strings.Contains(messages[0], "PID=123") || !strings.Contains(messages[0], "сессия=4") {
		t.Fatalf("messages = %#v", messages)
	}
}

func encodedRemoteUserLine(t *testing.T, user constandstruct.RemoteUser) string {
	t.Helper()
	data, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	return "REMOTE_USER|" + base64.StdEncoding.EncodeToString(data)
}
