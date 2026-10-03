// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/llm"
)

// The per-request input ceiling is a third notion. These tests pin that it
// parses on its own terms and that setting it leaves the two existing ceilings
// alone — otherwise it would be an alias, and a run configured with it would
// silently change what --max-tokens means.
func TestParseReviewFlags_RequestInputBudgetParsed(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--max-request-input-tokens", "32000", "--request-token-safety-margin", "0.85"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	budget, err := requestInputBudget(opts.maxRequestInputTokens, opts.requestTokenSafetyMargin)
	if err != nil {
		t.Fatalf("budget rejected after a successful parse: %v", err)
	}
	if budget.ProviderLimitTokens != 32000 || budget.SafetyMargin != 0.85 {
		t.Errorf("budget = %+v, want limit 32000 at margin 0.85", budget)
	}
	if got := budget.EffectiveLimitTokens(); got != 27200 {
		t.Errorf("effective limit = %d, want 27200", got)
	}
}

func TestParseReviewFlags_RequestInputBudgetDefaultsToDisabledWithDefaultMargin(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--from", "main", "--to", "dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	budget, err := requestInputBudget(opts.maxRequestInputTokens, opts.requestTokenSafetyMargin)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Enabled() {
		t.Error("no flag must mean no per-request ceiling, so existing runs are untouched")
	}
	if budget.SafetyMargin != llm.DefaultRequestInputSafetyMargin {
		t.Errorf("margin = %v, want the documented default %v", budget.SafetyMargin, llm.DefaultRequestInputSafetyMargin)
	}
}

// 0 is the documented "unset" form for the margin flag, so it resolves to the
// default rather than to a guard that refuses everything.
func TestParseReviewFlags_RequestInputBudgetZeroMarginMeansDefault(t *testing.T) {
	opts, err := parseReviewFlags([]string{"--max-request-input-tokens", "32000", "--request-token-safety-margin", "0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	budget, err := requestInputBudget(opts.maxRequestInputTokens, opts.requestTokenSafetyMargin)
	if err != nil {
		t.Fatal(err)
	}
	if budget.SafetyMargin != llm.DefaultRequestInputSafetyMargin {
		t.Errorf("margin = %v, want the default %v", budget.SafetyMargin, llm.DefaultRequestInputSafetyMargin)
	}
}

func TestParseReviewFlags_RequestInputBudgetDoesNotDisturbExistingCeilings(t *testing.T) {
	opts, err := parseReviewFlags([]string{
		"--max-tokens", "26000",
		"--max-tokens-budget", "120000",
		"--max-request-input-tokens", "32000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.maxTokens != 26000 {
		t.Errorf("maxTokens = %d, want 26000: the per-group ceiling must keep its own semantics", opts.maxTokens)
	}
	if opts.maxTokensBudget != 120000 {
		t.Errorf("maxTokensBudget = %d, want 120000: the run budget must keep its own semantics", opts.maxTokensBudget)
	}
}

func TestParseReviewFlags_RequestInputBudgetRejected(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"negative ceiling", []string{"--max-request-input-tokens", "-1"}, "--max-request-input-tokens"},
		{"margin above one", []string{"--max-request-input-tokens", "32000", "--request-token-safety-margin", "1.5"}, "--request-token-safety-margin"},
		{"negative margin", []string{"--max-request-input-tokens", "32000", "--request-token-safety-margin", "-0.5"}, "--request-token-safety-margin"},
		{"margin without a ceiling", []string{"--request-token-safety-margin", "0.9"}, "--max-request-input-tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseReviewFlags(tc.args)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestParseScanFlags_RequestInputBudgetParsed(t *testing.T) {
	opts, err := parseScanFlags([]string{"--max-request-input-tokens", "32000", "--request-token-safety-margin", "0.9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	budget, err := requestInputBudget(opts.maxRequestInputTokens, opts.requestTokenSafetyMargin)
	if err != nil {
		t.Fatal(err)
	}
	if budget.ProviderLimitTokens != 32000 || budget.SafetyMargin != 0.9 {
		t.Errorf("budget = %+v, want limit 32000 at margin 0.9", budget)
	}
}

func TestParseScanFlags_RequestInputBudgetRejected(t *testing.T) {
	if _, err := parseScanFlags([]string{"--max-request-input-tokens", "-100"}); err == nil {
		t.Fatal("expected an error for a negative per-request ceiling")
	}
}
