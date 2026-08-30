package ai

import (
	"strings"
	"testing"
)

func TestParseRefusalAnalysisValid(t *testing.T) {
	raw := `{"type":"refusal","summary":"Орган відмовив у наданні інформації.","isLegal":"illegal","legalNotes":"Відмова без посилання на підстави ст. 18.","violations":[{"article":"Стаття 18","reason":"відмова не містить підстави з вичерпного переліку"},{"article":"Стаття 17","reason":"строк відповіді прострочено"}],"deadlineOk":"missed","nextStep":"complaint","recommendation":"Подайте скаргу на відмову.","draftSubject":"Скарга на відмову","draftBody":"Прошу розглянути скаргу..."}`
	a, err := ParseRefusalAnalysis(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Type != "refusal" || a.IsLegal != "illegal" || a.DeadlineOk != "missed" || a.NextStep != "complaint" {
		t.Fatalf("wrong core fields: %+v", a)
	}
	if len(a.Violations) != 2 || a.Violations[0].Article != "Стаття 18" {
		t.Fatalf("wrong violations: %+v", a.Violations)
	}
	if a.DraftSubject == "" || a.DraftBody == "" {
		t.Fatalf("draft must be filled")
	}
}

func TestParseRefusalAnalysisFencedJSON(t *testing.T) {
	raw := "```json\n{\"type\":\"substantive\",\"summary\":\"ok\",\"isLegal\":\"legal\",\"deadlineOk\":\"ok\",\"nextStep\":\"none\"}\n```"
	a, err := ParseRefusalAnalysis(raw)
	if err != nil {
		t.Fatalf("parse fenced: %v", err)
	}
	if a.Type != "substantive" || a.IsLegal != "legal" {
		t.Fatalf("wrong fields: %+v", a)
	}
}

func TestParseRefusalAnalysisNormalizesUnknownValues(t *testing.T) {
	raw := `{"type":"nonsense","summary":"","isLegal":"maybe","legalNotes":"","violations":null,"deadlineOk":"soon","nextStep":"sue-everyone","recommendation":"","draftSubject":"","draftBody":""}`
	a, err := ParseRefusalAnalysis(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Type != "unclear" {
		t.Errorf("type: want unclear, got %q", a.Type)
	}
	if a.IsLegal != "unknown" {
		t.Errorf("isLegal: want unknown, got %q", a.IsLegal)
	}
	if a.DeadlineOk != "unknown" {
		t.Errorf("deadlineOk: want unknown, got %q", a.DeadlineOk)
	}
	if a.NextStep != "none" {
		t.Errorf("nextStep: want none, got %q", a.NextStep)
	}
}

func TestParseRefusalAnalysisGarbage(t *testing.T) {
	if _, err := ParseRefusalAnalysis("це не JSON зовсім"); err == nil {
		t.Fatalf("expected error for garbage")
	}
}

func TestBuildRefusalAnalysisPrompt(t *testing.T) {
	p := BuildRefusalAnalysisPrompt("Міністерство охорони здоров'я", "Статистика захворювань", "У наданні інформації відмовлено.")
	for _, want := range []string{"Міністерство охорони здоров'я", "Статистика захворювань", "У наданні інформації відмовлено.", "ТЕКСТ ВІДПОВІДІ ОРГАНУ"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt must contain %q", want)
		}
	}
	// Без органа и темы — секций не должно быть видно как «Орган: »
	p2 := BuildRefusalAnalysisPrompt("", "", "лише текст")
	if strings.Contains(p2, "Орган:") || strings.Contains(p2, "Тема запиту:") {
		t.Errorf("empty organ/subject must not appear in prompt: %q", p2)
	}
}

func TestRefusalAnalysisSystemPromptAnchors(t *testing.T) {
	s := refusalAnalysisSystemPrompt()
	for _, want := range []string{"2939-VI", "ст. 18", "ст. 17", "\"refusal\"", "\"complaint\"", "JSON"} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt must contain %q", want)
		}
	}
}
