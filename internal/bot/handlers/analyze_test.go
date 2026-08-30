package handlers

import (
	"strings"
	"testing"
	"time"

	"info-bot-go/internal/ai"
)

func TestAnalysisTypeLabels(t *testing.T) {
	cases := map[string]string{
		"refusal":     "❌ Повна відмова",
		"partial":     "🟡 Часткова відмова / надано не все",
		"brushoff":    "🤷 Відписка (відповідь без суті)",
		"substantive": "✅ Відповідь по суті",
		"ack":         "📨 Авто-підтвердження отримання",
		"weird-value": "❔ Незрозуміло",
		"":            "❔ Незрозуміло",
	}
	for in, want := range cases {
		if got := analysisTypeLabel(in); got != want {
			t.Errorf("analysisTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegalityAndNextStepLabels(t *testing.T) {
	if legalityLabel("illegal") != "⚠️ НЕЗАКОННА" {
		t.Errorf("legalityLabel(illegal) = %q", legalityLabel("illegal"))
	}
	if legalityLabel("whatever") != "❔ Оцінити неможливо" {
		t.Errorf("legalityLabel(whatever) = %q", legalityLabel("whatever"))
	}
	if nextStepLabel("complaint") != "СКАРГА на розпорядника" {
		t.Errorf("nextStepLabel(complaint) = %q", nextStepLabel("complaint"))
	}
	if nextStepLabel("") != "—" {
		t.Errorf("nextStepLabel(\"\") = %q", nextStepLabel(""))
	}
	if deadlineLabel("missed") != "❗ прострочено" {
		t.Errorf("deadlineLabel(missed) = %q", deadlineLabel("missed"))
	}
}

func TestBuildVerdictCard(t *testing.T) {
	a := &ai.RefusalAnalysis{
		Type:           "refusal",
		Summary:        "Орган відмовив без посилання на підстави.",
		IsLegal:        "illegal",
		LegalNotes:     "Відмова не мотивована.",
		Violations:     []ai.Violation{{Article: "Стаття 18", Reason: "немає підстави відмови"}},
		DeadlineOk:     "missed",
		NextStep:       "complaint",
		Recommendation: "Подайте скаргу впродовж місяця.",
		DraftSubject:   "Скарга",
		DraftBody:      "Прошу...",
	}
	card := buildVerdictCard("Міністерство охорони здоров'я", "Статистика", a)
	for _, want := range []string{
		"РОЗБІР ВІДПОВІДІ ОРГАНУ",
		"❌ Повна відмова",
		"Міністерство охорони здоров'я",
		"Статистика",
		"НЕЗАКОННА",
		"Стаття 18",
		"СКАРГА на розпорядника",
		"Не є юридичною консультацією",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card must contain %q", want)
		}
	}
}

func TestBuildVerdictCardEscapesHTML(t *testing.T) {
	a := &ai.RefusalAnalysis{Type: "refusal", IsLegal: "illegal", NextStep: "none"}
	card := buildVerdictCard("Орган <A&B>", "", a)
	if strings.Contains(card, "<A&B>") {
		t.Errorf("card must escape HTML, got: %s", card)
	}
	if !strings.Contains(card, "&lt;A&amp;B&gt;") {
		t.Errorf("card must contain escaped organ")
	}
}

func TestBuildVerdictCardUnknownAnalysis(t *testing.T) {
	// Полностью пустой анализ не должен паниковать и должен показывать «непонятно».
	a := &ai.RefusalAnalysis{Type: "unclear", IsLegal: "unknown", DeadlineOk: "unknown", NextStep: "none"}
	card := buildVerdictCard("", "", a)
	if !strings.Contains(card, "❔ Незрозуміло") || !strings.Contains(card, "❔ Оцінити неможливо") {
		t.Errorf("unclear analysis must render fallback labels: %s", card)
	}
	if strings.Contains(card, "Наступний крок") {
		t.Errorf("nextStep=none must not render a step row")
	}
}

func TestAnalyzeLimiter(t *testing.T) {
	l := newAnalyzeLimiter(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		if !l.allow(42) {
			t.Fatalf("allow #%d must pass", i+1)
		}
	}
	if l.allow(42) {
		t.Fatalf("4th allow in window must be blocked")
	}
	if !l.allow(7) {
		t.Fatalf("another user must have own bucket")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.allow(42) {
		t.Fatalf("new window must reset the bucket")
	}
}

func TestSplitHTMLMessage(t *testing.T) {
	// Короткий текст — одним куском.
	if got := splitHTMLMessage("привіт", 100); len(got) != 1 || got[0] != "привіт" {
		t.Fatalf("short text: %+v", got)
	}

	// Длинный текст режется по абзацам, каждый кусок в лимите.
	para := strings.Repeat("абзац ", 200) // ~1200 символов
	text := para + "\n\n" + para + "\n\n" + para
	chunks := splitHTMLMessage(text, 1300)
	if len(chunks) < 2 {
		t.Fatalf("long text must be split, got %d chunks", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch) > 1300 {
			t.Errorf("chunk %d exceeds limit: %d", i, len(ch))
		}
	}

	// Один гигантский абзац без \n\n — жёсткая нарезка тоже в лимите.
	hard := strings.Repeat("x", 5000)
	chunks = splitHTMLMessage(hard, 1000)
	if len(chunks) < 5 {
		t.Fatalf("hard split expected >=5 chunks, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch) > 1000 {
			t.Errorf("hard chunk %d exceeds limit: %d", i, len(ch))
		}
	}
}
