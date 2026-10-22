package auxiliaryfunc

import "testing"

func TestParseFoundLines(t *testing.T) {
	lines := []string{
		"FOUND|123|7|1cv7s.exe|DOMAIN\\user",
		"ERROR|ignored",
		"FOUND|bad|8|1cv7s.exe|DOMAIN\\other",
	}

	processes := ParseFoundLines(lines)
	if len(processes) != 1 {
		t.Fatalf("len(processes) = %d, want 1", len(processes))
	}
	process := processes[0]
	if process.PID != 123 || process.Session != 7 || process.Name != "1cv7s.exe" || process.Owner != `DOMAIN\user` {
		t.Fatalf("unexpected process: %#v", process)
	}
}
