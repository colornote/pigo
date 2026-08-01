package llm

import "testing"

// TestAddUsageAnthropicForm verifies the Anthropic-compatible field names
// (input_tokens / output_tokens, cache_read_input_tokens /
// cache_creation_input_tokens) accumulate correctly.
func TestAddUsageAnthropicForm(t *testing.T) {
	total := addUsage(Usage{}, Usage{
		InputTokens:         10,
		OutputTokens:        5,
		CacheReadTokens:     2,
		CacheCreationTokens: 3,
	})
	if total.InputTokens != 10 || total.OutputTokens != 5 {
		t.Errorf("token counts: got %d/%d, want 10/5", total.InputTokens, total.OutputTokens)
	}
	if total.CacheHitTokens != 2 || total.CacheWriteTokens != 3 {
		t.Errorf("cache: got hit=%d write=%d, want hit=2 write=3", total.CacheHitTokens, total.CacheWriteTokens)
	}
	if total.CacheMissTokens != 0 {
		t.Errorf("cache miss: got %d, want 0", total.CacheMissTokens)
	}
}

// TestAddUsageOpenAIForm verifies the OpenAI-compatible field names
// (prompt_tokens / completion_tokens, prompt_cache_*_tokens) are
// normalized into the canonical fields — this was the bug that made the
// footer context usage always show 0 on OpenAI-compatible endpoints.
func TestAddUsageOpenAIForm(t *testing.T) {
	total := addUsage(Usage{}, Usage{
		PromptTokens:          20,
		CompletionTokens:      7,
		PromptCacheHitTokens:  4,
		PromptCacheMissTokens: 16,
	})
	if total.InputTokens != 20 || total.OutputTokens != 7 {
		t.Errorf("token counts: got %d/%d, want 20/7", total.InputTokens, total.OutputTokens)
	}
	if total.CacheHitTokens != 4 || total.CacheMissTokens != 16 {
		t.Errorf("cache: got hit=%d miss=%d, want hit=4 miss=16", total.CacheHitTokens, total.CacheMissTokens)
	}
}

// TestAddUsageAccumulatesAcrossForms verifies accumulation across requests
// regardless of which field convention each response used.
func TestAddUsageAccumulatesAcrossForms(t *testing.T) {
	total := addUsage(Usage{}, Usage{InputTokens: 10, OutputTokens: 5})
	total = addUsage(total, Usage{PromptTokens: 20, CompletionTokens: 7})
	total = addUsage(total, Usage{InputTokens: 1, OutputTokens: 1})
	if total.InputTokens != 31 {
		t.Errorf("InputTokens: got %d, want 31", total.InputTokens)
	}
	if total.OutputTokens != 13 {
		t.Errorf("OutputTokens: got %d, want 13", total.OutputTokens)
	}
}

// TestAddUsageAnthropicAndOpenAICacheTogether verifies cache fields from
// both conventions accumulate into the same canonical totals.
func TestAddUsageAnthropicAndOpenAICacheTogether(t *testing.T) {
	total := addUsage(Usage{}, Usage{CacheReadTokens: 5, CacheCreationTokens: 2})
	total = addUsage(total, Usage{PromptCacheHitTokens: 3, PromptCacheWriteTokens: 4})
	if total.CacheHitTokens != 8 {
		t.Errorf("CacheHitTokens: got %d, want 8", total.CacheHitTokens)
	}
	if total.CacheWriteTokens != 6 {
		t.Errorf("CacheWriteTokens: got %d, want 6", total.CacheWriteTokens)
	}
}
