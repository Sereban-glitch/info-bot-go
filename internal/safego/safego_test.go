package safego

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ТЗ №4, D3: паника внутри задачи ловится и НЕ убивает процесс —
// последующие вызовы работают дальше.
func TestRunRecoversPanic(t *testing.T) {
	called := false
	// паникующая итерация не должна обрушить тест
	Run("test-panic", func() {
		panic("boom")
	})
	// дошли сюда — процесс жив; следующая задача выполняется
	Run("test-ok", func() {
		called = true
	})
	if !called {
		t.Fatal("после паники задачи должны продолжать выполняться")
	}
}

// Run без паники просто выполняет функцию.
func TestRunNormal(t *testing.T) {
	got := 0
	Run("normal", func() { got = 42 })
	if got != 42 {
		t.Fatalf("got=%d, ожидаем 42", got)
	}
}

// Go: горутина с паникой не валит процесс; вторая горутина отрабатывает.
func TestGoRecoversPanic(t *testing.T) {
	done := make(chan struct{})
	Go("goroutine-ok", func() {
		Go("goroutine-panic", func() {
			panic("goroutine boom")
		})
		// даём паникующей горутине шанс упасть (если бы recover не работал)
		time.Sleep(50 * time.Millisecond)
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("горутина не завершилась")
	}
	// процесс жив — тест дошёл до этой строки
}

// Проверка, что стек паники пишется в журнал (не молча).
func TestPanicLogged(t *testing.T) {
	var buf strings.Builder
	logf = func(format string, v ...interface{}) {
		fmt.Fprintf(&buf, format, v...)
	}
	defer func() { logf = defaultLogf }()

	Run("logged-panic", func() {
		panic("must-be-logged")
	})
	out := buf.String()
	if !strings.Contains(out, "[PANIC]") || !strings.Contains(out, "must-be-logged") {
		t.Fatalf("паника должна писаться в журнал с пометкой [PANIC], получили: %q", out)
	}
	if !strings.Contains(out, "goroutine") {
		t.Fatal("в журнале должен быть стек вызовов")
	}
}
