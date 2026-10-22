package ops

import (
	"context"
	"testing"
	"time"
)

func TestCancelCurrentOperationCancelsContext(t *testing.T) {
	operationCtx, started := beginOperation(context.Background(), func(string) {})
	if !started {
		t.Fatal("тестовая операция не запущена")
	}
	defer func() {
		if IsOperationRunning() {
			finishOperation()
		}
	}()

	if !CancelCurrentOperation(nil) {
		t.Fatal("отмена выполняющейся операции не принята")
	}
	select {
	case <-operationCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("контекст операции не отменён")
	}
	finishOperation()
	if IsOperationRunning() {
		t.Fatal("операция осталась в состоянии выполнения")
	}
}

func TestBeginOperationRejectsConcurrentOperation(t *testing.T) {
	_, started := beginOperation(context.Background(), func(string) {})
	if !started {
		t.Fatal("первая операция не запущена")
	}
	defer finishOperation()

	_, secondStarted := beginOperation(context.Background(), func(string) {})
	if secondStarted {
		t.Fatal("конкурирующая операция не должна запускаться")
	}
}
