package main

import "testing"

// Отклонённый старт (операция занята) не должен слать событие статуса — иначе
// он погасит индикатор и кнопку «Отменить» у уже идущей операции.
func TestRunBlockingRejectedStartEmitsNoStatus(t *testing.T) {
	var events []bool
	a := &App{onStatus: func(running bool) { events = append(events, running) }}

	value, err := runBlocking(a, func(done func(int, error)) bool {
		// Транспорт не запускается: операция занята.
		_ = done
		return false
	})

	if err != errOperationNotStarted {
		t.Fatalf("err = %v, want errOperationNotStarted", err)
	}
	if value != 0 {
		t.Fatalf("value = %d, want 0", value)
	}
	if len(events) != 0 {
		t.Fatalf("при отклонении статус меняться не должен, получено: %v", events)
	}
}

// Успешный старт: статус «идёт», затем «свободно», и возвращается результат.
func TestRunBlockingStartedEmitsBusyThenIdle(t *testing.T) {
	var events []bool
	a := &App{onStatus: func(running bool) { events = append(events, running) }}

	value, err := runBlocking(a, func(done func(int, error)) bool {
		go done(42, nil)
		return true
	})

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if value != 42 {
		t.Fatalf("value = %d, want 42", value)
	}
	if len(events) != 2 || events[0] != true || events[1] != false {
		t.Fatalf("ожидали [true,false], получили %v", events)
	}
}
