package auxiliaryfunc

import (
	constandstruct "filesearch-admin/internal/constAndStruct"
	"strconv"
	"strings"
)

func ParseFoundLines(lines []string) []constandstruct.RemoteWordProcess {
	var result []constandstruct.RemoteWordProcess
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) != 5 || parts[0] != "FOUND" {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		session, err := strconv.Atoi(parts[2])
		if err != nil {
			session = -1
		}
		result = append(result, constandstruct.RemoteWordProcess{PID: pid, Session: session, Name: parts[3], Owner: parts[4]})
	}
	return result
}

// функция для логирования
