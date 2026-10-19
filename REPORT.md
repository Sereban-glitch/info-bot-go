# SmartZapyt audit: corrected review and bounded fixes

## Scope and provenance

Reviewed main `47acbbb56f4641446a1f5670d29bfdc725df3f1c` and original PR #7
`7c28014d35095e741980473bfff36494573a4a74`.
Original PR #7 added four audit/proposal/test files and changed no production code.
This revision replaces illustrative tests with tests that invoke real functions,
fixes asynchronous test synchronization, corrects bot statistics and classification-gate diagnostics, and makes two bounded text corrections.
It does not fix all of the product issues below. No merge or deployment was performed.

## Changes in this revision

- Correct `StatusLabel("rejected")` to `❌ У задоволенні відмовлено`.
- Use a plain-text `Для власника:` heading in Help, consistent with its existing
  plain-text send mode. Keep the existing owner authorization boundary.
- Replace `audit_test.go`'s empty tests and simulated implementation branches with
  assertions against production status/rating/classification functions.
- Exercise the registered Help handler offline using a capturing context. Check
  both owner and regular-user output, without Telegram API calls.
- Exercise `GetRequestStatus` against synthetic local HTTP fixtures, including
  incoming letters while awaiting classification, acknowledgement, and refusal.
- In `safego`, replace the prior sleep-based asynchronous panic test with explicit
  completion synchronization. Check panic logging and the next task's execution.
  No production logging mutex or panic-recovery behavior is changed.
- Use a shared `sentlog.Summarize` for bot dashboard and synchronization counters.
  Delivered requests form one consistent population; delivery failures and
  withdrawals without a substantive reply are separate from answered and awaiting
  requests. A reply received before withdrawal remains counted.
  Acknowledgements remain a subset of awaiting replies. Unconfirmed sends are
  separate. Legacy bounce timestamps do not count as answers.
- Render owner and personal `/stats` from the actual sent log rather than mixing
  cached all-channel counters and portal-only pending counts. A cross-user fixture
  verifies that the personal view remains limited to its owner.
- Share the effective synchronization interval between the worker and display,
  so a zero/negative setting consistently means 20 minutes.
- Show the last recorded unclassified count independently from reply count, with
  wording explicitly scoped to the journal, not a claim of live portal freshness
  or a user-authored assessment.
- Correct `/api/requests`' classification-required error: the portal requires
  classification of earlier shared-account answers; editing the subject will
  not fix that. A full local HTTP/authenticated handler test proves no portal
  POST and no sent-log append occur on that blocked path. No new missing-rating
  gate, automatic outcome decision, or external-write retry is introduced.
- Remove the WIP notes and invalid `fix_proposal.diff`. The latter failed
  `git apply --check` at line 5 and was not an applicable patch.

## Corrected findings

### Response presence, outcome, and rating are different

`internal/dostup/client.go:ResponseArrived` includes `waiting_classification`,
`rejected`, `not_held`, successful and partially successful outcomes. The actual
parser independently records incoming letters. `ClassifyReply` recognizes a
refusal as substantive and an acknowledgement as interim.

`internal/bot/handlers/dostupsync.go:syncStatuses` records a substantive reply
before attempting portal outcome classification. Therefore, the blanket claim
that awaiting classification or refusal necessarily means “no response” is false.
This is a code-path result, not a claim that every live record is correct.

`internal/dostup/ratings.go:OpennessIndex` uses public agency statistics:
rounded `100 * Successful / Requests`, with at least five requests required.
A missing rating is not evidence of ignoring requests. Successful outcomes are
not all letters received and do not measure response timeliness.

### Actual frontend problems remain open

The shipped compiled rating component labels `successful` as “Відповідей” and
uses “Хто відповідає вчасно, а хто ігнорує запити.” This overstates what the metric
measures. Correct labels should describe successful/partially successful outcomes,
coverage and the minimum sample, without inferring ignoring from missing data.

The requests screen offers refused/partial filters, but its mapping does not
retain these outcomes in the normalized display status. This is an outcome-filter
problem, not evidence that all refusals vanish from the answered count.

The current React source/build manifest is absent from this repository; the
shipped app is `internal/web/static/assets/index-BzgxKBHD.js`. The separate
`mini-app/index.html` is not its React source. Do not patch minified assets by hand;
restore the matching source/build workflow before changing these UI behaviors.

### Analytics population claim corrected

`/api/analytics` uses delivered entries in the bot's sent log, including imported
shared-account requests. It is not nationwide portal analytics. However, the
shipped analytics screen already describes its data as app-wide. The homepage's
“Загальнонаціональні дані Доступу” card loads `/api/portal-stats`, not
`/api/analytics`. The original audit conflated those screens and sources.

### Other confirmed but unfixed issues

- `/my` increments its three-request check counter only after successful lookups.
  Failures can cause more than three attempts. Each HTTP client request can wait
  up to 60 seconds, and the handler supplies no initial progress message.
- `/my` preview truncation slices bytes, which can cut UTF-8; message truncation
  can also cut HTML. Removing all semicolons would damage legitimate content and
  is not an acceptable general preview sanitizer.
- The raw zero-minute `/stats` display is fixed by sharing the worker's effective
  interval. `config.Load` still does not populate this field from the environment;
  wiring new environment behavior is outside this change. Production configuration
  was not inspected.
- Templates have no inline Back/Cancel button. Adding callback `cancel` alone
  would not work: the existing Cancel module registers `/cancel`, not that
  callback. Navigation needs a registered handler and session-state tests.
- `/my` can display a generic arrival icon beside a refusal label. This cosmetic
  inconsistency does not establish loss of the response.

## Assessment/gating risks intentionally not redesigned

- The existing new-request gate is the portal's `ErrNeedsClassification`, not
  absence of an agency rating. This patch keeps that restriction and corrects
  its misleading API error; it does not invent a new indefinite local gate.
- `SentEntry.LastStatus` has no user/rule/AI provenance. It must not be presented
  as a user's satisfaction judgment. The new dashboard does not do that.
- `syncStatuses` finalizes response presence before `autoclassify`. Later cycles
  skip finalized entries. If rule/AI inference or `ReportStatus` fails, assessment
  can remain unfinished; blindly retrying would expand external portal writes.
- Existing automatic classification maps broad text markers and AI `substantive`
  to `successful`; this is not equivalent to user satisfaction. No new AI calls,
  portal classifications, retries or policy changes are introduced here.
- The manual classification callback currently lacks a sent-log ownership check
  and does not synchronize the resulting local `LastStatus`. This is a separate
  security/correctness priority; this patch does not claim to fix it. The stale
  status limitation is why the new count says “За журналом”.
- The revised bot statistics do not change the Mini App analytics implementation;
  its delivery/outcome accounting should be reviewed separately.

## Test quality and limitations

The original ten audit tests were not ten end-to-end regressions: three were
empty, several reimplemented logic or checked constants, and only a subset called
production functions. Existing `ratings_test.go`, `analytics_api_test.go`, and
`rating_api_test.go` already exercise important production boundaries.

The revised tests use synthetic data, offline Telegram objects and local HTTP
fixtures. No bot process, SMTP sender, live portal session or AI service is started.
Go modules are downloaded before testing, verified, then module-network access is
disabled with `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`.
No production credentials or production data are needed.

## Reconciliation and fresh validation

This is a reconstructed candidate, not a byte-identical recovered artifact.
The original local workspace was lost. Eight files were recovered from commit
`d6f8fe67f3922c6963f05722a551a3bf4086b63a`; remaining reviewed changes were
reconstructed from the retained task context. The candidate is based on the
concurrent branch head `e4c7f65efd4e132c051c410050725c2942e4223e`.

The useful arithmetic case from the concurrent change is retained as
`TestAuditOpennessIndexDenominatorArithmetic`. It calls production `OpennessIndex`
with synthetic 10-success/10-request and 10-success/11-request aggregates and
asserts `(100, true)` and `(91, true)`. It does not establish how the live portal
feed counts awaiting classification, attachments or requester assessments.

Historical validation reported before workspace loss is not reused as fresh
proof. This rebuilt candidate receives new full tests, race checks, vet, build,
and independent code review before publication. Fresh results are recorded below
only after execution.

Official Go 1.24.3 linux/amd64 archive SHA256:
`3333f6ea53afa971e9078895eaa4ac7204a8c6b5c68c10e6bc9a33e8e391bdd8`.
Module downloads are completed before testing; module network access is then
disabled using `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`.

Fresh rebuilt-candidate results on 2026-10-19 (all exit 0):

- `go mod verify`: all modules verified.
- `go test ./... -count=1`: PASS.
- `go test -race ./... -count=1`: PASS.
- `go vet ./...`: PASS.
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o <output> .`: PASS, normal VCS stamping enabled in the new standalone clone.
- `go test -race ./internal/safego -count=100`: PASS.
- Targeted verbose handler/parser/summary/interval/gate tests, including the
  arithmetic case: PASS; exact selection and output are in `rebuilt-targeted.log`.
- `git diff HEAD --check`: PASS.

No built binary was started. No live Telegram, SMTP, portal, or AI endpoint was
used by validation. The request-gate integration test uses a local HTTP fixture
and asserts zero portal POSTs and zero sent-log appends.

The complete exported patch must be checked against the exact concurrent base
head above and independently reviewed before the branch is updated. These fresh
results supersede the lost workspace's earlier validation for this candidate.

## Merge-readiness decision

The original PR should not be merged as submitted: it contains unsupported audit
claims, weak tests and an invalid proposal. This candidate is ready for review as
a bounded audit correction, stronger tests, corrected bot dashboard counts,
truthful classification-gate diagnostics, and two small text fixes. It is not a
complete assessment/provenance redesign, nor a fix for the rating UI or
refusal/partial filters. Re-run required CI on the actual
updated PR head before approving merge. Main pushes trigger deployment, so merge
and deployment remain separate explicit decisions.
