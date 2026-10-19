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
