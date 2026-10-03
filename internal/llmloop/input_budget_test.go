// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"context"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/config/template"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/session"
)

// metaDeps wires the request identity a real run attaches, so the guard's
// task_type reflects the family under test instead of its unknown placeholder.
func metaDeps(deps Deps) Deps {
	deps.NewRequestMeta = func(filePath string, taskType session.TaskType, requestNo int) llm.RequestMeta {
		return llm.RequestMeta{
			Provider:  "test",
			Model:     deps.Model,
			FilePath:  filePath,
			TaskType:  string(taskType),
			RequestNo: requestNo,
		}
	}
	return deps
}

// guardedDeps wraps client in the real per-request input guard, so these tests
// exercise the composition the command wires rather than a stand-in.
func guardedDeps(client llm.LLMClient, limit int, maxTokens int) Deps {
	deps := newTestDeps(llm.NewRequestInputGuardClient(client, "fake",
		llm.RequestInputBudget{ProviderLimitTokens: limit, SafetyMargin: 0.9}))
	deps.Template = template.Template{MaxTokens: maxTokens, MaxToolRequestTimes: 3}
	return metaDeps(deps)
}

// heavyConversation builds a tool-heavy history: the shape the ticket calls
// out as at risk, where results accumulate faster than compression reclaims.
func heavyConversation(turns int) []llm.Message {
	msgs := []llm.Message{llm.NewTextMessage("user", strings.Repeat("review this diff carefully. ", 40))}
	for i := 0; i < turns; i++ {
		msgs = append(msgs,
			llm.NewToolCallMessage("", []llm.ToolCall{{
				ID: "call_x", Type: "function",
				Function: llm.FunctionCall{Name: "file_read", Arguments: `{"path":"main.go"}`},
			}}, llm.NativeTurn{}, ""),
			llm.NewToolResultMessage("call_x", strings.Repeat("package main\n", 800)),
		)
	}
	return msgs
}

// A conversation that fits is sent exactly as it was before the guard existed.
func TestRunMainTask_UnderInputBudgetSendsRequest(t *testing.T) {
	client := &fakeClient{responses: []*llm.ChatResponse{taskDoneResponseWithArguments(`{}`)}}
	deps := guardedDeps(client, 1_000_000, 1_000_000)

	if _, _, err := NewRunner(deps).RunMainTask(context.Background(), heavyConversation(1), "a.go"); err != nil {
		t.Fatalf("a request under the ceiling must be sent, got %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(client.requests))
	}
}

// The guard refuses rather than sends, and main_task gets one compression
// attempt plus one retry. What matters is the ordering: the conversation that
// reaches the provider is the reduced one, not the refused one.
//
// The compression template is deliberately empty here: that path reduces the
// conversation without an LLM round-trip, which keeps the assertion about
// ordering rather than about summary quality. The compression call itself is
// covered by TestRunMainTask_MemoryCompressionRequestIsGuardedToo.
func TestRunMainTask_OverInputBudgetCompressesBeforeSending(t *testing.T) {
	client := &fakeClient{responses: []*llm.ChatResponse{taskDoneResponseWithArguments(`{}`)}}
	deps := guardedDeps(client, 1_000, 1_000_000)

	before := CountMessagesTokens(heavyConversation(6))
	if _, _, err := NewRunner(deps).RunMainTask(context.Background(), heavyConversation(6), "a.go"); err != nil {
		t.Fatalf("compression should have made the request fit, got %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("provider requests = %d, want exactly 1 (the compressed retry)", len(client.requests))
	}
	if after := CountMessagesTokens(client.requests[0].Messages); after >= before {
		t.Errorf("the sent conversation must be smaller: %d tokens after, %d before", after, before)
	}
}

// When compression cannot reduce the conversation, the refusal stands. The
// typed error must survive so the caller reports a real cause instead of a
// provider failure that never happened.
func TestRunMainTask_OverInputBudgetRefusesWhenCompressionCannotHelp(t *testing.T) {
	client := &fakeClient{responses: []*llm.ChatResponse{taskDoneResponseWithArguments(`{}`)}}
	deps := guardedDeps(client, 5, 1_000_000)

	_, _, err := NewRunner(deps).RunMainTask(context.Background(), heavyConversation(0), "a.go")
	if err == nil {
		t.Fatal("an unreducible over-budget conversation must not be sent")
	}
	budgetErr, ok := llm.AsRequestInputBudgetError(err)
	if !ok {
		t.Fatalf("err = %v, want a request-input refusal", err)
	}
	if budgetErr.TaskType != string(session.MainTask) {
		t.Errorf("task type = %q, want %q", budgetErr.TaskType, session.MainTask)
	}
	if len(client.requests) != 0 {
		t.Fatalf("provider requests = %d, want 0", len(client.requests))
	}
}

// The retry must not be a silent second paid round-trip for the same bytes, and
// the session must not attribute an answer to a conversation never sent.
func TestRunMainTask_InputBudgetRetryRecordsTheConversationActuallySent(t *testing.T) {
	client := &fakeClient{responses: []*llm.ChatResponse{taskDoneResponseWithArguments(`{}`)}}
	deps := guardedDeps(client, 1_000, 1_000_000)

	if _, _, err := NewRunner(deps).RunMainTask(context.Background(), heavyConversation(6), "a.go"); err != nil {
		t.Fatal(err)
	}
	records := deps.Session.GetOrCreateFileSession("a.go").TaskRecords[session.MainTask]
	if len(records) < 2 {
		t.Fatalf("main_task records = %d, want the refused snapshot plus the sent one", len(records))
	}
	sent := CountMessagesTokens(client.requests[0].Messages)
	if got := CountMessagesTokens(records[len(records)-1].RequestMessages); got != sent {
		t.Errorf("final record holds %d tokens, want the %d actually sent", got, sent)
	}
}

// memory_compression_task goes through the same guard: when the guard refuses
// it, compression fails and the main-task refusal stands, rather than the run
// quietly proceeding on an over-budget conversation.
func TestRunMainTask_MemoryCompressionRequestIsGuardedToo(t *testing.T) {
	client := &fakeClient{}
	deps := guardedDeps(client, 1_000, 3_000)
	deps.Template.MemoryCompressionTask.Messages = []template.ChatMessage{
		{Role: "user", Content: "summarize the following review so far:\n{{context}}"},
	}

	_, _, err := NewRunner(deps).RunMainTask(context.Background(), heavyConversation(6), "a.go")
	if err == nil {
		t.Fatal("expected the run to end on a refusal")
	}
	if _, ok := llm.AsRequestInputBudgetError(err); !ok {
		t.Fatalf("err = %v, want a request-input refusal", err)
	}
	// Both the main request and the compression request were refused: nothing
	// reached the provider, which is the whole point of the guard.
	if len(client.requests) != 0 {
		t.Fatalf("provider requests = %d, want 0", len(client.requests))
	}
}
