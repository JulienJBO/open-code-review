# MES-3763 — OCR v2 stabilisation and Qwen qualification

## Scope

Stabilise the existing OpenCodeReview fork by combining the existing sharding work (MES-3435 / upstream PR #1640) and provider-aware request input hard-cap work (MES-3018 / upstream PR #1641) on a dedicated branch based on current upstream. This ticket delivers OCR stabilisation and measurement only; it does not build the Pi reviewer or the A/B runner, and it must not make OCR a required development/CI gate.

## Required behavior

- Preserve small-diff behavior while deterministically accounting for every selected file and chunk on large diffs. No selected content may be silently omitted or truncated; partial/unknown coverage must not report a complete review.
- Apply an effective input-token ceiling to every request, including compression and every task family, accounting for tools/schemas and calibrated margin. Reject oversize requests before HTTP; do not claim a provider-confirmed 32k ceiling without provider evidence.
- Transmit raw chunk history once per chunk on the nominal path; count explicit re-emissions and receipts accurately.
- Keep output machine-readable and stable: resolved input base/head, a SHA-256 and byte count for the supplied ticket context (without persisting its text), findings, coverage, request/token/cache/cost/latency/error usage, and provider/model identity.
- Provide a pinned fork ref or reproducible binary and a simple rollback path. Modify the Messenger wrapper only if the fork is actually certified and wrapper changes are necessary.

## Verification and release boundary

- Classify each finding on upstream PR #1640 and #1641 as fixed, disproved with evidence, or deferred with a reason.
- Exercise all seven paths: MAIN, PLAN, FILTER, GROUPING, RE_LOCATION, COMPRESSION, and scan, including small diffs, multi-file/hunk-heavy input, concurrent groups, refetch/resume, and exhausted budgets.
- Run Go tests with race detection plus project checks and coverage; do not mask failures or coverage gaps.
- Reuse the five real Messenger diffs from MES-3016 (or exact equivalents) for the Qwen3.7-Flash smoke. Before any completion request, verify credential availability and seek a billing/prepaid check; enforce a hard aggregate-token budget and strict timeout. With `OCR_RAW_LOGGING=1`, report per-request provider prompt/completion/total tokens, cache read/write, task type, latency, retries/errors, estimated cost, and the `prompt_tokens <= 32000` result, plus per-run max/p50/p90/p99 where sample size permits and coverage. If provider access or billing cannot be established without a human console action, preserve the local deliverable but block the ticket on that exact external prerequisite; never substitute DeepSeek or claim Qwen parity.
- Do not merge upstream PRs or configure production/global review gates. Upstream publication is optional and conditional on compatibility and CLA; fork delivery does not depend on upstream merge.

## Baseline observed

- Current upstream at planning: `origin/main` `fabbdb296b0d97e2140ada8d54ca7d6d6d1d7ad4` (v1.12.13).
- Fork `main` at planning: `486022daaf14f7142275eddb9b3cacc3cc5dadfa`, 0 commits ahead / 11 behind current upstream; existing fork branches MES-3435 `d4cf2c77f47b488845c30aec59b1f4bde9651027` and MES-3018 `7ef27b508718870eec3de477016fffccc6ca9355`.
- Upstream PR #1640 is open/ready and #1641 is open/draft on base `a758d9cbfb689937c7857ad64b2dd66adb58c0c2`; both have `REVIEW_REQUIRED`, with 10 and 2 inline findings respectively. Their Alibaba CLA check is pending; upstream publication/merge is not a prerequisite to fork-only delivery.
- The installed `ocr` command is `v1.12.11` at `a758d9cb`, built 2026-09-29; no `opencodereview` command name is installed.
