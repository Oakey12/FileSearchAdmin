package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseComputersAcceptsAllForms(t *testing.T) {
	cases := map[string]string{
		"computers": "\xEF\xBB\xBF" + `{"computers": [{"name": "Бухгалтерия", "ip": "10.0.0.1"}, "10.0.0.2", {"ip": "pc-sklad"}]}`,
		"servers":   `{"servers": [{"name": "Бухгалтерия", "ip": "10.0.0.1"}, {"name": "10.0.0.2", "ip": "10.0.0.2"}, {"name": "pc-sklad", "ip": "pc-sklad"}]}`,
		"array":     `[{"name": "Бухгалтерия", "ip": "10.0.0.1"}, "10.0.0.2", " pc-sklad "]`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			computers, _, err := ParseComputers([]byte(data))
			if err != nil {
				t.Fatalf("ParseComputers() вернул ошибку: %v", err)
			}
			if len(computers) != 3 {
				t.Fatalf("компьютеров %d, ожидалось 3: %#v", len(computers), computers)
			}
			if computers[0].Name != "Бухгалтерия" || computers[0].IP != "10.0.0.1" {
				t.Fatalf("первая запись: %#v", computers[0])
			}
			// Без имени в таблице показывается адрес.
			if computers[1].Name != "10.0.0.2" || computers[2].Name != "pc-sklad" || computers[2].IP != "pc-sklad" {
				t.Fatalf("записи без имени: %#v", computers[1:])
			}
		})
	}
}

func TestParseComputersRejectsBadInput(t *testing.T) {
	cases := map[string]struct{ data, want string }{
		"broken json": {`{"computers": [`, "ошибка JSON"},
		"empty":       {`{"computers": []}`, "список компьютеров пуст"},
		"no ip":       {`{"computers": [{"name": "ПК"}]}`, "запись №1: не указан ip"},
		"bad ip":      {`["10.0.0.1", "10.0.0.300 "]`, "запись №2: некорректный"},
		"number":      {`[42]`, "запись №1: ожидается строка"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := ParseComputers([]byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ошибка %v, ожидалось содержащее %q", err, tc.want)
			}
		})
	}
}

func TestComputersExampleIsValid(t *testing.T) {
	data, err := os.ReadFile("../../docs/computers.example.json")
	if err != nil {
		t.Fatalf("образец не прочитан: %v", err)
	}
	computers, _, err := ParseComputers(data)
	if err != nil {
		t.Fatalf("образец не разбирается: %v", err)
	}
	if len(computers) < 2 {
		t.Fatalf("в образце слишком мало записей: %d", len(computers))
	}
}

func TestParseComputersSkipsDuplicateAddresses(t *testing.T) {
	data := `{"computers": [
		{"name": "", "ip": "192.168.140.72"},
		{"name": "", "ip": "192.168.140.84"},
		{"name": "Склад", "ip": "192.168.140.72"},
		{"name": "Касса", "ip": "192.168.140.72"},
		" 192.168.140.84 "
	]}`
	computers, duplicates, err := ParseComputers([]byte(data))
	if err != nil {
		t.Fatalf("повторы не должны отклонять файл: %v", err)
	}
	if duplicates != 3 {
		t.Fatalf("пропущено повторов %d, ожидалось 3", duplicates)
	}
	if len(computers) != 2 {
		t.Fatalf("компьютеров %d, ожидалось 2: %#v", len(computers), computers)
	}
	// Первое непустое имя достаётся адресу, записанному сначала без имени.
	if computers[0].IP != "192.168.140.72" || computers[0].Name != "Склад" {
		t.Fatalf("первый компьютер: %#v", computers[0])
	}
	if computers[1].IP != "192.168.140.84" || computers[1].Name != "192.168.140.84" {
		t.Fatalf("второй компьютер: %#v", computers[1])
	}
}