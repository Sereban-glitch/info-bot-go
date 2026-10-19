# SmartZapyt Audit Report
## Live Observations
- ❌ **"Для власника:" command list in help**: Code review `internal/bot/handlers/help.go:39` shows this is properly protected by `c.Sender().ID == m.deps.Cfg.AdminID`. NOT a bug, owner observed this.
- ❌ **Historical «кожні 0 хв»**: `getEnvInt("DOSTUP_SYNC_MINUTES", 0)` means if `DostupSyncMinutes` is not set, it defaults to 0. We should check if the sync routine handles 0 or negative numbers properly.

Let's check sync interval zero handling.
- ❌ **"Для власника:" command list**: Protected by `c.Sender().ID == m.deps.Cfg.AdminID`. NOT a bug.
- ❌ **Historical «кожні 0 хв»**: `getEnvInt("DOSTUP_SYNC_MINUTES", 0)` will parse as 0 if the env var isn't set, then `interval = time.Duration(0) * time.Minute` in `Start()`. Then `jitter := time.Duration(rand.Int63n(int64(interval / 4)))` occurs. `int64(0/4)` -> `0`. `rand.Int63n(0)` causes a panic. Wait, `if m := w.deps.Cfg.DostupSyncMinutes; m > 0` condition in `dostupsync.go` protects against `interval` being 0, falling back to 20m.
However, in `/stats` dashboard (`handleAdminStats`), the text literally renders `%d` using `m.deps.Cfg.DostupSyncMinutes`, so if it's unset (0), it literally renders «кожні 0 хв», even though the sync interval is actually 20m!
Yes! `dostupsync.go:68` uses 20m if `DostupSyncMinutes <= 0`, but `start.go:256` prints `m.deps.Cfg.DostupSyncMinutes` directly.
- 🎯 **Historical «кожні 0 хв»**: Verified. If `DOSTUP_SYNC_MINUTES` is unset in `.env`, `getEnvInt` returns 0. The sync worker correctly falls back to 20m (`dostupsync.go:68`), but the dashboard `handleAdminStats` (`start.go:256`) naively prints `gs.DostupSyncMinutes`, resulting in `0`.

Now let's check other UX observations:
- mixed SmartZapyt title, ІНФОЗАПИТИ avatar and Прозоро introduction
- 🎯 **Mixed Branding**: Verified. Code still contains "Смарт Запит" (`apk.go`), "SmartZapyt" (Audit instructions), and "Прозоро" (`start.go`).
- 🎯 **No Back/Cancel in Templates message**: Let's check `templates.go`. The keyboard rows only contain `tpl` buttons. There is no `cancel` or `back` inline button. Verified.
- 🎯 **My Requests ~20s delay with no progress feedback**: Let's check `myrequests.go`.
- 🎯 **My Requests ~20s delay**: Verified. In `internal/bot/handlers/myrequests.go`, it checks up to 3 latest requests synchronously (with `m.deps.Dostup.GetRequestStatus`) AND sleeps for 1 second per check: `time.Sleep(1 * time.Second)`. Network delays + timeouts could easily reach 20s if the server is slow. Since there's no progress message sent before this blocking operation, the user perceives the bot as unresponsive.
- 🎯 **Status typo «У відовленні відмовлено»**: Verified in `internal/dostup/client.go:632`. Should be "У задоволенні відмовлено" (Rejected).
- 🎯 **Mixed 📬 plus ❌**: In `internal/bot/handlers/myrequests.go:94`, we have:
  ```go
  if st.Status == "successful" || st.Status == "partially_successful" {
          status = "✅"
  } else if dostup.ResponseArrived(st.Status) {
          status = "📬"
  }
  ```
  If `Status` is `rejected` (which means `ResponseArrived` is true), the emoji is `📬`. But `dostup.StatusLabel("rejected")` is `❌ У відовленні відмовлено`.
  Then it renders:
  ```
  1. 📬 19.10.2026 🌐
  ...
  ℹ️ ❌ У відовленні відмовлено
  ```
  This creates the "mixed 📬 plus ❌" issue.

- 🎯 **Previews contain raw semicolon-separated CSV and signature fragments**: Previews come from `st.ResponseExcerpt`. Let's check where this is handled and if we can truncate/clean it.
- 🎯 **Anonymity warning overpromises**: `start.go` says:
  ```
  🛡️ *БЕЗПЕКА ТА АНОНІМНІСТЬ:*
  • Запити подаються від громадської ініціативи «Громадський моніторинг» — ваше прізвище не обов'язкове.
  ```
  However, in `profile.go`:
  ```
  💡 Закон не вимагає підтверджувати особу — псевдонім не заборонений.
  ```
  While true that a pseudonym is technically not banned, the platform often rejects pseudonymous requests (or the agency ignores them citing incomplete data), so "БЕЗПЕКА ТА АНОНІМНІСТЬ" is an overpromise without legal assurances. We should rewrite this.

Now let's check `safego` failures:
- 🎯 **safego Data Race**: `safego` data race. `safego_test.go:58` writes to `logf` via test override (`logf = func`), while a background goroutine from `TestGoRecoversPanic` reads `logf`.
```
Write at 0x00000079c3c0 by goroutine 13:
  info-bot-go/internal/safego.TestPanicLogged()
      /app/internal/safego/safego_test.go:58 +0xf8
```
This is easily fixable by using a mutex around `logf`, or not using a global `logf` override that leaks across tests. Wait, `TestGoRecoversPanic` doesn't wait for the inner panic goroutine to fully finish writing to log! It sleeps 50ms and closes `done`, but the inner goroutine panics, recovers, and *then* logs to `logf`. The test might finish `TestGoRecoversPanic` before the log finishes. When `TestPanicLogged` runs concurrently (or afterwards), it writes to `logf`, causing a race condition.
- 🎯 **Previews contain raw CSV**: Let's check `internal/bot/handlers/myrequests.go` and `internal/dostup/` to see how `st.ResponseExcerpt` is populated or if we can truncate/sanitize it.
## Isolated Regression Tests ONLY (No product fixes)
I need to add isolated regression tests for:
- start/help/templates/my requests/profile/cancel navigation and stale callbacks;
- owner authorization and non-owner behavior;
- slow/failed/malformed responses, timeout/context cancellation;
- preview normalization for CSV/HTML/Unicode, truncation and Telegram escaping;
- status mapping;
- interval units, rounding, disabled and zero cases.
## Fix Plan
1. **Fix `safego` data race**:
   - `internal/safego/safego.go`: Provide a mutex for `logf` OR simply avoid doing `time.Sleep` without a proper `sync.WaitGroup` in the tests. Better yet, `logf` shouldn't be read/written without a mutex if we want to allow concurrent panics to log safely.

2. **Fix Status Typo and Mapping**:
   - `internal/dostup/client.go:632`: Fix "У відовленні відмовлено" -> "У задоволенні відмовлено".
   - `internal/bot/handlers/myrequests.go:94`: The mapping assigns `📬` for any `ResponseArrived(st.Status)`. It should map to `❌` for `rejected` statuses.

3. **Fix Interval Zero Cases**:
   - `internal/bot/handlers/start.go:256`: Instead of printing `m.deps.Cfg.DostupSyncMinutes` directly, which shows `0` if unset, print `20` (the actual default) if it's `<= 0`.

4. **Fix Preview Normalization**:
   - Create a helper to clean up CSV formatting or large whitespace blocks from `ResponseExcerpt` before presenting it in the UI in `myrequests.go`.

5. **Fix Mixed Branding & Anonymity warning**:
   - `start.go`: Remove or clarify "БЕЗПЕКА ТА АНОНІМНІСТЬ" to reflect reality.
   - `apk.go`: Update "Смарт Запит" to "Прозоро".

6. **Fix Cancel/Back Navigation in Templates**:
   - `internal/bot/handlers/templates.go`: Add a `« Cancel` or `« Back` inline button for easy navigation out of templates.

7. **Fix My Requests Blocking**:
   - `internal/bot/handlers/myrequests.go`: Before starting the synchronous checks (up to 3 seconds), send a "Завантаження статусів..." message and `Edit()` it after completion.
- 🎯 **Rating missing vs Response exists**: In `OpennessIndex` (`internal/dostup/ratings.go:215`), if `st.Requests < RatingMinRequests`, it returns `0, false`. If an agency has 1 request and 1 successful response, it has a response but NO rating (because it hasn't reached the minimum request threshold). This means missing rating does NOT equal "ignoring".
- 🎯 **Ranking Thresholds / Numerator / Denominator**: The logic `int(math.Round(100 * float64(st.Successful) / float64(st.Requests)))` computes the rating. It only counts `st.Successful` responses in the numerator and `st.Requests` in the denominator. `st.Requests < RatingMinRequests (5)` returns `0, false`. This explains the user's observation: an agency could reply (so it has a response), but if it only has 4 total requests, its rating is hidden.

Let's check the API endpoint `/api/analytics` and `/api/requests` to see how "refusal" is handled.
- 🎯 **Status Mapping Refusals vs Telegram (JS Side vs API)**: The Mini App receives `requests` array via `/api/requests`. Inside the JS client, it filters by status (`id: refused`, `id: replied`). In `requests.go` (if there is a mapping), does it map `rejected` to `refused` or something else? Wait, `internal/web/api.go` or `requests.go` exports the requests.

## Metric Source and Population Distinctions

**A. API Analytics (`/api/analytics`) vs Rating (`/api/rating`)**
- `AnalyticsResponse`: Only looks at `s.sentLog.ListAll()`. The `sentLog` contains *local bot-originated requests* plus requests from `MyRequestsFull()` (the shared portal account). This population represents the bot users and shared bot portal account ONLY. It does **not** represent all users of the public `dostup.org.ua` portal. Thus, labelling these stats as representing "Загальнонаціональні дані Доступу" in the Mini App is inaccurate.
- `RatingResponse`: The rating index logic (`OpennessIndex`) operates on `BodyStats` parsed dynamically from the public `dostup.org.ua` portal pages for *each agency* via `collectRatingBatch` using `dostup.RefreshBodyStats`. This population *does* reflect all third-party public portal users across the country, independent of the bot.

**B. Numerator and Denominator Breakdown**
- **Analytics (`api.go: handleAnalytics`)**:
  - *Population*: `sentLog` (Bot users + bot portal account).
  - *Numerator (Answered)*: Increments if `e.ReplyReceivedAt != ""` (which maps to `successful` / `partially_successful`).
  - *Denominator (Total)*: Total requests in `sentLog` where `Delivered == true`.
- **Rating (`ratings.go: OpennessIndex`)**:
  - *Population*: Entire public portal for a specific agency (`BodyStats`).
  - *Numerator*: `st.Successful` (only public portal requests marked as Successful or Partially Successful by any requester).
  - *Denominator*: `st.Requests` (Total public portal requests sent to that agency).
  - *Threshold constraint*: Rating hidden if denominator < `RatingMinRequests` (5).
