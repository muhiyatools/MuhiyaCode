package gateway

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestUsageParsingGoldenProviderShapes(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		want  contract.Usage
	}{
		{
			name:  "deepseek top-level values",
			usage: `{"prompt_tokens":120,"completion_tokens":11,"total_tokens":131,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":20}`,
			want: contract.Usage{
				PromptTokens: 120, CompletionTokens: 11, TotalTokens: 131, CachedTokens: 100,
				CacheReadTokens: intPointer(100), CacheMissTokens: intPointer(20),
				UncachedInputTokens: intPointer(20), CacheUsageSchema: "deepseek.prompt_cache", CacheUsageDerivation: "direct-complementary",
				PromptTokensAvailable: true, CompletionTokensAvailable: true,
			},
		},
		{
			name:  "openai nested cached tokens",
			usage: `{"prompt_tokens":120,"completion_tokens":11,"total_tokens":131,"prompt_tokens_details":{"cached_tokens":100}}`,
			want: contract.Usage{
				PromptTokens: 120, CompletionTokens: 11, TotalTokens: 131, CachedTokens: 100,
				CacheReadTokens: intPointer(100), CacheMissTokens: intPointer(20), MissDerived: true,
				UncachedInputTokens: intPointer(20), CacheUsageSchema: "openai.prompt_tokens_details", CacheUsageDerivation: "uncached=prompt-cache_read",
				PromptTokensAvailable: true, CompletionTokensAvailable: true,
			},
		},
		{
			name:  "deepseek zero wins over larger nested value",
			usage: `{"prompt_tokens":100,"completion_tokens":0,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":100,"prompt_tokens_details":{"cached_tokens":99}}`,
			want: contract.Usage{
				PromptTokens: 100, CachedTokens: 0,
				CacheReadTokens: intPointer(0), CacheMissTokens: intPointer(100),
				UncachedInputTokens: intPointer(100), CacheUsageSchema: "deepseek.prompt_cache", CacheUsageDerivation: "direct-complementary",
				PromptTokensAvailable: true, CompletionTokensAvailable: true,
			},
		},
		{
			name:  "precedence applies independently to read and miss",
			usage: `{"prompt_tokens":100,"prompt_cache_miss_tokens":7,"prompt_tokens_details":{"cached_tokens":90}}`,
			want: contract.Usage{
				PromptTokens: 100, CachedTokens: 90,
				CacheReadTokens: intPointer(90), CacheMissTokens: intPointer(7),
				UncachedInputTokens: intPointer(7), CacheUsageSchema: "mixed.cache-members", CacheUsageDerivation: "direct-members",
				PromptTokensAvailable: true,
			},
		},
		{
			name:  "top-level read with nested fallback derives selected read",
			usage: `{"prompt_tokens":100,"prompt_cache_hit_tokens":80,"prompt_tokens_details":{"cached_tokens":70}}`,
			want: contract.Usage{
				PromptTokens: 100, CachedTokens: 80,
				CacheReadTokens: intPointer(80), CacheMissTokens: intPointer(20), MissDerived: true,
				UncachedInputTokens: intPointer(20), CacheUsageSchema: "mixed.deepseek-read+derived", CacheUsageDerivation: "uncached=prompt-cache_read",
				PromptTokensAvailable: true,
			},
		},
		{
			name:  "null top-level fields permit nested fallback",
			usage: `{"prompt_tokens":100,"prompt_cache_hit_tokens":null,"prompt_cache_miss_tokens":null,"prompt_tokens_details":{"cached_tokens":80}}`,
			want: contract.Usage{
				PromptTokens: 100, CachedTokens: 80,
				CacheReadTokens: intPointer(80), CacheMissTokens: intPointer(20), MissDerived: true,
				UncachedInputTokens: intPointer(20), CacheUsageSchema: "openai.prompt_tokens_details", CacheUsageDerivation: "uncached=prompt-cache_read",
				PromptTokensAvailable: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseUsageGolden(t, test.usage)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("usage mismatch\n got: %#v\nwant: %#v", got, test.want)
			}
		})
	}
}

func TestUsageParsingZeroAndNullRemainDistinct(t *testing.T) {
	zero := parseUsageGolden(t, `{"prompt_tokens":0,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":0}}`)
	if zero.CacheReadTokens == nil || *zero.CacheReadTokens != 0 {
		t.Fatalf("reported cache-read zero became unavailable: %#v", zero)
	}
	if zero.CacheMissTokens == nil || *zero.CacheMissTokens != 0 || !zero.MissDerived {
		t.Fatalf("derived cache-miss zero was not retained: %#v", zero)
	}
	if !zero.PromptTokensAvailable || !zero.CompletionTokensAvailable {
		t.Fatalf("reported prompt/completion zero became unavailable: %#v", zero)
	}

	absent := parseUsageGolden(t, `{"prompt_tokens":0,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":null}}`)
	if absent.CacheReadTokens != nil || absent.CacheMissTokens != nil || absent.MissDerived {
		t.Fatalf("null cache fields became reported values: %#v", absent)
	}
	if !absent.PromptTokensAvailable || !absent.CompletionTokensAvailable {
		t.Fatalf("independent prompt/completion values were lost: %#v", absent)
	}

	missing := parseUsageGolden(t, `{"prompt_tokens":0,"completion_tokens":0}`)
	if missing.CacheReadTokens != nil || missing.CacheMissTokens != nil {
		t.Fatalf("missing cache fields became reported values: %#v", missing)
	}
}

func TestUsageParsingContradictionsAreFlaggedNotCorrected(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		read int
		miss int
	}{
		{
			name: "reported split disagrees with prompt total",
			raw:  `{"prompt_tokens":100,"prompt_cache_hit_tokens":90,"prompt_cache_miss_tokens":20}`,
			read: 90,
			miss: 20,
		},
		{
			name: "nested cached value exceeds prompt total",
			raw:  `{"prompt_tokens":10,"prompt_tokens_details":{"cached_tokens":12}}`,
			read: 12,
			miss: -2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseUsageGolden(t, test.raw)
			if !got.Contradictory {
				t.Fatalf("contradictory payload was not flagged: %#v", got)
			}
			if got.CacheReadTokens == nil || *got.CacheReadTokens != test.read || got.CachedTokens != test.read {
				t.Fatalf("cache read was corrected instead of stored verbatim: %#v", got)
			}
			if got.CacheMissTokens == nil || *got.CacheMissTokens != test.miss {
				t.Fatalf("cache miss was corrected instead of retained: %#v", got)
			}
		})
	}
}

func TestUsageParsingMalformedPayloadIsNonFatalAndDiagnostic(t *testing.T) {
	tests := []struct {
		name              string
		usage             string
		wantPrompt        int
		wantPromptValid   bool
		wantComplete      int
		wantCompleteValid bool
		diagnosticParts   []string
	}{
		{
			name:            "usage is not an object",
			usage:           `[]`,
			diagnosticParts: []string{"usage must be an object"},
		},
		{
			name:  "token fields have invalid types",
			usage: `{"prompt_tokens":"100","completion_tokens":3.5,"prompt_cache_hit_tokens":"80","prompt_cache_miss_tokens":{}}`,
			diagnosticParts: []string{
				"usage.prompt_tokens must be an integer",
				"usage.completion_tokens must be an integer",
				"usage.prompt_cache_hit_tokens must be an integer",
				"usage.prompt_cache_miss_tokens must be an integer",
			},
		},
		{
			name:              "malformed cache details do not discard valid token totals",
			usage:             `{"prompt_tokens":42,"completion_tokens":5,"prompt_tokens_details":"bad"}`,
			wantPrompt:        42,
			wantPromptValid:   true,
			wantComplete:      5,
			wantCompleteValid: true,
			diagnosticParts:   []string{"usage.prompt_tokens_details must be an object"},
		},
		{
			name:            "malformed higher-priority read blocks nested fallback",
			usage:           `{"prompt_tokens":100,"prompt_cache_hit_tokens":"bad","prompt_tokens_details":{"cached_tokens":80}}`,
			wantPrompt:      100,
			wantPromptValid: true,
			diagnosticParts: []string{"usage.prompt_cache_hit_tokens must be an integer"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseUsageGolden(t, test.usage)
			if got.PromptTokens != test.wantPrompt || got.PromptTokensAvailable != test.wantPromptValid {
				t.Fatalf("prompt tokens did not parse independently: %#v", got)
			}
			if got.CompletionTokens != test.wantComplete || got.CompletionTokensAvailable != test.wantCompleteValid {
				t.Fatalf("completion tokens did not parse independently: %#v", got)
			}
			if got.CacheReadTokens != nil || got.CacheMissTokens != nil || got.MissDerived || got.CachedTokens != 0 {
				t.Fatalf("malformed cache usage produced values: %#v", got)
			}
			if got.Diagnostic == "" {
				t.Fatalf("malformed usage did not produce a diagnostic: %#v", got)
			}
			for _, part := range test.diagnosticParts {
				if !strings.Contains(got.Diagnostic, part) {
					t.Fatalf("diagnostic %q does not contain %q", got.Diagnostic, part)
				}
			}
		})
	}
}

func TestUsageParsingPreservesLargeIntegersAcrossSSEJSON(t *testing.T) {
	const read = 9007199254740993
	const miss = 7
	const prompt = read + miss
	got := parseUsageGolden(t, fmt.Sprintf(`{"prompt_tokens":%d,"prompt_cache_hit_tokens":%d,"prompt_cache_miss_tokens":%d}`, prompt, read, miss))
	if got.PromptTokens != prompt || got.CacheReadTokens == nil || *got.CacheReadTokens != read || got.CacheMissTokens == nil || *got.CacheMissTokens != miss {
		t.Fatalf("large provider integers lost precision: %#v", got)
	}
	if got.Contradictory {
		t.Fatalf("self-consistent large values were flagged contradictory: %#v", got)
	}
}

func TestUsageParsingMergesPartialUsageWithProviderPrecedence(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[],"usage":{"prompt_tokens_details":{"cached_tokens":80}}}`,
		`data: {"choices":[],"usage":{"prompt_tokens":100}}`,
		`data: {"choices":[],"usage":{"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":100}}`,
		`data: [DONE]`,
	}, "\n")
	result, err := decodeSSEStream(stream, ModelProfile{})
	if err != nil {
		t.Fatalf("partial usage stream failed: %v", err)
	}
	got := result.Usage
	if got.CacheReadTokens == nil || *got.CacheReadTokens != 0 || got.CacheMissTokens == nil || *got.CacheMissTokens != 100 || got.MissDerived {
		t.Fatalf("later top-level fields did not override nested values: %#v", got)
	}
}

func TestRawUsagePayloadPreservesProviderObject(t *testing.T) {
	raw, ok := rawUsageFromSSELine(`data: {"choices":[],"usage":{"prompt_cache_hit_tokens":7,"vendor_extension":{"source":"raw"}}}`)
	if !ok {
		t.Fatal("usage payload was not detected")
	}
	want := `{"prompt_cache_hit_tokens":7,"vendor_extension":{"source":"raw"}}`
	if string(raw) != want {
		t.Fatalf("raw usage = %s, want %s", raw, want)
	}
	if _, ok := rawUsageFromSSELine(`data: {"choices":[]}`); ok {
		t.Fatal("payload without usage was reported")
	}
}

func TestMiniMaxUsageThresholdAndReasoningDetails(t *testing.T) {
	profile := ResolveModelProfile("MiniMax-M3")
	below := NewStreamAccumulatorForProfile(profile, nil, nil)
	if err := below.ConsumeLine(`data: {"choices":[],"usage":{"prompt_tokens":511,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":400}}}`); err != nil {
		t.Fatal(err)
	}
	if usage := below.Result().Usage; usage.CacheReadTokens != nil || usage.CacheMissTokens != nil {
		t.Fatalf("sub-threshold MiniMax cache values must be unavailable: %#v", usage)
	}

	warm := NewStreamAccumulatorForProfile(profile, nil, nil)
	line := `data: {"choices":[{"delta":{"reasoning_details":[{"type":"text","text":"inspect "},{"type":"text","text":"carefully"}]}}],"usage":{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":700}}}`
	if err := warm.ConsumeLine(line); err != nil {
		t.Fatal(err)
	}
	result := warm.Result()
	if result.Reasoning != "inspect carefully" || string(result.ReasoningDetails) != `[{"text":"inspect ","type":"text"},{"text":"carefully","type":"text"}]` {
		t.Fatalf("MiniMax reasoning parse = text %q details %s", result.Reasoning, result.ReasoningDetails)
	}
	if result.Usage.CacheReadTokens == nil || *result.Usage.CacheReadTokens != 700 || result.Usage.CacheMissTokens == nil || *result.Usage.CacheMissTokens != 300 || !result.Usage.MissDerived {
		t.Fatalf("MiniMax usage parse = %#v", result.Usage)
	}
}

// decodeSSEStream replays a full batch SSE payload through the same
// line-by-line ConsumeLine path the live streaming provider uses (feature 010
// T035: the tests that used to call the removed batch-decode island
// ParseOpenAIStream/NewStreamAccumulator now exercise the one real decode
// path instead — no separate implementation left to drift from it).
func decodeSSEStream(text string, profile ModelProfile) (StreamResult, error) {
	a := NewStreamAccumulatorForProfile(profile, nil, nil)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if err := a.ConsumeLine(line); err != nil {
			return StreamResult{}, err
		}
	}
	return a.Result(), nil
}

func parseUsageGolden(t *testing.T, usage string) contract.Usage {
	t.Helper()
	stream := "data: {\"choices\":[],\"usage\":" + usage + "}\n\ndata: [DONE]\n"
	result, err := decodeSSEStream(stream, ModelProfile{})
	if err != nil {
		t.Fatalf("decodeSSEStream returned an error for usage %s: %v", usage, err)
	}
	return result.Usage
}

func intPointer(value int) *int { return &value }
