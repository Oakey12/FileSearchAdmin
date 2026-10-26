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
			computers, err := ParseComputers([]byte(data))
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
		"duplicate":   {`["10.0.0.1", {"ip": "10.0.0.1"}]`, "уже указан в записи №1"},
		"number":      {`[42]`, "запись №1: ожидается строка"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseComputers([]byte(tc.data))
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
	computers, err := ParseComputers(data)
	if err != nil {
		t.Fatalf("образец не разбирается: %v", err)
	}
	if len(computers) < 2 {
		t.Fatalf("в образце слишком мало записей: %d", len(computers))
	}
}
