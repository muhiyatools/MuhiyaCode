package gateway

import (
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// ModelAdapter is selected only from the already-selected immutable catalog
// record. It normalizes provider-family behavior but has no model-selection API.
type ModelAdapter interface {
	Family() string
	Profile(contract.Model) ModelProfile
}

type familyAdapter struct {
	family string
}

func (a familyAdapter) Family() string { return a.family }

func (a familyAdapter) Profile(model contract.Model) ModelProfile {
	identity := model.ID + " " + model.Name
	switch a.family {
	case "minimax-openrouter":
		return ResolveModelProfile("minimax " + identity)
	case "deepseek":
		return ResolveModelProfile("deepseek " + identity)
	default:
		return ResolveModelProfile(identity)
	}
}

var modelAdapters = map[string]ModelAdapter{
	"minimax-openrouter": familyAdapter{family: "minimax-openrouter"},
	"deepseek":           familyAdapter{family: "deepseek"},
	"openai-compatible":  familyAdapter{family: "openai-compatible"},
}

func AdapterForModel(model contract.Model) ModelAdapter {
	family := strings.ToLower(strings.TrimSpace(model.ProviderFamily))
	if adapter, ok := modelAdapters[family]; ok {
		return adapter
	}
	return modelAdapters["openai-compatible"]
}

func ResolveCatalogModelProfile(model contract.Model) ModelProfile {
	return AdapterForModel(model).Profile(model)
}
