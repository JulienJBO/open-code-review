// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// countingClient records every request that reached the inner client. Its call
// count is the HTTP-count proxy the refusal tests assert on: the guard sits in
// front of the only method every task family uses, so "inner never called"
// is the strongest available statement that nothing was sent.
type countingClient struct {
	requests []ChatRequest
	usage    UsageInfo
}

func (c *countingClient) CompletionsWithCtx(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	c.requests = append(c.requests, req)
	usage := c.usage
	return &ChatResponse{Model: "test", Usage: &usage}, nil
}

// fixedEstimator returns a constant so a test can place a request exactly
// above or below a limit without shaping a conversation to hit a token count.
type fixedEstimator int

func (e fixedEstimator) EstimateRequestInputTokens(string, ChatRequest) int { return int(e) }

func messagesOf(n int) []Message {
	msgs := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, NewTextMessage("user", "hello"))
	}
	return msgs
}

func TestRequestInputBudget_DisabledBudgetIsInert(t *testing.T) {
	inner := &countingClient{}
	budget := RequestInputBudget{}
	if budget.Enabled() {
		t.Fatal("a zero budget must be disabled")
	}
	if got := budget.EffectiveLimitTokens(); got != 0 {
		t.Errorf("EffectiveLimitTokens() = %d, want 0 for a disabled budget", got)
	}
	// An inert budget must not even wrap: callers compare the result to the
	// client they passed in.
	if got := NewRequestInputGuardClient(inner, "m", budget); got != LLMClient(inner) {
		t.Error("a disabled budget must return the inner client unchanged")
	}
	if NewRequestInputGuardClient(nil, "m", budget) != nil {
		t.Error("a nil inner client must stay nil")
	}
}

func TestRequestInputBudget_EffectiveLimitAppliesMargin(t *testing.T) {
	cases := []struct {
		name   string
		budget RequestInputBudget
		want   int
	}{
		{"default margin", RequestInputBudget{ProviderLimitTokens: 32000}, 28800},
		{"explicit margin", RequestInputBudget{ProviderLimitTokens: 32000, SafetyMargin: 0.5}, 16000},
		{"full margin", RequestInputBudget{ProviderLimitTokens: 1000, SafetyMargin: 1}, 1000},
		// A fractional margin must not floor to "send nothing": the guard
		// would refuse every request, including a one-token probe.
		{"tiny margin floors at one token", RequestInputBudget{ProviderLimitTokens: 1, SafetyMargin: 0.1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.budget.EffectiveLimitTokens(); got != tc.want {
				t.Errorf("EffectiveLimitTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequestInputBudget_ValidateRejectsWideningMargins(t *testing.T) {
	if err := (RequestInputBudget{ProviderLimitTokens: -1}).Validate(); err == nil {
		t.Error("a negative provider limit must be rejected")
	}
	for _, margin := range []float64{-0.1, 1.5, math.NaN(), math.Inf(1)} {
		if err := (RequestInputBudget{ProviderLimitTokens: 100, SafetyMargin: margin}).Validate(); err == nil {
			t.Errorf("margin %v must be rejected", margin)
		}
		if err := ValidateRequestInputSafetyMargin(margin); err == nil {
			t.Errorf("ValidateRequestInputSafetyMargin(%v) must fail", margin)
		}
	}
	if err := (RequestInputBudget{ProviderLimitTokens: 100, SafetyMargin: 0.9}).Validate(); err != nil {
		t.Errorf("a sane budget must validate, got %v", err)
	}
}

func TestRequestInputGuard_AllowsUnderLimitAndForwardsRequestUnchanged(t *testing.T) {
	inner := &countingClient{usage: UsageInfo{PromptTokens: 900}}
	var records []RequestInputRecord
	guard := NewRequestInputGuardClient(inner, "test-model",
		RequestInputBudget{ProviderLimitTokens: 1000, SafetyMargin: 0.5},
		WithRequestInputEstimator(fixedEstimator(400)),
		WithRequestInputSink(RequestInputSinkFunc(func(r RequestInputRecord) { records = append(records, r) })))

	req := ChatRequest{
		Messages:   messagesOf(2),
		Tools:      []ToolDef{{Type: "function"}},
		ToolChoice: "auto",
		SessionID:  "sess-1",
	}
	if _, err := guard.CompletionsWithCtx(context.Background(), req); err != nil {
		t.Fatalf("a request under the limit must be sent, got %v", err)
	}
	if len(inner.requests) != 1 {
		t.Fatalf("inner calls = %d, want 1", len(inner.requests))
	}
	// Cache-relevant fields must reach the provider exactly as built: the
	// guard is read-only on the request.
	if got := inner.requests[0]; got.ToolChoice != "auto" || got.SessionID != "sess-1" || len(got.Tools) != 1 || len(got.Messages) != 2 {
		t.Errorf("the guard must forward the request untouched, got %+v", got)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Decision != RequestInputAllowed {
		t.Errorf("decision = %q, want %q", rec.Decision, RequestInputAllowed)
	}
	if rec.ProviderPromptTokens != 900 || rec.EstimateDeltaTokens != 500 {
		t.Errorf("delta record = (%d, %d), want (900, 500)", rec.ProviderPromptTokens, rec.EstimateDeltaTokens)
	}
	if rec.EffectiveInputLimit != 500 || rec.ProviderInputLimit != 1000 || rec.SafetyMargin != 0.5 {
		t.Errorf("limits = (%d, %d, %v), want (500, 1000, 0.5)", rec.EffectiveInputLimit, rec.ProviderInputLimit, rec.SafetyMargin)
	}
}

func TestRequestInputGuard_RefusesWithoutCallingInner(t *testing.T) {
	inner := &countingClient{}
	var records []RequestInputRecord
	guard := NewRequestInputGuardClient(inner, "test-model",
		RequestInputBudget{ProviderLimitTokens: 1000, SafetyMargin: 0.9},
		WithRequestInputEstimator(fixedEstimator(901)),
		WithRequestInputSink(RequestInputSinkFunc(func(r RequestInputRecord) { records = append(records, r) })))

	_, err := guard.CompletionsWithCtx(context.Background(), ChatRequest{Messages: messagesOf(1)})
	budgetErr, ok := AsRequestInputBudgetError(err)
	if !ok {
		t.Fatalf("err = %v, want a *RequestInputBudgetError", err)
	}
	if len(inner.requests) != 0 {
		t.Fatalf("inner calls = %d, want 0: a refusal must emit no request", len(inner.requests))
	}
	if budgetErr.Cause() != RequestInputBudgetExceededCause {
		t.Errorf("cause = %q, want %q", budgetErr.Cause(), RequestInputBudgetExceededCause)
	}
	if budgetErr.EstimatedInputTokens != 901 || budgetErr.EffectiveLimitTokens != 900 {
		t.Errorf("error = (%d, %d), want (901, 900)", budgetErr.EstimatedInputTokens, budgetErr.EffectiveLimitTokens)
	}
	if !strings.Contains(budgetErr.Error(), RequestInputBudgetExceededCause) {
		t.Errorf("message %q must name the cause", budgetErr.Error())
	}
	if len(records) != 1 || records[0].Decision != RequestInputRefused {
		t.Fatalf("a refusal must be recorded as such, got %+v", records)
	}
	// The refusal must not report a provider count: nothing was sent.
	if records[0].ProviderPromptTokens != 0 {
		t.Errorf("a refused request has no provider prompt tokens, got %d", records[0].ProviderPromptTokens)
	}
}

// TestRequestInputGuard_RefusesEveryTaskFamily is the ticket's central
// requirement: a family that a future change forgets to wire cannot reach the
// provider. The guard keys off the request itself, so the task type is only
// observed data — the table documents that each family is covered rather than
// proving per-family code exists.
func TestRequestInputGuard_RefusesEveryTaskFamily(t *testing.T) {
	families := []string{
		"main_task", "plan_task", "review_filter_task",
		"memory_compression_task", "re_location_task", "grouping_task",
	}
	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			inner := &countingClient{}
			var records []RequestInputRecord
			guard := NewRequestInputGuardClient(inner, "test-model",
				RequestInputBudget{ProviderLimitTokens: 1000},
				WithRequestInputEstimator(fixedEstimator(5000)),
				WithRequestInputSink(RequestInputSinkFunc(func(r RequestInputRecord) { records = append(records, r) })))
			ctx := WithRequestMeta(context.Background(), RequestMeta{
				Model: "test-model", FilePath: "a.go", TaskType: family, RequestNo: 1,
			})
			if _, err := guard.CompletionsWithCtx(ctx, ChatRequest{Messages: messagesOf(1)}); err == nil {
				t.Fatal("an over-budget request must be refused")
			}
			if len(inner.requests) != 0 {
				t.Fatalf("inner calls = %d, want 0", len(inner.requests))
			}
			if len(records) != 1 || records[0].TaskType != family {
				t.Fatalf("record = %+v, want task type %q", records, family)
			}
		})
	}
}

func TestRequestInputGuard_UnknownTaskTypeIsNotGuessed(t *testing.T) {
	inner := &countingClient{}
	var records []RequestInputRecord
	guard := NewRequestInputGuardClient(inner, "m",
		RequestInputBudget{ProviderLimitTokens: 100},
		WithRequestInputEstimator(fixedEstimator(1)),
		WithRequestInputSink(RequestInputSinkFunc(func(r RequestInputRecord) { records = append(records, r) })))
	if _, err := guard.CompletionsWithCtx(context.Background(), ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if records[0].TaskType != TaskTypeUnknown {
		t.Errorf("task type = %q, want %q", records[0].TaskType, TaskTypeUnknown)
	}
}

func TestRequestInputGuard_InnerErrorIsForwardedAndStillRecorded(t *testing.T) {
	inner := &countingClient{}
	var records []RequestInputRecord
	guard := NewRequestInputGuardClient(inner, "m",
		RequestInputBudget{ProviderLimitTokens: 1000},
		WithRequestInputEstimator(fixedEstimator(1)),
		WithRequestInputSink(RequestInputSinkFunc(func(r RequestInputRecord) { records = append(records, r) })))
	_, err := guard.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].Decision != RequestInputAllowed {
		t.Fatalf("records = %+v, want one allow", records)
	}
}

// A panicking sink must not take a review down with it: observability is
// secondary to the run it observes.
func TestRequestInputGuard_SinkFailureIsContained(t *testing.T) {
	guard := NewRequestInputGuardClient(&countingClient{}, "m",
		RequestInputBudget{ProviderLimitTokens: 1000},
		WithRequestInputEstimator(fixedEstimator(1)),
		WithRequestInputSink(RequestInputSinkFunc(func(RequestInputRecord) { panic("sink exploded") })))
	if _, err := guard.CompletionsWithCtx(context.Background(), ChatRequest{}); err != nil {
		t.Fatalf("a panicking sink must not fail the request, got %v", err)
	}
}

func TestTokenizerRequestInputEstimator_CountsToolsAndSchemas(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "absolute repository path to read"},
			"start_line": map[string]any{"type": "integer", "description": "first line to include"},
			"end_line":   map[string]any{"type": "integer", "description": "last line to include"},
		},
		"required": []any{"path"},
	}
	withoutTools := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: messagesOf(1),
	})
	withTools := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: messagesOf(1),
		Tools: []ToolDef{{Type: "function", Function: FunctionDef{
			Name: "file_read", Description: "Read a file", Parameters: schema,
		}}},
	})
	// The incident this ticket answers is a conversation that fits the prompt
	// budget while its tool declarations do not. The estimator must see the
	// difference, otherwise the guard inherits the same blind spot.
	if withTools <= withoutTools {
		t.Errorf("tool definitions must add to the estimate: %d vs %d without", withTools, withoutTools)
	}
}

func TestTokenizerRequestInputEstimator_CountsToolCallsAndResults(t *testing.T) {
	base := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: []Message{NewTextMessage("user", "read it")},
	})
	withHistory := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: []Message{
			NewTextMessage("user", "read it"),
			NewToolCallMessage("", []ToolCall{{
				ID: "call_1", Type: "function",
				Function: FunctionCall{Name: "file_read", Arguments: `{"path":"main.go"}`},
			}}, NativeTurn{}, ""),
			NewToolResultMessage("call_1", "package main\nfunc main() {}\n"),
		},
	})
	if withHistory <= base {
		t.Errorf("tool call/result history must add to the estimate: %d vs %d without", withHistory, base)
	}
}

// A raw provider definition is the exact bytes the provider receives, so it
// must win over the re-marshaled struct.
func TestTokenizerRequestInputEstimator_PrefersRawToolDefinition(t *testing.T) {
	raw := json.RawMessage(`{"type":"function","function":{"name":"x","description":"a very long description that the struct path would also carry","parameters":{"type":"object"}}}`)
	tool := ToolDef{Type: "function", Function: FunctionDef{
		Name: "x", Description: "a very long description that the struct path would also carry",
		Parameters: map[string]any{"type": "object"}, RawDefinition: raw,
	}}
	got := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{Tools: []ToolDef{tool}})
	if got <= requestEnvelopeFramingTokens {
		t.Errorf("estimate = %d, want the raw definition counted", got)
	}
}

func TestTokenizerRequestInputEstimator_CountsNativeReplayState(t *testing.T) {
	// Native replay payloads (thinking blocks, tool-use) are invisible to
	// ExtractText but are re-sent verbatim, so they must be counted.
	plain := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: []Message{NewTextMessage("assistant", "")},
	})
	withNative := TokenizerRequestInputEstimator{}.EstimateRequestInputTokens("m", ChatRequest{
		Messages: []Message{{Role: "assistant", Content: "", Native: NativeTurn{
			Family: "anthropic-messages", Payload: ReasoningPayload(strings.Repeat("thinking ", 200)),
		}}},
	})
	if withNative <= plain {
		t.Errorf("native replay state must add to the estimate: %d vs %d without", withNative, plain)
	}
}

func TestTokenizerRequestInputEstimator_EmptyRequestStillCostsFraming(t *testing.T) {
	if got := (TokenizerRequestInputEstimator{}).EstimateRequestInputTokens("m", ChatRequest{}); got < requestEnvelopeFramingTokens {
		t.Errorf("estimate = %d, want at least the envelope framing (%d)", got, requestEnvelopeFramingTokens)
	}
}

func TestRequestInputBudgetError_UnwrapsThroughWrapping(t *testing.T) {
	inner := &RequestInputBudgetError{EstimatedInputTokens: 10, EffectiveLimitTokens: 5}
	wrapped := fmt.Errorf("LLM completion error: %w", inner)
	if _, ok := AsRequestInputBudgetError(wrapped); !ok {
		t.Fatal("a wrapped refusal must still be recognizable")
	}
	if _, ok := AsRequestInputBudgetError(context.Canceled); ok {
		t.Fatal("an unrelated error must not be reported as a refusal")
	}
}
