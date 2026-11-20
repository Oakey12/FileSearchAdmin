package ops

import (
	"context"
	"strings"
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
	remotefunc "filesearch-admin/internal/remoteFunc"
)

func diagJournal(t *testing.T, user string, lines ...string) (string, string) {
	t.Helper()
	sink, journal := collectJournal()
	problem := logConnectionResult(constandstruct.Server{Name: "TERM2", IP: "192.168.20.31"}, user, lines, sink)
	return problem, journal()
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("в журнале нет %q:\n%s", want, text)
		}
	}
}

// Реальная ошибка с TERM2/OBMEN1C/Term: «Отказано в доступе».
func TestConnectionAccessDenied(t *testing.T) {
	problem, text := diagJournal(t, `CORP\admin`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|fail|700|Отказано в доступе. (0x80070005)",
		"DIAG|dcom_privacy|fail|650|Отказано в доступе. (0x80070005)",
	)
	if problem != "отказано в доступе" {
		t.Fatalf("problem = %q", problem)
	}
	mustContain(t, text, "НЕ ПОДКЛЮЧАЕТСЯ — отказано в доступе", "net localgroup Администраторы", `«CORP\admin»`)
	if strings.Contains(text, "UAC") {
		t.Fatal("совет про UAC только для локальной учётной записи")
	}
}

func TestConnectionAccessDeniedLocalAccount(t *testing.T) {
	_, text := diagJournal(t, `.\admin`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|fail|700|Access is denied.",
		"DIAG|dcom_privacy|fail|650|Access is denied.",
	)
	mustContain(t, text, "UAC", "доменную учётную запись")
}

// Реальная ошибка с TERMSRV: «Неверная попытка доступа к адресу памяти».
func TestConnectionInvalidMemoryAccess(t *testing.T) {
	problem, text := diagJournal(t, `CORP\admin`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|fail|600|Неверная попытка доступа к адресу памяти. (0x800703E6)",
		"DIAG|dcom_privacy|fail|600|Неверная попытка доступа к адресу памяти. (0x800703E6)",
	)
	if problem != "сбой DCOM/WMI на сервере" {
		t.Fatalf("problem = %q", problem)
	}
	mustContain(t, text, "DistributedCOM", "winmgmt")
}

func TestConnectionNeedsPrivacy(t *testing.T) {
	problem, text := diagJournal(t, `CORP\admin`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|fail|700|Отказано в доступе. (0x80070005)",
		"DIAG|dcom_privacy|ok|300|",
		"DIAG|wmi|ok|20|TERM2, Windows Server",
	)
	if problem != "нужно шифрование DCOM" {
		t.Fatalf("problem = %q", problem)
	}
	mustContain(t, text, "с шифрованием DCOM — есть")
}

func TestConnectionPortClosed(t *testing.T) {
	problem, text := diagJournal(t, `CORP\admin`, "DIAG|port|fail|3000|нет ответа на порт 135 за 3 с")
	if problem != "нет связи" {
		t.Fatalf("problem = %q", problem)
	}
	mustContain(t, text, "НЕТ СВЯЗИ")
}

func TestConnectionAllGood(t *testing.T) {
	problem, text := diagJournal(t, `CORP\admin`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|ok|80|",
		"DIAG|wmi|ok|20|TERM2, Microsoft Windows Server 2019",
		"DIAG|processes|ok|50|1cv7s.exe ×4",
		"DIAG|launch|ok|40|",
	)
	if problem != "" {
		t.Fatalf("problem = %q", problem)
	}
	mustContain(t, text, "запущена 1С — 1cv7s.exe ×4", "запуск команд на сервере — работает", "готов к работе")
}

func TestConnectionLaunchDenied(t *testing.T) {
	problem, _ := diagJournal(t, `CORP\user`,
		"DIAG|port|ok|5|",
		"DIAG|dcom|ok|80|",
		"DIAG|wmi|ok|20|TERM2",
		"DIAG|processes|ok|50|",
		"DIAG|launch|fail|40|доступ запрещён (код 2)",
	)
	if problem != "запуск команд запрещён" {
		t.Fatalf("problem = %q", problem)
	}
}

// Операция целиком: итог перечисляет проблемные серверы в порядке списка.
func TestTestConnectionSummary(t *testing.T) {
	original := runRemoteDiagnose
	runRemoteDiagnose = func(ctx context.Context, ip, user, password string, progress remotefunc.ProgressFunc) ([]string, error) {
		if ip == "10.0.0.2" {
			return []string{"DIAG|port|ok|1|", "DIAG|dcom|fail|1|Отказано в доступе. (0x80070005)", "DIAG|dcom_privacy|fail|1|Отказано в доступе. (0x80070005)"}, nil
		}
		return []string{"DIAG|port|ok|1|", "DIAG|dcom|ok|1|", "DIAG|wmi|ok|1|X", "DIAG|processes|ok|1|", "DIAG|launch|ok|1|"}, nil
	}
	t.Cleanup(func() { runRemoteDiagnose = original })

	sink, journal := collectJournal()
	targets := []constandstruct.Server{{Name: "A", IP: "10.0.0.1"}, {Name: "B", IP: "10.0.0.2"}}
	if !TestConnection(context.Background(), targets, `CORP\admin`, "Секрет-42", sink) {
		t.Fatal("тест не запустился")
	}
	WaitForOperations(context.Background())
	mustContain(t, journal(), "готовы к работе 1 из 2; с проблемами: B — отказано в доступе")
	if strings.Contains(journal(), "Секрет-42") {
		t.Fatal("пароль попал в журнал")
	}
}
