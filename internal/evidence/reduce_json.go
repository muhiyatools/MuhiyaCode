package evidence

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type JSONReductionInput struct {
	Kind, Status, Artifact, Query string
	Complete                      bool
	Raw                           []byte
}

func ReduceJSON(input JSONReductionInput) ObservationCard {
	card := ObservationCard{Status: input.Status, Complete: input.Complete, Artifact: input.Artifact, Summary: input.Kind + " structured result"}
	var value any
	if err := json.Unmarshal(input.Raw, &value); err != nil {
		card.Facts = []string{"invalid_json=" + err.Error()}
		card.Excerpts = []Excerpt{{Source: input.Kind, StartLine: 1, EndLine: 1, Text: truncateUTF8(string(input.Raw), 2_000), Protected: true}}
		return card
	}
	flattened := make([]string, 0, 64)
	flattenJSON("$", value, 0, &flattened)
	query := strings.ToLower(strings.TrimSpace(input.Query))
	sort.SliceStable(flattened, func(i, j int) bool {
		left := strings.Contains(strings.ToLower(flattened[i]), query)
		right := strings.Contains(strings.ToLower(flattened[j]), query)
		if left != right && query != "" {
			return left
		}
		return flattened[i] < flattened[j]
	})
	shown := min(80, len(flattened))
	if shown > 0 {
		card.Excerpts = []Excerpt{{Source: input.Kind, StartLine: 1, EndLine: shown, Text: strings.Join(flattened[:shown], "\n")}}
	}
	card.OmittedLines = len(flattened) - shown
	return card
}

func flattenJSON(path string, value any, depth int, output *[]string) {
	if depth > 8 || len(*output) >= 1_000 {
		*output = append(*output, path+"=<depth-or-item-limit>")
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenJSON(path+"."+key, typed[key], depth+1, output)
		}
	case []any:
		for index, item := range typed {
			flattenJSON(fmt.Sprintf("%s[%d]", path, index), item, depth+1, output)
		}
	default:
		*output = append(*output, fmt.Sprintf("%s=%v", path, typed))
	}
}
