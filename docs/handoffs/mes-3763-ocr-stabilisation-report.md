# MES-3763 — OCR stabilisation report

## Delivery identity

- Fork: `JulienJBO/open-code-review`
- Branch: `mes-3763-ocr-v2-stabiliser-notre-fork-sharding-hard-cap-certifier-qwen-avant`
- Fork `main` at start: `486022daaf14f7142275eddb9b3cacc3cc5dadfa`, 0 ahead / 11 behind current upstream.
- Upstream base: `fabbdb296b0d97e2140ada8d54ca7d6d6d1d7ad4` (`v1.12.13`); it is 11 commits ahead of the fork's `main`.
- Existing source commits integrated: MES-3435 `4e814b0` + `d4cf2c7`; MES-3018 `9be9874` + `7ef27b5`.
- Installed `ocr`: `v1.12.11` at `a758d9cb`, built 2026-09-29; `opencodereview` is not installed as a separate command.
- The final candidate SHA and the SHA-256 of the binary rebuilt from that exact SHA are recorded in the linked fork PR and Linear delivery evidence; the generated binary is not committed.
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

## Qwen/DashScope provider gate

- `bwenv run` reports `DASHSCOPE_API_KEY` available without revealing the value.
- A read-only `GET https://dashscope.aliyuncs.com/compatible-mode/v1/models` returned `HTTP 401`, provider code `invalid_api_key`.
- No model completion request was sent. Therefore there are no new provider prompt-token, cache, cost, latency, or coverage measurements; no Qwen parity/32k claim is made. The API documentation surfaced no billing-balance endpoint, so current credit/arrears cannot be verified by this client.
- Operator action needed: refresh/replace `DASHSCOPE_API_KEY` through the approved 1Password/`bwenv` path, then verify the DashScope account has active entitlement and prepaid credit for Qwen3.7-Flash. On wake, re-read MES-3763 and current PR/reviews/CI, rerun the exact five-case corpus (15-minute timeout and 100,000-token aggregate cap per case) with `OCR_RAW_LOGGING=1`, and record per-request provider metrics. The invocation is `bwenv run -- env OCR_RAW_LOGGING=1 dist/opencodereview review --from <base> --to <head> --provider dashscope --model <verified-Qwen-Flash-ID> --max-request-input-tokens 32000 --request-token-safety-margin 0.9 --max-tokens-budget 100000 --timeout 15 --format json`; repeat for each case only after GET `/models` and manual credit verification succeed. Do not use DeepSeek as a substitute.

## Upstream PRs / CLA

- Alibaba PR #1640 is open/ready and #1641 is open/draft; both are on the older base `a758d9cbfb689937c7857ad64b2dd66adb58c0c2` and have `REVIEW_REQUIRED`, with 10 and 2 inline findings respectively. The findings above are addressed in this integration branch; upstream branches have not been pushed or merged.
- Alibaba CLA status was pending at inspection; upstream publication/merge is not required for the fork. No CLA or upstream merge is claimed.

## Reproduction and rollback

- Rebuild from the pinned candidate SHA with `make build`; verify with `dist/opencodereview --version` and SHA-256 recorded above.
- Revert to the previously used fork ref by checking out that pinned ref and rebuilding; no installed or global OCR binary is replaced by this work.
