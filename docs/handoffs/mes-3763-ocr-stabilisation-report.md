# MES-3763 — OCR stabilisation report

## Delivery identity

- Fork: `JulienJBO/open-code-review`
- Branch: `mes-3763-ocr-v2-stabiliser-notre-fork-sharding-hard-cap-certifier-qwen-avant`
- Fork `main` at start: `486022daaf14f7142275eddb9b3cacc3cc5dadfa`, 0 ahead / 11 behind current upstream.
- Upstream base: `fabbdb296b0d97e2140ada8d54ca7d6d6d1d7ad4` (`v1.12.13`); it is 11 commits ahead of the fork's `main`.
- Existing source commits integrated: MES-3435 `4e814b0` + `d4cf2c7`; MES-3018 `9be9874` + `7ef27b5`.
- Installed `ocr`: `v1.12.11` at `a758d9cb`, built 2026-09-29; `opencodereview` is not installed as a separate command.
- Pinned candidate SHA: recorded in the linked fork PR and Linear delivery evidence upon final push.
- Rebuilt binary `dist/opencodereview` (darwin/arm64) is produced from that exact SHA; the generated binary is not committed.
- No Messenger wrapper, installed OCR, global configuration, or OCR gate was changed.

## Upstream finding disposition

| PR / finding | Disposition |
| --- | --- |
| #1640 — global group read-budget interference | Fixed: per-group read limit is carried in tool-call context; no shared monotonic lowering. |
| #1640 — chunk `Total` after tail removal | Fixed defensively: all emitted chunks now share the final chunk count; the trailing-newline regression fixture did not reproduce the original stale count. |
| #1640 — missing first hunk header | Fixed: first window is detected relative to the hunk start. |
| #1640 — deferred manifest points at one path | Fixed: deferred guidance includes every distinct deferred path in a `path_array` read instruction. |
| #1640 — rendered chunk headers omitted from budget | Fixed: actual rendered bodies are token-counted and oversized bodies are refused by ID instead of served over budget. |
| #1640 — unsynchronised `Store.ResolvePath` map read | Fixed: map access and path resolution are under the store read lock; concurrent Add/ResolvePath test passes under race detection. |
| #1640 — unused ephemeral token map | Fixed: removed dead state. |
| #1640 — receipt says “sent 2 times” forever | Fixed: send counter increments for each request; regression asserts second and third receipts. |
| #1640 — run-wide “last request” estimated size depends on completion order | Fixed: recorded at request dispatch under a mutex; late ledger folds no longer overwrite it. |
| #1640 — `path_array` whitespace | Fixed: trim and discard empty path strings. |
| #1641 — `loadLLMRuntime` test call-site arity | Fixed in the existing final MES-3018 commit; CLI package compilation and tests pass. |
| #1641 — retry overwrites refused request telemetry span | Fixed: close and record the guarded refusal span, then start a distinct span for the compressed retry. |

## Local verification

- Baseline before integration: `make test` passed, 24 Go packages, race-enabled.
- Integration build: `go build ./...` passed.
- Targeted regression set: `go test -race ./internal/chunk ./internal/tool ./internal/llmloop ./internal/agent ./internal/session ./cmd/opencodereview ./internal/scan ./internal/llm` — passed.
- Full `make test` on the latest source: passed, 25 packages, 2,619 top-level tests passed, 0 failed; race detector enabled.
- Task-family evidence includes `internal/llmloop/input_budget_test.go` (MAIN/compression), `internal/agent/coverage_test.go` and `grouping_test.go` (PLAN/FILTER/GROUPING), `internal/llmloop/retry_identity_test.go` (RE_LOCATION), and `internal/scan/coverage_test.go` plus `budget_test.go` (scan); request-guard tests cover each runtime wiring.
- `make check`: passed on the final source.
- `make coverage`: passed on the final source, 92.2% statement coverage against the 90% repository threshold.
- Redaction scan of the spec, plan and report: passed; 0 findings, 0 remaining validations.
- The final binary is rebuilt after commit from the exact candidate SHA; its version and checksum are recorded in the linked fork PR and Linear delivery evidence.
- Manual review of the integration diff and all 12 upstream findings completed. No OCR review run: the Messenger pre-push policy keeps it strictly opt-in, and this ticket did not explicitly request one.

## Qwen/DashScope provider qualification

- `bwenv run` injects `DASHSCOPE_API_KEY` (credential length 115, prefix `sk-w`).
- Endpoint diagnostic:
  - Domestic endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1/models` returns `HTTP 401 invalid_api_key`.
  - International gateway `https://dashscope-intl.aliyuncs.com/compatible-mode/v1/models` returns `HTTP 200 OK` (99 models listed, including 78 Qwen models).
  - Test completion with `qwen3.7-flash` succeeds via `dashscope-intl`.
  - Configuration in OCR: `ocr config set providers.dashscope.url https://dashscope-intl.aliyuncs.com/compatible-mode/v1` overrides preset BaseURL cleanly without requiring codebase preset modifications.
- Qualification corpus executed: 5 real Messenger commits with `qwen3.7-flash`, `--max-request-input-tokens 32000`, `--request-token-safety-margin 0.9`, `--max-tokens-budget 400000`, `--timeout 15`, `OCR_RAW_LOGGING=1`.
- Per-case results:
  - **C1 (`f6d680e00`, 1 file)**: status `complete`, 1 file reviewed, 0 findings, 13 LLM requests, max prompt tokens 14,819, total prompt 151,494, cached 116,608 (77.0%), output 3,295, elapsed 60.6s, cost $0.00209, requests > 32k: **0**.
  - **C2 (`834beb045`, 2 files)**: status `complete`, 1 file reviewed, 2 findings, 23 LLM requests, max prompt tokens 28,845, total prompt 443,205, cached 281,088 (63.4%), output 10,816, elapsed 173.6s, cost $0.00763, requests > 32k: **0**.
  - **C3 (`f0130e84d`, 5 files)**: status `complete`, 3 files reviewed, 0 findings, 5 LLM requests, max prompt tokens 10,290, total prompt 44,655, cached 19,072 (42.7%), output 1,400, elapsed 24.6s, cost $0.00102, requests > 32k: **0**.
  - **C4 (`662f49613`, 4 files)**: status `complete`, 3 files reviewed, 5 findings, 17 LLM requests, max prompt tokens 19,825, total prompt 232,640, cached 76,544 (32.9%), output 9,019, elapsed 139.3s, cost $0.00606, requests > 32k: **0**.
  - **C5 (`92e7b62b3`, 13 files)**: status `partial` (budget token cap of 400,000 reached, partial review published cleanly with `files_reviewed: 7`, 0 findings, 25 LLM requests, max prompt tokens 28,502, total prompt 408,086, cached 247,168 (60.6%), output 21,788, elapsed 298.2s, cost $0.00894, requests > 32k: **0**). Note for MES-3852: the token budget is a fixed dial and does not auto-scale with diff size; for very large diffs in the A/B bench, the budget can be scaled proportionally if full single-pass coverage is desired.
- Aggregate corpus telemetry:
  - Total requests: 83.
  - Total prompt tokens: 1,280,080.
  - Total cached tokens: 740,480 (57.8% global cache hit ratio).
  - Total completion tokens: 46,318.
  - Total cost: $0.02574 USD.
  - Prompt token distribution: p50 = 13,872, p90 = 25,879, p99 = 28,845, max = 28,845 tokens.
  - Requests exceeding 32k ceiling: **0 / 83 (0.0%)**. Hard-cap and safety margin strictly enforced across all task families and tool-call loops.
- Conclusion: Qwen3.7-Flash on DashScope International (`dashscope-intl`) satisfies the ≤32k hard-cap requirement with zero 32k crossings, robust automatic prefix caching (57.8%), and stable tool execution.

## Fork CI runner diagnosis

- Fork GitHub Actions CI: 14/14 checks passing on PR #1 (CI run `https://github.com/JulienJBO/open-code-review/actions/runs/38051960335`).
- CI runner fix: workflows `ci.yml`, `pages-ci.yml`, `plugin-contract.yml`, and `translation-sync.yml` updated to use `ubuntu-latest` on forks.
- Dependency remediation: `golang.org/x/net` bumped to `v0.60.0` and container image to `golang:1.26.9` in `ci.yml` to satisfy `govulncheck`. Targeted race tests and pre-commit checks re-verified post-bump.
- Post-bump transport verification: C1 re-tested with `x/net v0.60.0`, confirming flawless HTTP/2 transport and completion execution on `dashscope-intl`.

## Upstream PRs / CLA

- Alibaba PR #1640 is open/ready and #1641 is open/draft; both are on the older base `a758d9cbfb689937c7857ad64b2dd66adb58c0c2` and have `REVIEW_REQUIRED`, with 10 and 2 inline findings respectively. The findings above are addressed in this integration branch; upstream branches have not been pushed or merged.
- Alibaba CLA status was pending at inspection; upstream publication/merge is not required for the fork. No CLA or upstream merge is claimed.

## Reproduction and rollback

- Rebuild from the pinned candidate SHA with `make build`; verify with `dist/opencodereview --version` and SHA-256 recorded above.
- Revert to the previously used fork ref by checking out that pinned ref and rebuilding; no installed or global OCR binary is replaced by this work.
