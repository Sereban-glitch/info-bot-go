# SmartZapyt Audit Report

## 1. Live Observations Confirmed via Code Evidence
**A. Historical "кожні 0 хв"**
- **Evidence**: `internal/bot/handlers/start.go:256` uses `m.deps.Cfg.DostupSyncMinutes` directly in string formatting for `/stats`. If the environment variable `DOSTUP_SYNC_MINUTES` is unset, `config.go` sets this to `0`.
- **Reality Check**: `dostupsync.go` correctly defends against `interval = 0` by falling back to 20 minutes (`if m := w.deps.Cfg.DostupSyncMinutes; m > 0`). This zero-minute display is code-reproducible under a specific config (where `DOSTUP_SYNC_MINUTES` is not defined), not a verified current production config.

**B. Markdown formatting defect in Help**
- **Evidence**: `internal/bot/handlers/help.go:41` appends `*Для власника:*` using Markdown syntax (asterisks). However, `c.Send(text, kb)` doesn't specify `tb.ModeMarkdown`. Therefore, it literally renders `*Для власника:*` instead of bolding it. (The command leak was refuted: the `if c.Sender().ID == m.deps.Cfg.AdminID` protects it, so only the owner sees this section).

**C. No Back/Cancel in Templates message**
- **Evidence**: `internal/bot/handlers/templates.go:37`. The template selection keyboard is fully populated with `tpl` inline buttons representing templates, but lacks any general navigation action (e.g. `pskip` equivalent or `Cancel`/`Back`), leaving the user stuck in that UI block without manual typed commands.

**D. My Requests ~20s delay without progress feedback**
- **Evidence**: `internal/bot/handlers/myrequests.go:31`. The handler checks up to 3 live statuses synchronously via `m.deps.Dostup.GetRequestStatus(slug)` and intentionally sleeps `time.Sleep(1 * time.Second)` between them. With network latency, this easily accumulates a significant block. Crucially, there's no "Loading..." interstitial message sent *before* this blocking operation.

**E. Previews contain raw semicolon-separated CSV and signature fragments**
- **Evidence**: `internal/bot/handlers/myrequests.go:94`. The preview relies purely on `st.ResponseExcerpt`. `dostup/client.go` fetches the raw content and `myrequests.go` just truncates it to 200 chars. Since CSV tabular data (like budgets) and standard footers ("З повагою, ...") are not stripped, they appear raw to the user.

**F. Status typo "У відовленні відмовлено" and mixed 📬 plus ❌**
- **Evidence 1 (Typo)**: `internal/dostup/client.go:632` maps the `rejected` status to `"❌ У відовленні відмовлено"`. It should be `задоволенні`.
- **Evidence 2 (Mixed Emojis)**: `internal/bot/handlers/myrequests.go:94`. The local emoji assignment sets `status = "📬"` if `dostup.ResponseArrived(st.Status)` is true. `rejected` evaluates to true, thus 📬 is assigned. But the appended descriptive text uses the `❌` from `StatusLabel("rejected")`.

**G. Anonymity wording analysis**
- **Evidence**: `internal/bot/handlers/start.go` advertises "БЕЗПЕКА ТА АНОНІМНІСТЬ" and asserts "ваше прізвище не обов'язкове", but `profile.go` says "Закон не вимагає підтверджувати особу — псевдонім не заборонений... Але справжнє ім'я надійніше". The wordings technically reflect the system's data flow (which permits missing last names), but whether this leads to legal rejections by agencies depends on external factors.

**H. Missing Rating does NOT imply Ignored Request**
- **Evidence**: `internal/dostup/ratings.go:245`. The `OpennessIndex` calculates the index using `int(math.Round(100 * float64(st.Successful) / float64(st.Requests)))` but importantly, if `st.Requests < RatingMinRequests` (which is 5), it returns `0, false`. This means an agency could answer all 4 of its requests successfully, but still have no rating. Missing response or stale/failed sync does not prove ignoring.

**I. Status aggregation mismatch (Mini App vs Telegram)**
- **Evidence**: Telegram treats `rejected` as `📬` (ResponseArrived == true). The Mini App aggregations in `internal/web/api.go` (`handleAnalytics`) and `internal/dostup/ratings.go` (`Leaderboard`) only aggregate `e.ReplyReceivedAt != ""` (which maps to answered) or `st.Successful` for ratings. There isn't an explicit "refused" status passed down in the top-level aggregates to the Mini App; the frontend (`internal/web/assets/index.js`) expects a `refused` filter in `/requests/`, but `internal/web/api.go` AnalyticsResponse only has `Answered`, `Awaiting`, and `AckOnly`. The Mini App requests list endpoint `/api/requests` aggregates statuses.

**J. Metric Source and Population Distinctions (Bot vs Portal Users)**
- **API Analytics (`/api/analytics`)**: Only looks at `s.sentLog.ListAll()`. The `sentLog` contains *local bot-originated requests* plus requests from `MyRequestsFull()` (the shared portal account). This population represents the bot users and shared bot portal account ONLY. It does **not** represent all users of the public `dostup.org.ua` portal. Labeling these stats as representing "Загальнонаціональні дані Доступу" in the Mini App is inaccurate.
- **API Rating (`/api/rating`)**: The rating index logic (`OpennessIndex`) operates on `BodyStats` parsed dynamically from the public portal pages for *each agency*. This population *does* reflect all third-party public portal users across the country, independent of the bot.
- **Numerators and Denominators**:
  - *Analytics*: Numerator (Answered) increments if `e.ReplyReceivedAt != ""`; Denominator (Total) is total requests in `sentLog` where `Delivered == true`.
  - *Rating*: Numerator is `st.Successful` (public portal requests marked as Successful or Partially Successful by any requester); Denominator is `st.Requests` (Total public portal requests sent to that agency).

---

## 2. Refuted Hypotheses
**A. Help shows a long flat command list leaking to users**
- **Refutation**: This is **not a leak**. `internal/bot/handlers/help.go:39` protects this section with `if c.Sender().ID == m.deps.Cfg.AdminID`. The user who observed this was the Owner (AdminID). Regular users do not see this. The only defect is the literal Markdown rendering (see 1B).

---

## 3. Discovered Code Defects
**A. `safego` Data Race in test logging**
- **Evidence**: `internal/safego/safego_test.go:58` overrides a global `logf` function pointer to capture panics for tests. Meanwhile, `TestGoRecoversPanic` spawns an asynchronous panic that writes to `logf`. Running `go test -race` proves that `TestPanicLogged` overrides `logf` concurrently while the delayed background panic is resolving, causing a panic/race in the tests.

---

## 4. Fix Plan (Prioritized)
1. **Fix Data Race (`safego`)**: Protect the global `logf` override with a mutex during test execution, or use isolated loggers per operation.
2. **Fix Status Typo and Mixed Status Mappings (`dostup/client.go`, `myrequests.go`)**:
   - Correct the typo to "У задоволенні відмовлено".
   - Modify `myrequests.go` to enforce `❌` mapping if `st.Status == "rejected"`, overriding the generic `📬`.
3. **Fix Interval Zero Cases (`start.go`)**: Ensure `handleAdminStats` defaults to `20` minutes for presentation if `cfg.DostupSyncMinutes <= 0`.
4. **Fix Preview Normalization (`myrequests.go`)**: Create a basic sanitization utility to strip `;\n` CSV artifacts and standard signature closures before displaying `ResponseExcerpt`.
5. **Fix My Requests Blocking (`myrequests.go`)**: Dispatch an initial "Завантаження статусів..." message, run the synchronous wait loop, and finally `c.Edit()` the message with the final results.
6. **Fix Cancel Navigation in Templates (`templates.go`)**: Inject an inline "« Cancel" / "« Back" button row for standard navigation flow.
7. **Fix literal Markdown rendering in Help (`help.go`)**: Pass `tb.ModeMarkdown` when sending the Help text.

*(Note: No production fixes are applied to codebase; isolated tests successfully verify the logic boundaries as requested. Branding fixes are deferred to user decision).*

## 5. Execution Environment & Test Metrics
- **Go Version:** `go version go1.24.3 linux/amd64`
- **Checkout SHA Verification:** Verified 47acbbb56f4641446a1f5670d29bfdc725df3f1c matches `HEAD`.
- **Exact Commands Run:**
  - `go version`
  - `git rev-parse HEAD`
  - `go vet ./...` (Exit code: 0)
  - `go test ./...` (Exit code: 1, failed on `safego`)
  - `go test -race ./...` (Exit code: 1, race condition in `safego`)
  - `go test -v ./internal/bot/handlers/audit_test.go` (Exit code: 0)
- **Test Counts & Failures:**
  - Base suite: 1 test failed (`TestPanicLogged` in `safego`) out of ~12 test suites.
  - Regression isolated suite: 10 tests added, 10 tests passed.
- **Limitations:** Isolated regression tests cannot easily trigger full bot handler contexts without mocking the `tb.Context` and `tb.Bot` objects which would pull in external behavior. The tests verify the observable logic branches (like string parsing, interval logic, status mapping, missing rating, and analytics grouping logic) locally instead of firing full HTTP/Telegram mock cycles.
