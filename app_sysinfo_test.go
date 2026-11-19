package main

import (
	"testing"

	constandstruct "filesearch-admin/internal/constAndStruct"
)

// Выгрузка идёт в порядке списка компьютеров, а не в порядке ответов.
func TestSysInfoBatchKeepsTargetOrder(t *testing.T) {
	batch := newSysInfoBatch([]constandstruct.Server{{IP: "10.0.0.3"}, {IP: "10.0.0.1"}, {IP: "10.0.0.2"}})
	batch.add(constandstruct.SystemInfoResult{IP: "10.0.0.2"})
	batch.add(constandstruct.SystemInfoResult{IP: "10.0.0.3"})
	batch.add(constandstruct.SystemInfoResult{IP: "10.0.0.1"})

	var store sysInfoStore
	if store.snapshot() != nil {
		t.Fatal("до сбора выгружать нечего")
	}
	store.set(batch)
	got := ""
	for _, r := range store.snapshot() {
		got += r.IP + " "
	}
	if got != "10.0.0.3 10.0.0.1 10.0.0.2 " {
		t.Fatalf("порядок: %q", got)
	}
}
