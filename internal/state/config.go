package state

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func DefaultSettings() contract.Settings {
	var settings contract.Settings
	settings.Version = 1
	settings.Provider.Type = "openai-compatible"
	settings.Provider.BaseURL = "https://api.muhiya.com/v1"
	settings.Provider.Models = []contract.Model{}
	settings.PermissionMode = contract.PermissionNormal
	settings.Effort = contract.EffortHigh
	settings.TokenEconomyMode = "off"
	settings.Theme = "muhiya-dark"
	settings.Shell.Preferred = "auto"
	settings.Shell.TimeoutMS = 120_000
	settings.Shell.OutputLimit = 30_000
	settings.RTL.Mode = "auto"
	settings.RTL.Align = "auto"
	settings.UI.BorderMode = "auto"
	return settings
}

func DefaultSecrets() contract.Secrets { return contract.Secrets{} }

func LoadSettings(paths ...Paths) (contract.Settings, error) {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return contract.Settings{}, err
	}
	raw, err := readJSON[json.RawMessage](p.SettingsFile, nil)
	if err != nil {
		return contract.Settings{}, err
	}
	settings := DefaultSettings()
	if len(raw) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return contract.Settings{}, fmt.Errorf("invalid settings.json: %w", err)
	}
	var legacy struct {
		Provider struct {
			Model string `json:"model"`
		} `json:"provider"`
	}
	_ = json.Unmarshal(raw, &legacy)
	if legacy.Provider.Model != "" && !modelExists(settings.Provider.Models, legacy.Provider.Model) {
		settings.Provider.Models = append(settings.Provider.Models, contract.Model{ID: StableModelID(legacy.Provider.Model), Name: legacy.Provider.Model, Source: "manual", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	normalizeSettings(&settings)
	if environmentMode := strings.TrimSpace(os.Getenv("MUHIYA_TOKEN_ECONOMY_MODE")); environmentMode != "" {
		mode, ok := normalizeTokenEconomyMode(environmentMode)
		if !ok {
			return contract.Settings{}, fmt.Errorf("invalid MUHIYA_TOKEN_ECONOMY_MODE %q", environmentMode)
		}
		settings.TokenEconomyMode = mode
	}
	if err := ValidateSettings(settings); err != nil {
		return contract.Settings{}, fmt.Errorf("invalid settings.json: %w", err)
	}
	return settings, nil
}

func SaveSettings(settings contract.Settings, paths ...Paths) error {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return err
	}
	normalizeSettings(&settings)
	if err := ValidateSettings(settings); err != nil {
		return err
	}
	return writeJSON(p.SettingsFile, settings, false)
}

func LoadSecrets(paths ...Paths) (contract.Secrets, error) {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return contract.Secrets{}, err
	}
	return readJSON(p.SecretsFile, DefaultSecrets())
}

func SaveSecrets(secrets contract.Secrets, paths ...Paths) error {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return err
	}
	return writeJSON(p.SecretsFile, secrets, true)
}

func ValidateSettings(settings contract.Settings) error {
	if settings.Version != 1 {
		return fmt.Errorf("unsupported settings version %d", settings.Version)
	}
	if settings.Provider.Type != "openai-compatible" {
		return fmt.Errorf("provider.type must be openai-compatible")
	}
	if settings.PermissionMode != contract.PermissionNormal && settings.PermissionMode != contract.PermissionAutoAccept {
		return fmt.Errorf("permissionMode must be normal or auto-accept")
	}
	if _, ok := normalizeEffort(string(settings.Effort)); !ok {
		return fmt.Errorf("effort must be low, medium, high, or max")
	}
	if _, ok := normalizeTokenEconomyMode(settings.TokenEconomyMode); !ok {
		return fmt.Errorf("tokenEconomyMode must be off, observe, balanced, or aggressive")
	}
	if !slices.Contains([]string{"auto", "pwsh", "powershell", "cmd", "sh"}, settings.Shell.Preferred) {
		return fmt.Errorf("shell.preferred is invalid")
	}
	if settings.Shell.TimeoutMS <= 0 || settings.Shell.OutputLimit <= 0 {
		return fmt.Errorf("shell timeout and output limit must be positive")
	}
	if !slices.Contains([]string{"auto", "visual", "native", "off"}, settings.RTL.Mode) {
		return fmt.Errorf("rtl.mode is invalid")
	}
	if !slices.Contains([]string{"auto", "right", "left"}, settings.RTL.Align) {
		return fmt.Errorf("rtl.align is invalid")
	}
	if !slices.Contains([]string{"auto", "unicode", "ascii"}, settings.UI.BorderMode) {
		return fmt.Errorf("ui settings are invalid")
	}
	seen := make(map[string]bool)
	for _, model := range settings.Provider.Models {
		if strings.TrimSpace(model.ID) == "" || strings.TrimSpace(model.Name) == "" || model.ContextLimit < 0 {
			return fmt.Errorf("model id/name must be non-empty and contextLimit non-negative")
		}
		if seen[model.ID] {
			return fmt.Errorf("duplicate model id %q", model.ID)
		}
		seen[model.ID] = true
	}
	return nil
}

func ActiveModel(settings contract.Settings) (contract.Model, bool) {
	for _, model := range settings.Provider.Models {
		if model.ID == settings.Provider.ActiveModelID {
			return model, true
		}
	}
	return contract.Model{}, false
}

func AutoAssignModels(settings *contract.Settings) {
	if len(settings.Provider.Models) == 0 {
		return
	}
	if modelExists(settings.Provider.Models, settings.Provider.ActiveModelID) {
		return
	}
	ranked := append([]contract.Model(nil), settings.Provider.Models...)
	sort.SliceStable(ranked, func(i, j int) bool { return modelScore(ranked[i]) > modelScore(ranked[j]) })
	// Prefer a Pro/Reasoner-class model; fall back to the highest-scoring model
	// when no name matches (deterministic ranked order).
	if pro, ok := firstModelMatching(ranked, "pro", "reasoner", "max", "large"); ok {
		settings.Provider.ActiveModelID = pro.ID
	} else {
		settings.Provider.ActiveModelID = ranked[0].ID
	}
}

// AssignFreshDefaultModels picks the explicit first-run model. It is
// intentionally separate from score-based AutoAssignModels: M3's enormous
// window makes it the right session default, but its context score alone does
// not reliably beat a "pro"-marked model. An existing choice is never touched.
func AssignFreshDefaultModels(settings *contract.Settings) bool {
	if settings == nil || strings.TrimSpace(settings.Provider.ActiveModelID) != "" {
		return false
	}
	for _, model := range settings.Provider.Models {
		// The M3 detector is shared with the capability profile (gateway
		// package) so the default can never miss a catalog entry the profile
		// resolver already treats as M3 (e.g. a bare "M3" name).
		if contract.IsMiniMaxM3Name(strings.ToLower(model.ID + " " + model.Name)) {
			settings.Provider.ActiveModelID = model.ID
			return true
		}
	}
	return false
}

// firstModelMatching returns the first model in ranked order whose id or name
// contains any of the given lowercase substrings. It lets AutoAssignModels honor
// an explicit family preference while keeping the score-based ranking as the
// tiebreaker and fallback.
func firstModelMatching(models []contract.Model, substrings ...string) (contract.Model, bool) {
	for _, model := range models {
		name := strings.ToLower(model.ID + " " + model.Name)
		for _, sub := range substrings {
			if strings.Contains(name, sub) {
				return model, true
			}
		}
	}
	return contract.Model{}, false
}

func UpsertModel(settings *contract.Settings, model contract.Model, activate bool) contract.Model {
	if strings.TrimSpace(model.ID) == "" {
		model.ID = StableModelID(model.Name)
	}
	if model.Source == "" {
		model.Source = "manual"
	}
	for i, existing := range settings.Provider.Models {
		if existing.ID == model.ID || existing.Name == model.Name {
			if model.CreatedAt == "" {
				model.CreatedAt = existing.CreatedAt
			}
			settings.Provider.Models[i] = model
			if activate {
				settings.Provider.ActiveModelID = model.ID
			}
			return model
		}
	}
	if model.CreatedAt == "" {
		model.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	settings.Provider.Models = append(settings.Provider.Models, model)
	if activate {
		settings.Provider.ActiveModelID = model.ID
	}
	return model
}

func SetConfig(key, value string, settings *contract.Settings, secrets *contract.Secrets) error {
	switch key {
	case "baseUrl":
		settings.Provider.BaseURL = value
	case "model":
		UpsertModel(settings, contract.Model{ID: value, Name: value, Source: "manual"}, true)
		AutoAssignModels(settings)
		// An explicit choice pins the model: the advisor proposes, but it never
		// overrides what the user asked for.
		settings.Provider.RolesPinned = true
	case "contextLimit":
		limit, err := strconv.Atoi(value)
		if err != nil || limit <= 0 {
			return fmt.Errorf("contextLimit must be a positive integer")
		}
		for i := range settings.Provider.Models {
			if settings.Provider.Models[i].ID == settings.Provider.ActiveModelID {
				settings.Provider.Models[i].ContextLimit = limit
				return nil
			}
		}
		return fmt.Errorf("no active model")
	case "apiKey":
		secrets.ProviderAPIKey = value
	case "permissionMode":
		settings.PermissionMode = contract.PermissionMode(value)
	case "effort", "reasoning", "reasoningEffort":
		effort, ok := normalizeEffort(value)
		if !ok {
			return fmt.Errorf("reasoning effort must be low, medium, high, or max")
		}
		settings.Effort = effort
	case "tokenEconomyMode":
		mode, ok := normalizeTokenEconomyMode(value)
		if !ok {
			return fmt.Errorf("tokenEconomyMode must be off, observe, balanced, or aggressive")
		}
		settings.TokenEconomyMode = mode
	case "reviewGating":
		normalized := strings.TrimSpace(strings.ToLower(value))
		switch normalized {
		case "", "default", "conservative", "off":
			settings.ReviewGating = normalized
		default:
			return fmt.Errorf("reviewGating must be off, conservative, or default")
		}
	case "advisor":
		// The session-start model advisor: "auto" (default) proposes a pairing on
		// the first prompt of a session; "off" always uses the configured models.
		normalized := strings.TrimSpace(strings.ToLower(value))
		switch normalized {
		case "", "auto", "off":
			settings.Provider.Advisor = normalized
		default:
			return fmt.Errorf("advisor must be auto or off")
		}
	case "rtlMode":
		settings.RTL.Mode = value
	case "rtlAlign":
		settings.RTL.Align = value
	case "uiBorderMode":
		settings.UI.BorderMode = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return ValidateSettings(*settings)
}

func StableModelID(name string) string {
	var result strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		allowed := char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("_.:-", char)
		if allowed {
			result.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(result.String(), "-")
	if value == "" {
		return "model"
	}
	return value
}

func RedactedSettings(settings contract.Settings, secrets contract.Secrets) map[string]any {
	raw, _ := json.Marshal(settings)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	provider, _ := value["provider"].(map[string]any)
	if secrets.ProviderAPIKey != "" {
		provider["apiKey"] = "********"
	} else {
		provider["apiKey"] = ""
	}
	return value
}

func normalizeSettings(settings *contract.Settings) {
	if settings.Version == 0 {
		settings.Version = 1
	}
	if effort, ok := normalizeEffort(string(settings.Effort)); ok {
		settings.Effort = effort
	}
	if mode, ok := normalizeTokenEconomyMode(settings.TokenEconomyMode); ok {
		settings.TokenEconomyMode = mode
	} else {
		// Tolerate values written by a future/experimental build. Conservative
		// off preserves compatibility and guarantees no request-side behavior.
		settings.TokenEconomyMode = "off"
	}
	if settings.Provider.Models == nil {
		settings.Provider.Models = []contract.Model{}
	}
	for i := range settings.Provider.Models {
		if settings.Provider.Models[i].ID == "" {
			settings.Provider.Models[i].ID = StableModelID(settings.Provider.Models[i].Name)
		}
	}
	AutoAssignModels(settings)
}

func normalizeEffort(value string) (contract.EffortLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "min", "minimal":
		return contract.EffortLow, true
	case "medium", "":
		return contract.EffortMedium, true
	case "high":
		return contract.EffortHigh, true
	case "max", "ultra", "xhigh":
		return contract.EffortMax, true
	default:
		return "", false
	}
}

func normalizeTokenEconomyMode(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "off":
		return "off", true
	case "observe":
		return "observe", true
	case "balanced":
		return "balanced", true
	case "aggressive":
		return "aggressive", true
	default:
		return "", false
	}
}

func modelExists(models []contract.Model, value string) bool {
	for _, model := range models {
		if model.ID == value || model.Name == value {
			return true
		}
	}
	return false
}

func modelScore(model contract.Model) int {
	score := model.ContextLimit
	name := strings.ToLower(model.ID + " " + model.Name)
	if strings.Contains(name, "pro") || strings.Contains(name, "max") || strings.Contains(name, "reasoner") || strings.Contains(name, "large") {
		score += 5_000_000
	}
	if strings.Contains(name, "flash") || strings.Contains(name, "mini") || strings.Contains(name, "lite") || strings.Contains(name, "fast") || strings.Contains(name, "small") {
		score -= 2_000_000
	}
	return score
}
