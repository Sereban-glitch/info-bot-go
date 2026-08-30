package ai

// ТЗ №6 «Розбір відмови»: AI-анализ ответов государственных органов.
//
// Пользователь пересылает боту ответ органа (текст, фото письма, голос или
// ответ прямо из гилки портала), AI определяет тип ответа (отказ / отписка /
// ответ по существу), оценивает законность по ЗУ № 2939-VI
// «Про доступ до публічної інформації», указывает нарушенные статьи и
// готовит ГОТОВЫЙ документ для следующего шага — уточнение, жалоба или
// обращение.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Violation — нарушенная (по оценке AI) статья закона.
type Violation struct {
	Article string `json:"article"`
	Reason  string `json:"reason"`
}

// RefusalAnalysis — структурированный вердикт AI по ответу органа.
type RefusalAnalysis struct {
	Type           string      `json:"type"`           // refusal|partial|brushoff|substantive|ack|unclear
	Summary        string      `json:"summary"`        // суть ответа в 1-2 предложениях
	IsLegal        string      `json:"isLegal"`        // legal|illegal|partially|unknown
	LegalNotes     string      `json:"legalNotes"`     // краткое обоснование оценки законности
	Violations     []Violation `json:"violations"`     // нарушенные статьи с пояснением
	DeadlineOk     string      `json:"deadlineOk"`     // ok|missed|unknown
	NextStep       string      `json:"nextStep"`       // clarification|complaint|appeal|none
	Recommendation string      `json:"recommendation"` // конкретный совет, что делать
	DraftSubject   string      `json:"draftSubject"`   // тема готового документа
	DraftBody      string      `json:"draftBody"`      // текст готового документа
}

// Допустимые значения полей: всё постороннее нормализуем в unknown/none,
// чтобы пользователь никогда не видел «сырого» мусора от модели.
var (
	analysisTypes     = map[string]bool{"refusal": true, "partial": true, "brushoff": true, "substantive": true, "ack": true, "unclear": true}
	analysisLegality  = map[string]bool{"legal": true, "illegal": true, "partially": true, "unknown": true}
	analysisDeadlines = map[string]bool{"ok": true, "missed": true, "unknown": true}
	analysisSteps     = map[string]bool{"clarification": true, "complaint": true, "appeal": true, "none": true}
)

// analysisMaxReplyLen — максимум символов текста ответа органа в запросе.
const analysisMaxReplyLen = 8000

// refusalAnalysisSystemPrompt — системная инструкция AI-юриста (полный
// разбор: вердикт + готовый документ одним ответом).
func refusalAnalysisSystemPrompt() string {
	return refusalSystemPrompt(true)
}

// refusalVerdictSystemPrompt — инструкция БЫСТРОГО этапа разбора:
// только вердикт, без документа. Ответ короче — модель отвечает за
// секунды, пользователь видит оценку почти сразу, а документ готовится
// отдельно (и может показываться в живом стриминге).
func refusalVerdictSystemPrompt() string {
	return refusalSystemPrompt(false)
}

// refusalSystemPrompt — общая инструкция; withDocument управляет секцией
// готового документа и финальной схемой JSON.
func refusalSystemPrompt(withDocument bool) string {
	docSection := ""
	jsonSchema := `{"type":"...","summary":"...","isLegal":"...","legalNotes":"...","violations":[{"article":"Стаття 18","reason":"..."}],"deadlineOk":"...","nextStep":"...","recommendation":"..."}`
	if withDocument {
		docSection = `
6. ГОТОВИЙ ДОКУМЕНТ (draftSubject + draftBody) — повний текст документа для наступного кроку:
   - clarification → офіційне уточнення/повторний запит: на що саме відповідь неповна, що саме просимо надати;
   - complaint → скарга: факти, порушені статті, вимоги (надати інформацію, розглянути, дати письмову відповідь);
   - appeal → звернення до вищого органу/уповноваженого;
   - none → обидва поля порожні рядки "".
   Документ пиши від першої особи («прошу», «звертаю увагу»), офіційним стилем, українською мовою, БЕЗ підпису і дати (бот додасть їх сам), БЕЗ email-адрес і без місць для заповнення на кшталт [ПІБ].
`
		jsonSchema = `{"type":"...","summary":"...","isLegal":"...","legalNotes":"...","violations":[{"article":"Стаття 18","reason":"..."}],"deadlineOk":"...","nextStep":"...","recommendation":"...","draftSubject":"...","draftBody":"..."}`
	} else {
		docSection = `
6. Документ на цьому етапі НЕ потрібен: наступним окремим кроком документ підготує інший запит. Просто постав вердикт.
`
	}
	return `Ти — досвідчений український юрист із законодавства про доступ до публічної інформації. Твоє завдання — розібрати відповідь державного органу (розпорядника інформації) на запит громадянина та оцінити її відповідність до Закону України «Про доступ до публічної інформації» № 2939-VI.

ЩО ВИЗНАЧИТИ:

1. Тип відповіді (type):
   - "refusal" — повна відмова надати інформацію;
   - "partial" — часткова відмова: надано не все, що питалося;
   - "brushoff" — відписка: формальна відповідь без суті (не за темою, загальні фрази, «переадресація» без підстав);
   - "substantive" — змістовна відповідь по суті запиту;
   - "ack" — лише автоматичне підтвердження отримання («ваш лист отримано»);
   - "unclear" — з наданого тексту неможливо зрозуміти.

2. Законність (isLegal): "legal" | "illegal" | "partially" | "unknown".

3. Порушені статті (violations) — лише ті, у яких ВПЕВНЕНИЙ. Ключові норми ЗУ № 2939-VI:
   - ст. 13: запит подається без пояснення мотивів, форма довільна; не можна вимагати відомостей, не передбачених законом;
   - ст. 17: відповідь — протягом 5 робочих днів; до 20 робочих днів у разі складності (з повідомленням запитувача); негайно — якщо інформація потрібна для захисту життя чи свободи;
   - ст. 18: ВИЧЕРПНИЙ перелік підстав відмови; відмова має бути мотивованою, з посиланням на конкретну підставу і з роз'ясненням порядку оскарження; відмова з інших підстав не допускається; перенаправлення до належного розпорядника — не порушення;
   - ст. 22: відповідальність за порушення законодавства про доступ до публічної інформації.
   Пам'ятай також про порядок оскарження рішень розпорядників (керівник, Уповноважений Верховної Ради України з прав людини, суд).

4. Строки (deadlineOk): "ok" | "missed" | "unknown" — оціни за датами, якщо вони є в тексті (5 робочих днів).

5. Наступний крок (nextStep):
   - "clarification" — уточнити запит / повторно запросити;
   - "complaint" — скарга на дії/бездіяльність розпорядника;
   - "appeal" — оскарження до вищого органу, Уповноваженого ВР України з прав людини або суду;
   - "none" — нічого не потрібно (відповідь по суті).
` + docSection + `
ПРАВИЛА:
- Жодних вигаданих фактів: спирайся лише на наданий текст відповіді та контекст запиту.
- Посилайся лише на ті статті, у яких впевнений.
- Якщо тексту відповіді немає, він обірваний, а фото (якщо надано) не містить читабельного тексту листа — тип "unclear", isLegal "unknown", nextStep "none".
- summary і recommendation пиши простою мовою, зрозумілою неюристу.

ОБОВ'ЯЗКОВО поверни ЛИШЕ валідний JSON (без markdown-огорож, без пояснень навколо):
` + jsonSchema
}

// BuildRefusalAnalysisPrompt — пользовательская часть запроса к модели.
func BuildRefusalAnalysisPrompt(organ, subject, replyText string) string {
	var b strings.Builder
	b.WriteString("РОЗБІР ВІДПОВІДІ ОРГАНУ\n\n")
	if strings.TrimSpace(organ) != "" {
		b.WriteString("Орган: " + strings.TrimSpace(organ) + "\n")
	}
	if strings.TrimSpace(subject) != "" {
		b.WriteString("Тема запиту: " + strings.TrimSpace(subject) + "\n")
	}
	b.WriteString("\nТЕКСТ ВІДПОВІДІ ОРГАНУ:\n")
	b.WriteString(replyText)
	b.WriteString("\n\nПроаналізуй цю відповідь та поверни JSON згідно з інструкцією.")
	return b.String()
}

// AnalyzeRefusal выполняет AI-розбір ответа органа в два этапа: быстрый
// вердикт + готовый документ. Фото письма — опционально (мультимодальный
// запрос; фото учитывается только на этапе вердикта).
// Используется мини-приложением (POST /api/analyze); бот-хендлер работает
// с этапами по отдельности (AnalyzeRefusalVerdict + AnalyzeRefusalDocument),
// чтобы показать вердикт сразу и стримить документ.
func (r *Rotator) AnalyzeRefusal(organ, subject, replyText string, photoBase64 []byte) (*RefusalAnalysis, error) {
	a, err := r.AnalyzeRefusalVerdict(organ, subject, replyText, photoBase64)
	if err != nil {
		return nil, err
	}
	if a.NextStep == "none" || a.NextStep == "" {
		return a, nil
	}
	subj, body, derr := r.AnalyzeRefusalDocument(a, organ, subject, replyText, nil)
	if derr != nil {
		// Документ не удался — вердикт ценен сам по себе; не валится весь разбор.
		return a, nil
	}
	a.DraftSubject, a.DraftBody = subj, body
	return a, nil
}

// AnalyzeRefusalVerdict — БЫСТРЫЙ этап: вердикт без документа
// (тип, законность, нарушения, строки, следующий шаг, рекомендация).
func (r *Rotator) AnalyzeRefusalVerdict(organ, subject, replyText string, photoBase64 []byte) (*RefusalAnalysis, error) {
	if len(replyText) > analysisMaxReplyLen {
		replyText = replyText[:analysisMaxReplyLen]
	}

	parts := []interface{}{
		map[string]string{"text": BuildRefusalAnalysisPrompt(organ, subject, replyText)},
	}
	media := false
	if len(photoBase64) > 0 {
		media = true
		parts = append(parts, map[string]interface{}{
			"inlineData": map[string]string{
				"mimeType": "image/jpeg",
				"data":     base64.StdEncoding.EncodeToString(photoBase64),
			},
		})
	}

	contents := []interface{}{
		map[string]interface{}{
			"role":  "user",
			"parts": parts,
		},
	}

	text, err := r.tryProxyThenDirect(refusalVerdictSystemPrompt(), contents, "application/json", media)
	if err != nil {
		return nil, err
	}
	return ParseRefusalAnalysis(text)
}

// refusalDocumentSystemPrompt — инструкция этапа документа: модель знает
// вердикт и пишет только готовый документ. Формат — простой текст
// (первый ряд — тема), чтобы документ можно было показывать в стриминге
// без JSON-шума.
func refusalDocumentSystemPrompt(a *RefusalAnalysis) string {
	violations := "не визначені"
	if len(a.Violations) > 0 {
		var sb strings.Builder
		for i, v := range a.Violations {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(v.Article)
			if v.Reason != "" {
				sb.WriteString(" — " + v.Reason)
			}
		}
		violations = sb.String()
	}
	return `Ти — досвідчений український юрист із законодавства про доступ до публічної інформації. Вердикт щодо відповіді органу вже поставлено (нижче). Твоє завдання — скласти ГОТОВИЙ ДОКУМЕНТ для наступного кроку громадянина.

ВЕРДИКТ (вже поставлено, не переглядай його):
- тип відповіді органу: ` + a.Type + `
- оцінка законності: ` + a.IsLegal + `
- порушені статті: ` + violations + `
- наступний крок: ` + a.NextStep + `

ФОРМАТ ВІДПОВІДІ — простий текст, без JSON і markdown-огорож:
перший рядок — тема документа;
порожній рядок;
далі — повний текст документа.

ВИМОГИ ДО ДОКУМЕНТА:
- ` + documentGoal(a.NextStep) + `
- Пиши від першої особи («прошу», «звертаю увагу»), офіційним стилем, українською мовою.
- БЕЗ підпису і дати (бот додасть їх сам), БЕЗ email-адрес, без місць для заповнення на кшталт [ПІБ].
- Жодних вигаданих фактів: спирайся лише на текст відповіді органу та контекст запиту.
- Згадай порушені статті та вимоги, які випливають із вердикту.`
}

// documentGoal — что именно должно получиться на следующем шаге.
func documentGoal(nextStep string) string {
	switch nextStep {
	case "clarification":
		return "Офіційне уточнення/повторний запит: на що саме відповідь неповна або не за темою, що саме просимо надати, з посиланням на строки відповіді за ст. 17 ЗУ № 2939-VI."
	case "complaint":
		return "Скарга на дії/бездіяльність розпорядника: факти (запит, відповідь, строки), порушені статті, вимоги (надати інформацію, розглянути скаргу, дати письмову мотивовану відповідь)."
	case "appeal":
		return "Звернення до вищого органу/Уповноваженого Верховної Ради України з прав людини: суть порушення, що просимо зробити, посилання на закон."
	default:
		return "Коротке повторне звернення із проханням надати інформацію по суті запиту."
	}
}

// AnalyzeRefusalDocument — МЕДЛЕННЫЙ этап: готовый документ для следующего
// шага. onChunk (если задан) получает дельты текста по мере генерации —
// так бот показывает документ «печатается прямо на глазах» (пилот
// стриминга). Возвращает тему и текст документа.
func (r *Rotator) AnalyzeRefusalDocument(a *RefusalAnalysis, organ, subject, replyText string, onChunk func(string)) (string, string, error) {
	if a == nil || a.NextStep == "none" || a.NextStep == "" {
		return "", "", nil
	}
	if len(replyText) > analysisMaxReplyLen {
		replyText = replyText[:analysisMaxReplyLen]
	}

	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []interface{}{
				map[string]string{"text": BuildRefusalAnalysisPrompt(organ, subject, replyText)},
			},
		},
	}

	text, err := r.tryProxyThenDirectStream(refusalDocumentSystemPrompt(a), contents, "", false, onChunk)
	if err != nil {
		return "", "", err
	}
	return ParseDocumentDraft(text)
}

// ParseDocumentDraft разбирает ответ этапа документа: первый ряд — тема,
// остальное — текст. Чистая функция, покрыта тестами.
func ParseDocumentDraft(raw string) (subject, body string, err error) {
	raw = strings.TrimSpace(raw)
	// Модель могла обернуть текст в markdown-заборы — снимаем.
	if strings.HasPrefix(raw, "```") {
		if idx := strings.Index(raw, "\n"); idx >= 0 {
			raw = raw[idx+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}
	if raw == "" {
		return "", "", nil
	}
	if idx := strings.Index(raw, "\n"); idx >= 0 {
		subject = strings.TrimSpace(raw[:idx])
		body = strings.TrimSpace(raw[idx+1:])
	} else {
		subject = raw
	}
	if subject == "" {
		subject = "Документ"
	}
	return subject, body, nil
}

// ParseRefusalAnalysis парсит и нормализует ответ модели (чистая функция для тестов).
func ParseRefusalAnalysis(raw string) (*RefusalAnalysis, error) {
	var a RefusalAnalysis
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &a); err != nil {
		return nil, fmt.Errorf("ai: не вдалося розібрати відповідь моделі: %w", err)
	}
	if !analysisTypes[a.Type] {
		a.Type = "unclear"
	}
	if !analysisLegality[a.IsLegal] {
		a.IsLegal = "unknown"
	}
	if !analysisDeadlines[a.DeadlineOk] {
		a.DeadlineOk = "unknown"
	}
	if !analysisSteps[a.NextStep] {
		a.NextStep = "none"
	}
	return &a, nil
}
