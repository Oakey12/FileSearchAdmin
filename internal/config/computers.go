package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	constandstruct "filesearch-admin/internal/constAndStruct"
	"net"
	"strings"
)

// MaxComputers ограничивает список для проверки программы: файл на тысячи
// адресов почти наверняка собран по ошибке, а проверка шла бы часами.
const MaxComputers = 5000

type computersConfig struct {
	Computers []json.RawMessage `json:"computers"`
	Servers   []json.RawMessage `json:"servers"`
}

type computerEntry struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// ParseComputers разбирает JSON со списком компьютеров для проверки программы.
// Принимаются три формы, чтобы большой список было удобно собирать как угодно:
//
//	{"computers": [{"name": "Бухгалтерия", "ip": "192.168.1.10"}, "192.168.1.11"]}
//	{"servers":   [...]}   — тот же servers.json, что раздаёт Nginx
//	["192.168.1.10", "192.168.1.11"]
//
// Имя необязательно: без него в таблице показывается адрес. Повторяющиеся
// адреса пропускаются; их число — второе значение.
func ParseComputers(data []byte) ([]constandstruct.Server, int, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF")))

	var entries []json.RawMessage
	if bytes.HasPrefix(data, []byte("[")) {
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, 0, fmt.Errorf("ошибка JSON: %w", err)
		}
	} else {
		var config computersConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, 0, fmt.Errorf("ошибка JSON: %w", err)
		}
		entries = config.Computers
		if len(entries) == 0 {
			entries = config.Servers
		}
	}
	if len(entries) == 0 {
		return nil, 0, errors.New(`список компьютеров пуст: ожидается {"computers": [{"name": "...", "ip": "..."}]}`)
	}
	if len(entries) > MaxComputers {
		return nil, 0, fmt.Errorf("в списке %d адресов — больше допустимых %d", len(entries), MaxComputers)
	}

	seen := make(map[string]int, len(entries))
	duplicates := 0
	computers := make([]constandstruct.Server, 0, len(entries))
	for index, raw := range entries {
		var entry computerEntry
		var address string
		if err := json.Unmarshal(raw, &address); err == nil {
			entry.IP = address
		} else if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, 0, fmt.Errorf("запись №%d: ожидается строка с IP или объект {\"name\", \"ip\"}", index+1)
		}
		entry.Name = strings.TrimSpace(entry.Name)
		entry.IP = strings.TrimSpace(entry.IP)
		if entry.IP == "" {
			return nil, 0, fmt.Errorf("запись №%d: не указан ip", index+1)
		}
		if net.ParseIP(entry.IP) == nil && (!validHostname(entry.IP) || numericLabels(entry.IP)) {
			return nil, 0, fmt.Errorf("запись №%d: некорректный IP-адрес или DNS-имя %q", index+1, entry.IP)
		}
		// Повтор адреса — обычное дело, когда список склеен из нескольких
		// выгрузок; из-за него не стоит отклонять весь файл. Компьютер
		// проверяется один раз, имя берётся первое непустое.
		key := strings.ToLower(entry.IP)
		if first, exists := seen[key]; exists {
			duplicates++
			if entry.Name != "" && computers[first].Name == computers[first].IP {
				computers[first].Name = entry.Name
			}
			continue
		}
		seen[key] = len(computers)
		if entry.Name == "" {
			entry.Name = entry.IP
		}
		computers = append(computers, constandstruct.Server{Name: entry.Name, IP: entry.IP})
	}
	return computers, duplicates, nil
}

// numericLabels отличает опечатку в IP («192.168.1.300») от DNS-имени: имя из
// одних цифр с точками формально допустимо, но в списке ПК это всегда ошибка.
func numericLabels(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && character != '.' {
			return false
		}
	}
	return true
}
