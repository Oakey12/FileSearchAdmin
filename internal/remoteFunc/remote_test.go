package remotefunc

import (
	"context"
	"encoding/base64"
	"errors"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInterpretPowerShellResult(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		runErr    error
		wantLines []string
		wantError string
	}{
		{name: "success", lines: []string{"FOUND|1|2|1cv7s.exe|DOMAIN\\user"}, wantLines: []string{"FOUND|1|2|1cv7s.exe|DOMAIN\\user"}},
		{name: "controlled error", lines: []string{"ERROR|Отказано в доступе"}, runErr: errors.New("exit status 1"), wantLines: []string{"ERROR|Отказано в доступе"}},
		{name: "unstructured PowerShell error", lines: []string{"ParserError: unexpected token"}, runErr: errors.New("exit status 1"), wantError: "ParserError"},
		{name: "empty process error", runErr: errors.New("executable not found"), wantError: "executable not found"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lines, err := interpretPowerShellResult(test.lines, test.runErr)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(lines, test.wantLines) {
					t.Fatalf("lines = %#v, want %#v", lines, test.wantLines)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestEncodePasswordForStdin(t *testing.T) {
	password := "сложный пароль !@# с пробелом"
	encoded := encodePasswordForStdin(password)
	if encoded == password || strings.Contains(encoded, password) {
		t.Fatal("encoded stdin must not contain the plain password")
	}
}

func TestPowerShellOutputCollectorStreamsOnlyProgressLines(t *testing.T) {
	var progress []string
	collector := &powerShellOutputCollector{progress: func(line string) {
		progress = append(progress, line)
	}}
	first := []byte("STAGE|Подк")
	second := []byte("лючение CIM/DCOM\r\nCOMMAND_LINE|секретный вывод\r\nTIMING|Подключение CIM|125\r\n")
	if _, err := collector.Write(first); err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Write(second); err != nil {
		t.Fatal(err)
	}
	wantOutput := append(append([]byte(nil), first...), second...)
	if got := collector.result(); !reflect.DeepEqual(got, wantOutput) {
		t.Fatalf("result() = %q, want %q", got, wantOutput)
	}
	wantProgress := []string{
		"STAGE|Подключение CIM/DCOM",
		"TIMING|Подключение CIM|125",
	}
	if !reflect.DeepEqual(progress, wantProgress) {
		t.Fatalf("progress = %#v, want %#v", progress, wantProgress)
	}
}

func TestDecodeRemoteCommandOutput(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("первая строка\r\nвторая|строка"))
	lines, err := decodeRemoteCommandOutput([]string{
		"TIMING|Выполнение PowerShell|125",
		"COMMAND_OUTPUT|" + encoded,
		"COMMAND_OK",
	})
	if err != nil {
		t.Fatalf("decodeRemoteCommandOutput() error = %v", err)
	}
	want := []string{
		"TIMING|Выполнение PowerShell|125",
		"COMMAND_LINE|первая строка",
		"COMMAND_LINE|вторая|строка",
		"COMMAND_OK",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("decodeRemoteCommandOutput() = %#v, want %#v", lines, want)
	}
}

func TestDecodeRemoteCommandOutputJoinsChunksBeforeUTF8Decoding(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("я"))
	lines, err := decodeRemoteCommandOutput([]string{
		"COMMAND_OUTPUT|" + encoded[:4],
		"COMMAND_OUTPUT|" + encoded[4:],
		"COMMAND_OK",
	})
	if err != nil {
		t.Fatalf("decodeRemoteCommandOutput() error = %v", err)
	}
	want := []string{"COMMAND_LINE|я", "COMMAND_OK"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("decodeRemoteCommandOutput() = %#v, want %#v", lines, want)
	}
}

func TestCommandRemoteTimeoutSeconds(t *testing.T) {
	if got := commandRemoteTimeoutSeconds(context.Background()); got != 110 {
		t.Fatalf("commandRemoteTimeoutSeconds(background) = %d, want 110", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got := commandRemoteTimeoutSeconds(ctx); got != 5 {
		t.Fatalf("commandRemoteTimeoutSeconds(short deadline) = %d, want 5", got)
	}
}

func TestRunRemoteScriptInUserSessionRejectsInvalidSelection(t *testing.T) {
	_, err := RunRemoteScriptInUserSession(
		context.Background(),
		"192.168.1.10",
		`DOMAIN\admin`,
		"password",
		"Get-Date",
		constandstruct.RemoteUser{DisplayName: `DOMAIN\user`, SID: "invalid", SessionID: 3},
	)
	if err == nil || !strings.Contains(err.Error(), "пользовательская сессия") {
		t.Fatalf("получена ошибка %v", err)
	}
}

func TestRemoteStageErrorPreservesDeadlineAndStage(t *testing.T) {
	err := &remoteStageError{
		cause: context.DeadlineExceeded,
		stage: "Подключение CIM/DCOM",
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("ошибка этапа должна сохранять context.DeadlineExceeded")
	}
	if stage := ErrorStage(err); stage != "Подключение CIM/DCOM" {
		t.Fatalf("ErrorStage() = %q", stage)
	}
}

func TestLastRemoteStage(t *testing.T) {
	lines := []string{
		"STAGE|Подключение CIM/DCOM",
		"TIMING|Подключение CIM|100",
		"STAGE|Поиск процессов",
	}
	if stage := lastRemoteStage(lines); stage != "Поиск процессов" {
		t.Fatalf("lastRemoteStage() = %q", stage)
	}
}

func TestRunRemoteScriptRejectsUnsafeOutputLimit(t *testing.T) {
	_, err := RunRemoteScriptWithOutputLimit(
		context.Background(), "192.0.2.1", "DOMAIN\\admin", "password", "Get-Date", 0,
	)
	if err == nil || !strings.Contains(err.Error(), "лимит результата") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	_, err = RunRemoteScriptWithOutputLimit(
		context.Background(), "192.0.2.1", "DOMAIN\\admin", "password", "Get-Date",
		MaxRemoteOutputCharactersAllowed+1,
	)
	if err == nil || !strings.Contains(err.Error(), "лимит результата") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestNewRemoteJobID(t *testing.T) {
	first, err := newRemoteJobID()
	if err != nil {
		t.Fatalf("newRemoteJobID: %v", err)
	}
	second, err := newRemoteJobID()
	if err != nil {
		t.Fatalf("newRemoteJobID: %v", err)
	}
	if len(first) != 32 || first == second {
		t.Fatalf("некорректные идентификаторы: %q, %q", first, second)
	}
	for _, character := range first {
		if !strings.ContainsRune("0123456789abcdef", character) {
			t.Fatalf("идентификатор содержит недопустимый символ %q", character)
		}
	}
}
