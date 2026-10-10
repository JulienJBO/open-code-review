# OCR v2 stabilisation Implementation Plan

> **For agentic workers:** Execute this plan inline, task by task. Each task is testable before proceeding.

**Goal:** Integrate and stabilise existing diff sharding and request hard-cap work in the OCR fork, then qualify it with reproducible local and (if authorized) provider measurements.

**Architecture:** Keep the Go CLI and current runtime boundaries. Carry the existing MES-3435 and MES-3018 implementations onto a dedicated branch based on current upstream, then correct proven defects at the chunk, store, request-accounting, and telemetry boundaries. Keep provider smoke and Messenger wrapper changes outside the local implementation unless the exact provider proof justifies them.

**Tech Stack:** Go, `go test -race`, Makefile checks, GitHub CLI, Orca Linear.

**Spec:** `docs/specs/mes-3763-ocr-v2-stabilisation.md`

## Global Constraints

- Do not modify main or force-push; use a dedicated integration branch and publish only to the fork.
- Do not duplicate the already existing MES-3435/MES-3018 implementation.
- Do not present partial/unknown coverage as a complete review or claim a provider-confirmed 32k cap without measured evidence.
- Do not make OCR a required development, pre-push, or CI gate; do not build the Pi reviewer/A-B runner.
- Require an explicit provider-access/billing check and a strict spend/time limit before any DashScope request.
- A ready PR uses `manual-merge`; never arm auto-merge.

---

### Task 1: Carry the existing implementations to current upstream

**Files:**
- Git history: commits `4e814b0`, `d4cf2c7` from `fork/mes-3435-shard-review-context`; `9be9874`, `7ef27b5` from `fork/mes-3018-hard-cap-request-input-tokens`.
- Worktree branch: `JulienJBO/mes-3763-ocr-v2-stabiliser-notre-fork-sharding-hard-cap-certifier-qwen-avant`.

- [x] **Step 1:** Cherry-pick the existing sharding commits, then the hard-cap commits, resolving only conflicts required by upstream changes.
- [x] **Step 2:** Run `go build ./...` and targeted `go test ./internal/chunk ./internal/agent ./internal/llm ./internal/llmloop ./internal/tool ./internal/scan ./cmd/opencodereview`.
- [x] **Step 3:** Inspect diff and list actual post-port code paths before modifying any production code.

### Task 2: Correct verified sharding and coverage findings

**Files:**
- Inspect/modify: `internal/chunk/chunk.go`, `internal/chunk/manifest.go`, `internal/chunk/read.go`, `internal/chunk/store.go`, `internal/agent/context_plan.go`, `internal/tool/file_read_diff.go`.
- Tests: matching `*_test.go` files in those packages.

- [x] **Step 1:** For each applicable PR #1640 inline finding, add or extend one focused regression test and run that test to observe the expected failure.
- [x] **Step 2:** Verify race-safe `Store.ResolvePath`, rendered-header token budget, hunk header/count metadata, deferred multi-path manifest, cross-group budget behavior, and no silent path/chunk loss against current call sites.
- [x] **Step 3:** Implement only proven corrections, rerun each focused test, then run `go test -race ./internal/chunk ./internal/agent ./internal/tool`.
- [x] **Step 4:** Record each #1640 finding as fixed, disproved with evidence, or deferred with a concrete reason in the delivery report.

### Task 3: Correct ephemeral-history and hard-cap/retry findings

**Files:**
- Inspect/modify: `internal/llmloop/ephemeral.go`, `internal/llmloop/loop.go`, `internal/llm/request_input.go`, `cmd/opencodereview/shared.go`, `cmd/opencodereview/manual_e2e_retry_test.go` if present on the integrated branch.
- Tests: corresponding `*_test.go` files, including `internal/llmloop/input_budget_test.go` and request-input tests.

- [x] **Step 1:** Add failing regression tests for receipt re-emission counts, dead accounting state if still present, effective request limit across task families/compression, and span closure/retry identity for over-budget requests.
- [x] **Step 2:** Run the focused tests and confirm each fails for the intended observable behavior.
- [x] **Step 3:** Implement minimal corrections; every oversize request must be rejected before HTTP and compression retries must be independently accounted.
- [x] **Step 4:** Run `go test -race ./internal/llm ./internal/llmloop ./cmd/opencodereview ./internal/scan` and classify the #1641 findings.

### Task 4: Verify task-family coverage and machine-readable contract

**Files:**
- Inspect/modify: `cmd/opencodereview/output.go`, `cmd/opencodereview/review_cmd.go`, `cmd/opencodereview/scan_cmd.go`, `internal/agent/agent.go`, `internal/scan/agent.go`, and relevant docs/tests.

- [x] **Step 1:** Identify exact test entry points for MAIN, PLAN, FILTER, GROUPING, RE_LOCATION, COMPRESSION, and scan; add bounded regression cases where existing tests do not cover the integrated paths.
- [x] **Step 2:** Verify small diff compatibility, multi-file/hunk-heavy chunk coverage, concurrent groups, refetch/resume, and exhausted-budget behavior.
- [x] **Step 3:** Verify stable JSON reports base/head, ticket context, findings, coverage, usage and provider identity; partial/unknown coverage must be machine-distinguishable from complete.
- [x] **Step 4:** Run all affected package tests with `-race`.

### Task 5: Run repository validation and produce a reproducible fork artifact

**Files:**
- Build output: `dist/opencodereview` (do not commit generated local binary unless release policy requires it).
- Report: `docs/handoffs/mes-3763-ocr-stabilisation-report.md`.

- [x] **Step 1:** Run `make test`, `make check`, and `make coverage`; preserve exact package/test totals and failures.
- [x] **Step 2:** After commit, build the release binary from the final integration SHA, record `--version` and SHA-256 in the linked PR/Linear evidence, and document invocation plus rollback to the prior pinned ref.
- [x] **Step 3:** Classify all upstream inline findings and document coverage, commands, local evidence, and provider limitations in the report.

### Task 6: Qualify DashScope/Qwen only after access and billing checks

**Files:**
- Test corpus from MES-3016 or its exact equivalent; report: `docs/handoffs/mes-3763-ocr-stabilisation-report.md`.

- [x] **Step 1:** Check availability of the approved secret through `bwenv` without printing it; the read-only models endpoint returned HTTP 401 `invalid_api_key`, and no billing-balance endpoint was available.
- [ ] **Step 2:** Blocked on a refreshed credential and human confirmation of Qwen model entitlement and prepaid credit; no completion call was sent.
- [ ] **Step 3:** Provider metrics remain unmeasured; make no Qwen or 32k parity claim.
- [x] **Step 4:** Make no provider call; stop at the operator-block procedure with the exact external action and resumption command. After wake and confirmed credential/model/credit, run each of the five MES-3016 cases with `bwenv run -- env OCR_RAW_LOGGING=1 dist/opencodereview review --from <base> --to <head> --provider dashscope --model <verified-Qwen-Flash-ID> --max-request-input-tokens 32000 --request-token-safety-margin 0.9 --max-tokens-budget 100000 --timeout 15 --format json`.

### Task 7: Review and publish without merge automation

- [x] **Step 1:** Run `make license-check`, `make english-check`, inspect `git diff --check`, and review the full diff. Do not launch OCR review: the Messenger pre-push policy keeps it opt-in and the operator did not request it.
- [x] **Step 2:** Commit on the dedicated branch, run `pnpm run pr:check "feat(MES-3763): Stabilise OCR sharding and hard cap"` in the owning Messenger worktree, then run appropriate fork-local checks on the committed SHA.
- [ ] **Step 3:** Push to `fork`, create a ready PR for the fork with `manual-merge`, never auto-merge; attach the PR to MES-3763 and post verified evidence.
- [ ] **Step 4:** After publishing the PR, post an action-first Linear comment naming the exact credential/billing action and PR, transition to `Awaiting Operator`, then remove `feature-running`; wake through `feature-wake` and re-read the ticket/PR/reviews/CI before further mutation.
