# Focused MiniApp source recovery (19 October 2026)

## Provenance and limits

Base: info-bot-go main commit `5e3ad6aab29633eebcc12831ff3d519466efef95`.
The deployed React source/project and source maps are absent from this repository's
reachable history. `mini-app/index.html` is a different legacy implementation.
Related public repositories were checked read-only: info-zaput-lite contains no
React source; goclaw-mini-app and phoenix-mini-app are unrelated dashboards.
The retained `index-BzgxKBHD.js` is the immutable shipped input. These files are
focused recovered source and explicit transformations, not a claim to have
restored the original application project. No vendor/runtime code is changed.

## Reproduce

From the repository root (Node 22+; no npm install required):

    node mini-app/build-patches.mjs
    node mini-app/build-patches.mjs --check
    node --test mini-app/patches/*.test.mjs
    node --check internal/web/static/assets/index-status-*.js
    go test ./internal/web ./internal/sentlog ./internal/dostup

The generator verifies the original SHA256 and exactly one occurrence of every
anchor. A changed base or ambiguous patch fails closed. It writes a distinct
content-hashed bundle and updates the HTML entrypoint, avoiding stale asset-cache
reuse. Keep the original bundle as the generator input. After editing patches,
remove superseded `index-status-*.js` outputs once the new output is verified.
Existing APK CI copies the same web/static tree, so its next build uses the fix.

## Semantics

- Ranking counts successful requests, with the explicit successful/all formula,
  minimum five requests, and portal source. It does not imply all replies,
  timeliness, or intent. Home portal totals use the same successful label.
- The separate analytics page describes the application's shared request journal.
  Rating response-time data is also explicitly labeled as application-journal data.
- Rejected and partially successful results populate their existing filters.
- Recorded replies without a success classification show 'Відповідь без оцінки'.
  A waiting_classification flag alone does not manufacture a received reply.
- All received-outcome categories, including refusal, count as answers. Bounce
  timestamps do not count as answers or as the latest reply.
- The all-replies filter includes success, unrated, partial and refusal; narrower
  refusal and partial filters remain available. The donut legend identifies its
  non-refusal subset so it is not confused with the total received count.

## Validation limits

Node source and generated-bundle tests, syntax validation, reproducibility checks,
and Go embedded-asset HTTP-serving tests pass. Browser fixture runner is at
`mini-app/qa/ui_test.py`; it runs actual compiled UI with synthetic read-only API
fixtures. It could not execute here: local Chromium launch was prevented by the
execution environment. No screenshot or visual QA is claimed. Verify the real UI
in Telegram after the reviewed deployment, including mobile layout, ranking,
analytics scope, refusal/partial filters and unclassified received answers.

For a machine with working Chromium/Playwright, install the official Playwright
Python package and Chromium, then run `python mini-app/qa/ui_test.py` (the script
uses `/usr/bin/chromium`; adjust that path for your machine). The fixture server
and browser are local-only. The script neither signs in nor writes to production.

## Blue design integration (19 October 2026)

The approved navy/cobalt/cyan design is applied to the **real** shipped app, not
served as a demo. Editable presentation sources live beside these patches:

- `blue-shell.js`: responsive app frame and existing four navigation destinations.
- `blue-home.js`: personal counts, data-driven next action, request list, latest
  reply, portal totals, links to the existing rating and analytics pages.
- `blue-request-card.js`: shared request card linking to its exact ID.
- `blue-request-detail.js`: new `/requests/$requestId` view of the existing
  authenticated store, escaped response excerpt, recorded events, safe portal URL.
- `blue-theme.css`: shared design tokens and responsive styling, including existing
  rating, analytics, and login screens.

The `sz()` helper produces React elements through the shipped JSX runtime. No
additional runtime dependency, vendored library change, external font, background
service, or new API is introduced. Full original React source is still unavailable;
this remains an explicitly recovered, reproducible layer rather than a complete
reconstruction. `manifest.json` verifies each prior function exactly before replacing
its presentation. Source arrays are concatenated in order, then optional `append`
is added. `styles` generates a content-hashed stylesheet loaded after the retained
original CSS. The build command above generates **both** JS and CSS.

### Function and data coverage

All four former routes (`/`, `/requests`, `/rating`, `/analytics`), six request
filters, status classification fixes, portal scope/formula labels, native base URL,
Telegram initData priority, saved app login, 401 login event, visibility refresh,
and per-user API adapter are retained. The original donut becomes explicit status
counts; bounced requests are now separately labeled rather than included in a
"Прострочено" legend. New request/details CTAs open the production bot linked in
this repository's README, `https://t.me/Infozaputbot`; they do not send messages or
create requests by themselves.

The detail view does not invent AI analysis, attachments, or services absent from
the API. It displays `responseExcerpt` as React text and opens a source only for
credential-free HTTPS URLs on `dostup.org.ua` or `dostup.pravda.com.ua`. A bounce is
never presented as a reply. An unavailable response text/date is stated as such.
The original five-weekday estimate is described as **розрахунковий строк**, with
an explicit caveat that extensions and case-specific rules are not included. It is
not presented as a newly verified legal deadline or a scheduled reminder.

### Verification and release

Run the existing reproduce commands, plus:

    node --check mini-app/patches/blue-shell.js
    node --check mini-app/patches/blue-home.js
    node --check mini-app/patches/blue-request-detail.js
    python mini-app/qa/ui_test.py

The Node suite exercises generated content hashes/order, fail-closed builder
behavior, unchanged transport/authentication, real data mapping, component output,
source URL allowlist, missing/unsafe text, response classification and navigation
contracts. Browser fixtures are synthetic and local-only. Do not treat component
fixtures as actual Chromium screenshots. In this execution environment Chromium
failed before page rendering (`socket() failed: Operation not permitted`); an
escalated attempt also failed in the environment launcher. Runtime DOM checks
using jsdom execute the complete compiled React/router bundle, but do not validate
pixel layout. A separate `miniapp-ui` CI job runs real Chromium at mobile/desktop
widths and uploads reports/screenshots for review before promotion.

The preinstalled `/usr/bin/go` is not a Go compiler, so official Go 1.24.9 was
downloaded and its archive verified against the official release SHA256 before
local Go tests. Verify authenticated Telegram behavior after deployment.

Commit the new generated JS/CSS and updated HTML along with sources and tests.
The old input bundle stays immutable. The previous production bundle is retained
for rollback/cache continuity. A push to `main` automatically deploys the embedded
static tree and builds the APK in this repository; review and validate on a branch
before promotion. Revert the integration commit and redeploy to roll back the UI.

To reproduce the runtime-only DOM test without changing app dependencies:

    npm install --prefix /tmp/smartzapyt-dom-qa --ignore-scripts --no-audit --no-fund jsdom@30.1.3
    SMARTZAPYT_QA_NODE_MODULES=/tmp/smartzapyt-dom-qa/node_modules node mini-app/qa/dom_test.mjs

These fixtures also cover complex email-style message IDs, failed initial loads,
failed refreshes retaining prior records, successful-empty retries, bounced-only
accounts, and received replies whose receipt date is unavailable.
