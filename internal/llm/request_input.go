// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DefaultRequestInputSafetyMargin is the share of the provider limit the
// guard is willing to spend. The remaining 10% absorbs the gap between the
// tokenizer we count with and the one the provider counts with: a non-native
// tokenizer systematically under-counts, and a provider price cliff is not
// something to discover from a bill.
const DefaultRequestInputSafetyMargin = 0.90

// Framing allowances, in tokens, for the parts of the serialized request that
// are structure rather than prose: the JSON envelope (model, tool_choice,
// cache counters) and each message's role/block/tool-call identifiers.
// They are deliberately small and deliberately non-zero — an estimate that
// ignores framing is an estimate that under-counts on every single request.
const (
	requestEnvelopeFramingTokens = 16
	messageFramingTokens         = 4
	toolResultFramingTokens      = 3
)

// RequestInputBudgetExceededCause is the structured cause reported when the
// guard refuses a request. It is stable because downstream tooling classifies
// refusals on it; it never carries request content.
const RequestInputBudgetExceededCause = "request_input_budget_exceeded"

// TaskTypeUnknown is recorded for requests that carry no RequestMeta. An
// honest placeholder beats a fabricated task type: grouping and scan requests
// legitimately have none.
const TaskTypeUnknown = "unknown"

// RequestInputBudget is the per-request input ceiling. It is deliberately not
// derived from the per-group prompt ceiling or from the run budget: those
// bound different things (context selection, aggregate spend) and none of them
// means "never send a request whose input exceeds N".
//
// The zero value is disabled, which keeps every existing call site unchanged.
type RequestInputBudget struct {
	// ProviderLimitTokens is the provider's price/eligibility cliff for a
	// single request, in tokens. Zero disables the guard.
	ProviderLimitTokens int
	// SafetyMargin is the share of ProviderLimitTokens the guard may spend,
	// in (0, 1]. Zero means DefaultRequestInputSafetyMargin.
	SafetyMargin float64
}

// WithDefaults returns b with an unset safety margin replaced by the default.
// A negative margin is left alone so Validate can name it as the mistake.
func (b RequestInputBudget) WithDefaults() RequestInputBudget {
	if b.SafetyMargin == 0 {
		b.SafetyMargin = DefaultRequestInputSafetyMargin
	}
	return b
}

// Enabled reports whether the guard refuses anything. A disabled budget must
// not cost a single estimate, so callers check this first.
func (b RequestInputBudget) Enabled() bool { return b.ProviderLimitTokens > 0 }

// EffectiveLimitTokens is the internal ceiling requests are held to. It is the
// provider cliff discounted by the safety margin, floored so a fractional
// margin never yields a zero-token allowance.
func (b RequestInputBudget) EffectiveLimitTokens() int {
	if !b.Enabled() {
		return 0
	}
	margin := b.SafetyMargin
	if margin == 0 {
		margin = DefaultRequestInputSafetyMargin
	}
	limit := int(float64(b.ProviderLimitTokens) * margin)
	if limit < 1 {
		limit = 1
	}
	return limit
}

// Validate rejects a budget that would silently mis-behave: a negative limit,
// or a margin outside (0, 1], which would widen rather than narrow the guard.
func (b RequestInputBudget) Validate() error {
	if b.ProviderLimitTokens < 0 {
		return fmt.Errorf("--max-request-input-tokens must be a non-negative integer (0 disables the guard)")
	}
	if b.SafetyMargin != 0 {
		return ValidateRequestInputSafetyMargin(b.SafetyMargin)
	}
	return nil
}

// ValidateRequestInputSafetyMargin accepts a margin within (0, 1]. NaN and
// the infinities are rejected on purpose: they compare false against every
// range check, so a bare `> 1 || < 0` guard would wave them through and turn
// the ceiling into no ceiling at all.
func ValidateRequestInputSafetyMargin(margin float64) error {
	if margin != margin || margin > 1 || margin <= 0 {
		return fmt.Errorf("--request-token-safety-margin must be within (0, 1], got %v", margin)
	}
	return nil
}

// RequestInputEstimator turns a request into the token count it is expected to
// cost. It is an interface so a provider with a real tokenizer can replace the
// default without touching the guard; the guard itself never assumes which
// counter produced the number.
type RequestInputEstimator interface {
	EstimateRequestInputTokens(model string, req ChatRequest) int
}

// TokenizerRequestInputEstimator counts the serialized request with the
// built-in tokenizer. It covers every component that reaches the wire:
// message text, the native replay payload, tool definitions and their JSON
// schemas, tool call identifiers, and per-message framing.
//
// It is an estimate against a non-native tokenizer when the provider is not
// OpenAI. That is why the safety margin exists, and why the guard reports the
// provider's own count next to this one: the delta between them is the only
// honest measurement of how far off the estimate runs.
type TokenizerRequestInputEstimator struct{}

// EstimateRequestInputTokens implements RequestInputEstimator.
func (TokenizerRequestInputEstimator) EstimateRequestInputTokens(model string, req ChatRequest) int {
	total := requestEnvelopeFramingTokens
	for _, msg := range req.Messages {
		total += countMessageInputTokens(model, msg)
	}
	for _, tool := range req.Tools {
		total += countToolDefinitionTokens(model, tool)
	}
	if req.ToolChoice != "" {
		total += CountTokensForModel(req.ToolChoice, model)
	}
	return total
}

// countMessageInputTokens counts one history entry: its visible text, the
// native replay state ExtractText cannot see, and the structural tokens the
// provider spends on the role, the block wrappers and the tool-call
// identifiers that tie a result back to its call.
func countMessageInputTokens(model string, msg Message) int {
	total := CountTokensForModel(msg.ExtractText(), model) + msg.Native.EstimatedTokens()
	total += CountTokensForModel(msg.Role, model) + messageFramingTokens
	if msg.ToolCallID != "" {
		total += CountTokensForModel(msg.ToolCallID, model) + toolResultFramingTokens
	}
	for _, call := range msg.ToolCalls {
		total += CountTokensForModel(call.ID, model)
		total += CountTokensForModel(call.Function.Name, model)
		total += CountTokensForModel(call.Function.Arguments, model)
	}
	return total
}

// countToolDefinitionTokens counts a tool declaration, including its JSON
// schema. The raw provider definition wins when present: it is the exact
// bytes the provider receives, whereas the re-marshaled struct is a
// reconstruction of them.
func countToolDefinitionTokens(model string, tool ToolDef) int {
	if len(tool.Function.RawDefinition) > 0 && json.Valid(tool.Function.RawDefinition) {
		return CountTokensForModel(string(tool.Function.RawDefinition), model)
	}
	encoded, err := json.Marshal(tool)
	if err != nil {
		return CountTokensForModel(tool.Function.Name+tool.Function.Description, model)
	}
	return CountTokensForModel(string(encoded), model)
}

// RequestInputDecision is what the guard did with a request.
type RequestInputDecision string

const (
	// RequestInputAllowed means the estimate fit and the request was sent.
	RequestInputAllowed RequestInputDecision = "allow"
	// RequestInputRefused means the estimate did not fit and nothing was sent.
	RequestInputRefused RequestInputDecision = "refuse"
)

// RequestInputRecord is one observability line for one logical request: what
// was estimated, against which limits, what was decided, and — once the
// provider has answered — what the provider actually counted.
//
// It carries no request content by construction: counts, limits, identities.
// A record is safe to keep for an audit trail even when the raw capture sink
// is off, and it is the only way to tell an under-counting estimator from a
// provider that counted as expected.
type RequestInputRecord struct {
	Timestamp string `json:"timestamp"`

	// SessionID is stamped by the sink, which is bound per session and knows
	// its own ID; the guard leaves it empty.
	SessionID string `json:"session_id,omitempty"`
	// Identity mirrors RequestMeta; absent requests record an unknown task
	// type rather than a guessed one.
	FilePath  string `json:"file_path,omitempty"`
	TaskType  string `json:"task_type,omitempty"`
	RequestNo int    `json:"request_no,omitempty"`
	Model     string `json:"model,omitempty"`

	Decision RequestInputDecision `json:"decision"`

	EstimatedInputTokens int     `json:"estimated_input_tokens"`
	ProviderInputLimit   int     `json:"provider_input_limit,omitempty"`
	EffectiveInputLimit  int     `json:"effective_input_limit,omitempty"`
	SafetyMargin         float64 `json:"safety_margin,omitempty"`
	MessageCount         int     `json:"message_count"`
	ToolDefinitionCount  int     `json:"tool_definition_count"`
	ProviderPromptTokens int     `json:"provider_prompt_tokens,omitempty"`
	// EstimateDeltaTokens is ProviderPromptTokens - EstimatedInputTokens.
	// Positive means the provider counted more than the guard did, which is
	// the direction that crosses a price cliff unnoticed.
	EstimateDeltaTokens int `json:"estimate_delta_tokens,omitempty"`
}

// RequestInputSink receives one record per logical request. It is implemented
// by the raw capture writer so the decision trail lands next to the exchange
// it describes, in the same file and under the same opt-in.
type RequestInputSink interface {
	WriteRequestInput(rec RequestInputRecord)
}

// RequestInputSinkFunc adapts a function to RequestInputSink.
type RequestInputSinkFunc func(RequestInputRecord)

// WriteRequestInput implements RequestInputSink.
func (f RequestInputSinkFunc) WriteRequestInput(rec RequestInputRecord) { f(rec) }

// RequestInputBudgetError is returned instead of sending an over-budget
// request. It is a typed error so callers can react (the main loop retries
// once after compressing) while the message stays readable to a user who
// never learns the type.
type RequestInputBudgetError struct {
	TaskType             string
	Model                string
	EstimatedInputTokens int
	EffectiveLimitTokens int
	ProviderLimitTokens  int
	SafetyMargin         float64
	MessageCount         int
	ToolDefinitionCount  int
}

// Cause returns the stable structured cause name.
func (e *RequestInputBudgetError) Cause() string { return RequestInputBudgetExceededCause }

func (e *RequestInputBudgetError) Error() string {
	return fmt.Sprintf(
		"%s: estimated input %d tokens exceeds the effective per-request limit of %d tokens "+
			"(provider limit %d, safety margin %.2f); no request was sent",
		RequestInputBudgetExceededCause, e.EstimatedInputTokens, e.EffectiveLimitTokens,
		e.ProviderLimitTokens, e.SafetyMargin,
	)
}

// AsRequestInputBudgetError reports whether err is a budget refusal, unwrapping
// the chains callers wrap it in.
func AsRequestInputBudgetError(err error) (*RequestInputBudgetError, bool) {
	var target *RequestInputBudgetError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// RequestInputGuardOption customizes the guard. The zero configuration —
// default estimator, no sink — is the production one; the others exist so a
// test can drive the guard without a tokenizer or a file.
type RequestInputGuardOption func(*requestInputGuardClient)

// WithRequestInputEstimator replaces the estimator.
func WithRequestInputEstimator(e RequestInputEstimator) RequestInputGuardOption {
	return func(g *requestInputGuardClient) {
		if e != nil {
			g.estimator = e
		}
	}
}

// WithRequestInputSink installs the observability sink.
func WithRequestInputSink(s RequestInputSink) RequestInputGuardOption {
	return func(g *requestInputGuardClient) {
		g.sink = s
	}
}

// requestInputGuardClient wraps an LLMClient and enforces the per-request
// input ceiling on the single method every task family goes through.
type requestInputGuardClient struct {
	inner     LLMClient
	budget    RequestInputBudget
	estimator RequestInputEstimator
	sink      RequestInputSink
	// model is the run's resolved model, used when a request leaves Model
	// empty — the same fallback the concrete clients apply.
	model string
}

// NewRequestInputGuardClient wraps inner with the per-request input guard.
//
// A disabled budget returns inner unchanged rather than a pass-through wrapper:
// with no ceiling to enforce there is nothing to add, and an extra layer would
// only obscure the client that actually runs.
func NewRequestInputGuardClient(inner LLMClient, model string, budget RequestInputBudget, opts ...RequestInputGuardOption) LLMClient {
	if inner == nil || !budget.Enabled() {
		return inner
	}
	g := &requestInputGuardClient{
		inner:     inner,
		budget:    budget.WithDefaults(),
		estimator: TokenizerRequestInputEstimator{},
		model:     model,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

// CompletionsWithCtx estimates the full serialized request and refuses it
// before any HTTP call when it does not fit. On the allowed path it forwards
// unchanged, so cache breakpoints, session keys and message order are exactly
// what the caller built.
func (g *requestInputGuardClient) CompletionsWithCtx(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = g.model
	}
	meta, _ := RequestMetaFromContext(ctx)
	taskType := meta.TaskType
	if taskType == "" {
		taskType = TaskTypeUnknown
	}
	estimated := g.estimator.EstimateRequestInputTokens(model, req)
	limit := g.budget.EffectiveLimitTokens()

	rec := RequestInputRecord{
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
		FilePath:             meta.FilePath,
		TaskType:             taskType,
		RequestNo:            meta.RequestNo,
		Model:                model,
		EstimatedInputTokens: estimated,
		ProviderInputLimit:   g.budget.ProviderLimitTokens,
		EffectiveInputLimit:  limit,
		SafetyMargin:         g.budget.SafetyMargin,
		MessageCount:         len(req.Messages),
		ToolDefinitionCount:  len(req.Tools),
	}

	if estimated > limit {
		rec.Decision = RequestInputRefused
		g.emit(rec)
		return nil, &RequestInputBudgetError{
			TaskType:             taskType,
			Model:                model,
			EstimatedInputTokens: estimated,
			EffectiveLimitTokens: limit,
			ProviderLimitTokens:  g.budget.ProviderLimitTokens,
			SafetyMargin:         g.budget.SafetyMargin,
			MessageCount:         len(req.Messages),
			ToolDefinitionCount:  len(req.Tools),
		}
	}

	resp, err := g.inner.CompletionsWithCtx(ctx, req)
	if err == nil && resp != nil && resp.Usage != nil {
		rec.ProviderPromptTokens = int(resp.Usage.PromptTokens)
		rec.EstimateDeltaTokens = rec.ProviderPromptTokens - estimated
	}
	rec.Decision = RequestInputAllowed
	g.emit(rec)
	return resp, err
}

// emit delivers a record to the sink. Observability must never fail a review,
// so a panicking sink is contained rather than propagated into the caller.
func (g *requestInputGuardClient) emit(rec RequestInputRecord) {
	if g.sink == nil {
		return
	}
	defer func() { _ = recover() }()
	g.sink.WriteRequestInput(rec)
}
