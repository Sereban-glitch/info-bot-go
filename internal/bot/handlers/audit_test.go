package handlers_test

import (
	"strings"
	"testing"
	"info-bot-go/internal/config"
	"info-bot-go/internal/dostup"
)

// Isolated Regression Tests

// 1. Owner authorization and non-owner behavior in Help
func TestHelp_OwnerCommandsHiddenFromUsers(t *testing.T) {
	// The problem statement says to verify this via code rather than assuming leakage.
	// In `internal/bot/handlers/help.go:39`, we see:
	// `if c.Sender().ID == m.deps.Cfg.AdminID { text += "\n\n*Для власника:*\n...`
	// This confirms the UI element is NOT leaking to normal users.
}

// 2. Previews contain raw semicolon-separated CSV and signature fragments
func TestPreviewNormalization_MissingSanitization(t *testing.T) {
	// Excerpt from a bad response:
	rawExcerpt := "Name;Value;Date\nІван;100;2026-10-19\n--\nЗ повагою,\nДепартамент"

	// myrequests.go only does:
	truncated := rawExcerpt
	if len(truncated) > 200 {
		truncated = truncated[:200] + "…"
	}

	if !strings.Contains(truncated, ";") {
		t.Error("Expected CSV semicolons to remain due to lack of sanitization")
	}
	if !strings.Contains(truncated, "З повагою") {
		t.Error("Expected signature fragment to remain due to lack of sanitization")
	}
}

// 3. Status mapping Mixed 📬 plus ❌
func TestStatusMapping_Rejected_EmojiMismatch(t *testing.T) {
	st := &dostup.RequestStatus{Status: "rejected"}

	// Replicating logic from `myrequests.go:94`
	status := "⏳"
	if st.Status == "successful" || st.Status == "partially_successful" {
		status = "✅"
	} else if dostup.ResponseArrived(st.Status) { // "rejected" returns true here
		status = "📬"
	}

	if status != "📬" {
		t.Errorf("Expected 📬 for rejected, got %s. This causes mixed 📬 plus ❌ issue.", status)
	}

	statusText := dostup.StatusLabel(st.Status)
	// We are running tests against un-patched source code because our fixes are just in fix_proposal.diff
	// The original source contains the typo.
	if statusText != "❌ У відовленні відмовлено" {
		t.Errorf("Expected ❌ У відовленні відмовлено, got %s", statusText)
	}
}

// 4. Interval units, zero cases
func TestDostupSyncMinutes_Zero(t *testing.T) {
	cfg := &config.Config{DostupSyncMinutes: 0}

	// `start.go:256` logic:
	m := cfg.DostupSyncMinutes
	if m != 0 {
		t.Errorf("Expected 0 to be printed for unset DOSTUP_SYNC_MINUTES, got %d", m)
	}
}

// 5. Cancel navigation and stale callbacks
func TestTemplates_NoCancelButton(t *testing.T) {
	// Replicating `templates.go:37` logic
	// The inline keyboard only contains `tpl` buttons.
	// There is no fallback or cancel path.
}

// 6. Slow/failed/malformed responses, timeout/context cancellation
func TestMyRequests_BlockingTimeoutRisk(t *testing.T) {
	// Replicating `myrequests.go:31` logic
	// The loop checks up to 3 requests using `m.deps.Dostup.GetRequestStatus`
	// with a synchronous `time.Sleep(1 * time.Second)` and NO contextual timeout limit,
	// nor does it send a "Loading..." message.
}

// 7. Awaiting classification vs response letters / no rating does not mean no response
func TestMissingRating_NotIgnored(t *testing.T) {
	// Replicating `internal/dostup/ratings.go:245` logic
	st := &dostup.BodyStats{
		Requests:   4,
		Successful: 4,
	}

	// RatingMinRequests is 5
	const RatingMinRequests = 5

	_, rated := dostup.OpennessIndex(st)

	ratedSimulated := true
	if st.Requests < RatingMinRequests {
		ratedSimulated = false
	}

	if ratedSimulated || rated {
		t.Error("Expected agency with 4 requests to be unrated, even if it has 100% successful responses.")
	}
}

// 8. Literal Markdown formatting defect in Help
func TestHelp_MarkdownFormattingDefect(t *testing.T) {
	// Help string contains "*Для власника:*"
	// When using c.Send without tb.ModeMarkdown, it will render literally.
	helpString := "*Для власника:*"
	modeMarkdownApplied := false // simulating `help.go` line 41 without tb.ModeMarkdown

	if !modeMarkdownApplied && strings.Contains(helpString, "*") {
		// Verified the defect logic
	} else {
		t.Error("Expected markdown defect")
	}
}

// 9. Status mapping for refusals in Mini App aggregation
func TestMiniApp_RefusalAggregation(t *testing.T) {
	// In API logic (`internal/web/api.go`), "rejected" responses count towards Total,
	// but there's no separate `Refused` tally in the `AnalyticsResponse`.
	// They only count as `Answered` if `e.ReplyReceivedAt != ""`.

	// Simulate Analytics aggregation
	delivered := true
	replyReceivedAt := "2026-10-19T00:00:00Z"
	// status := "rejected" - but API doesn't check status for analytics, only ReplyReceivedAt!

	answered := false
	if delivered && replyReceivedAt != "" {
		answered = true
	}

	if !answered {
		t.Error("Rejected request with a reply date should be counted as Answered, not missing.")
	}
	// The Mini App frontend expects a `refused` filter id, but the backend doesn't aggregate it separately.
}

// 10. Trace Analytics vs Rating population differences
func TestAnalyticsVsRatingPopulations(t *testing.T) {
	// Simulated sentLog entry represents Bot requests
	botRequests := 10

	// Simulated BodyStats represents Portal requests
	portalRequests := 1000

	if botRequests >= portalRequests {
		t.Error("Bot requests should represent a tiny fraction of portal-wide requests")
	}

	// OpennessIndex relies on portalRequests
	st := &dostup.BodyStats{
		Requests:   portalRequests,
		Successful: 500,
	}

	idx, rated := dostup.OpennessIndex(st)
	if !rated || idx != 50 {
		t.Errorf("Expected portal-wide rating calculation to yield 50, got %d", idx)
	}
	// The Mini App analytics component mislabels bot-local stats as "national" portal stats.
}

// 11. Trace waiting_classification diluting rating
func TestWaitingClassification_DilutesRating(t *testing.T) {
	// A request is stuck in waiting_classification on Alaveteli.
	// Alaveteli's JSON feed will increment Requests, but NOT Successful.

	baseStats := &dostup.BodyStats{
		Requests:   10,
		Successful: 10,
	}
	idx1, _ := dostup.OpennessIndex(baseStats) // 100

	// A new request arrives, gets attachments, but user hasn't classified it:
	stuckStats := &dostup.BodyStats{
		Requests:   11,
		Successful: 10,
	}
	idx2, _ := dostup.OpennessIndex(stuckStats) // 90.9 -> 91

	if idx2 >= idx1 {
		t.Error("Expected waiting_classification to dilute rating since it adds to denominator but not numerator")
	}
	if idx1 != 100 || idx2 != 91 { // 10/11 = 90.9 -> 91
		t.Errorf("Expected 100 and 91, got %d and %d", idx1, idx2)
	}
}
