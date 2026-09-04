package alert

import (
        "strings"
        "testing"
        "time"
)

// TestBuildUserAlert — текст алерта: кто, где, что; длинные ошибки режутся;
// пустые поля подставляются.
func TestBuildUserAlert(t *testing.T) {
        txt := BuildUserAlert(123, "Іван @ivan", "handler analyze", "gemini: 429 quota")
        for _, want := range []string{
                "Пользователь столкнулся с ошибкой",
                "Іван @ivan (id 123)",
                "handler analyze",
                "gemini: 429 quota",
                "bot.log",
        } {
                if !strings.Contains(txt, want) {
                        t.Errorf("текст алерта должен содержать %q:\n%s", want, txt)
                }
        }

        long := strings.Repeat("помилка ", 100)
        cut := BuildUserAlert(1, "", "handler x", long)
        if len([]rune(cut)) > 600 {
                t.Errorf("длинная ошибка должна обрезаться, длина: %d", len([]rune(cut)))
        }
        if !strings.Contains(cut, "без імені") {
                t.Errorf("пустое имя должно подставляться:\n%s", cut)
        }
        empty := BuildUserAlert(1, "Іван", "handler x", "")
        if !strings.Contains(empty, "(без тексту помилки)") {
                t.Errorf("пустой текст ошибки должен подставляться:\n%s", empty)
        }
}

// TestDedupKey — ключ устойчив: обрезается текст, место учитывается,
// пробелы по краям не мешают.
func TestDedupKey(t *testing.T) {
        a := DedupKey("handler x", "err one")
        b := DedupKey("handler x", "  err one  ")
        if a != b {
                t.Errorf("пробелы не должны менять ключ: %q vs %q", a, b)
        }
        if DedupKey("handler x", "err one") == DedupKey("handler y", "err one") {
                t.Error("разные места должны давать разные ключи")
        }
        long := strings.Repeat("x", 300)
        if len(DedupKey("h", long)) > 100 {
                t.Errorf("ключ должен обрезаться, длина: %d", len(DedupKey("h", long)))
        }
}

// TestAllowSend — чистая логика дедупа и часового лимита.
func TestAllowSend(t *testing.T) {
        now := time.Now()
        seen := map[string]time.Time{}
        if !AllowSend("k", now, seen, 30*time.Minute, nil, 10) {
                t.Error("первая ошибка должна проходить")
        }
        seen["k"] = now
        if AllowSend("k", now, seen, 30*time.Minute, nil, 10) {
                t.Error("повтор в окне дедупа должен блокироваться")
        }
        if !AllowSend("k", now.Add(31*time.Minute), seen, 30*time.Minute, nil, 10) {
                t.Error("после окна дедупа должна проходить")
        }
        sent := []time.Time{now.Add(-2 * time.Hour)}
        if !AllowSend("k2", now, map[string]time.Time{}, 30*time.Minute, sent, 10) {
                t.Error("старые алерты не должны съедать лимит")
        }
        recent := []time.Time{now.Add(-30 * time.Second), now.Add(-1 * time.Minute)}
        if AllowSend("k3", now, map[string]time.Time{}, 30*time.Minute, recent, 2) {
                t.Error("исчерпанный часовой лимит должен блокировать")
        }
}
