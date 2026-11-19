package ops

import (
	"fmt"
	"strconv"
	"strings"

	constandstruct "filesearch-admin/internal/constAndStruct"
	psscript "filesearch-admin/internal/psScript"
	"filesearch-admin/internal/xlsx"
)

// SystemInfoSheet раскладывает сведения о компьютерах в таблицу для Excel:
// строка на компьютер, каждое поле — свой столбец (по ним работает фильтр и
// сортировка). Числа — числовыми ячейками. Компьютеры без ответа остаются в
// таблице со статусом «Ошибка» и причиной.
func SystemInfoSheet(results []constandstruct.SystemInfoResult) xlsx.Sheet {
	header := []string{
		"Сервер", "IP", "Статус",
		"Имя компьютера", "Домен / рабочая группа", "В домене", "Активный пользователь",
		"Windows", "Разрядность", "Версия", "Сборка", "Установлена", "Последняя загрузка", "Время работы",
		"Процессор", "Ядер", "Потоков", "Частота, МГц",
		"ОЗУ всего, ГБ", "ОЗУ свободно, ГБ",
		"Производитель", "Модель", "Серийный номер",
		"IP-адреса", "Диски", `Папки C:\Users`,
		fmt.Sprintf("Подключения по RDP (%d дн.)", psscript.RDPHistoryDays),
		"Ошибка",
	}
	rows := make([][]xlsx.Cell, 0, len(results))
	for _, r := range results {
		name := r.Name
		if name == r.IP {
			name = ""
		}
		if r.Error != "" {
			row := make([]xlsx.Cell, len(header))
			row[0], row[1], row[2] = xlsx.Text(name), xlsx.Text(r.IP), xlsx.Text("Ошибка")
			row[len(header)-1] = xlsx.Text(r.Error)
			rows = append(rows, row)
			continue
		}
		info := r.Info
		inDomain := "нет (рабочая группа)"
		if info.PartOfDomain {
			inDomain = "да"
		}
		if info.Domain == "" {
			inDomain = ""
		}
		rows = append(rows, []xlsx.Cell{
			xlsx.Text(name), xlsx.Text(r.IP), xlsx.Text("OK"),
			xlsx.Text(info.ComputerName), xlsx.Text(info.Domain), xlsx.Text(inDomain), xlsx.Text(info.LoggedOnUser),
			xlsx.Text(info.OSCaption), xlsx.Text(info.OSArchitecture), xlsx.Text(info.OSVersion), xlsx.Text(info.OSBuild),
			xlsx.Text(info.OSInstallDate), xlsx.Text(info.LastBootTime), xlsx.Text(info.Uptime),
			xlsx.Text(info.ProcessorName), intCell(info.ProcessorCores), intCell(info.ProcessorLogical), intCell(info.ProcessorClockMHz),
			numberCell(info.MemoryTotalGB), numberCell(info.MemoryFreeGB),
			xlsx.Text(info.Manufacturer), xlsx.Text(info.Model), xlsx.Text(info.SerialNumber),
			xlsx.Text(strings.Join(info.IPAddresses, "\n")),
			xlsx.Text(strings.Join(info.Disks, "\n")),
			xlsx.Text(strings.Join(info.UserFolders, "\n")),
			xlsx.Text(strings.Join(info.RDPConnections, "\n")),
			xlsx.Text(""),
		})
	}
	return xlsx.Sheet{Name: "Сведения о системе", Header: header, Rows: rows}
}

func intCell(v int) xlsx.Cell {
	if v <= 0 {
		return xlsx.Text("")
	}
	return xlsx.Number(float64(v))
}

// numberCell — число из строки удалённого сценария («15.9» или «15,9»); если
// не разбирается, остаётся текстом, чтобы значение не потерялось.
func numberCell(s string) xlsx.Cell {
	s = strings.TrimSpace(s)
	if s == "" {
		return xlsx.Text("")
	}
	if v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(s, ",", "."), "\u00a0", ""), 64); err == nil {
		return xlsx.Number(v)
	}
	return xlsx.Text(s)
}
