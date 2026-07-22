package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type OnboardingAction string

const (
	OnboardingSuppressed OnboardingAction = "suppressed"
	OnboardingAskDirect  OnboardingAction = "ask_direct"
	OnboardingGenerate   OnboardingAction = "generate"
)

type OnboardingDecision struct {
	Action            OnboardingAction
	UseAuxiliaryModel bool
	Questions         []contract.Question
	Reason            string
}

func DecideOnboarding(prompt string, assessment Assessment) OnboardingDecision {
	if question, ok := directMaterialQuestion(prompt); ok {
		return OnboardingDecision{Action: OnboardingAskDirect, Questions: []contract.Question{question}, Reason: "explicit_material_choice"}
	}
	if assessment.Class == ClassChat || assessment.Class == ClassTiny || clearlyScopedSmallPrompt(prompt, assessment) {
		return OnboardingDecision{Action: OnboardingSuppressed, Reason: "clear_low_complexity"}
	}
	if ShouldConsiderOnboarding(prompt) {
		return OnboardingDecision{Action: OnboardingGenerate, UseAuxiliaryModel: true, Reason: "material_ambiguity_unresolved"}
	}
	return OnboardingDecision{Action: OnboardingSuppressed, Reason: "no_material_ambiguity"}
}

func clearlyScopedSmallPrompt(prompt string, assessment Assessment) bool {
	if assessment.Class != ClassSmall && assessment.Class != ClassTiny {
		return false
	}
	return len(pathRE.FindAllStringIndex(prompt, 2)) > 0 || strings.ContainsAny(prompt, ".:/\\`")
}

func directMaterialQuestion(prompt string) (contract.Question, bool) {
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "either ") || !strings.Contains(lower, " or ") {
		return contract.Question{}, false
	}
	if !strings.Contains(lower, "not chosen") && !strings.Contains(lower, "which") && !strings.Contains(lower, "choose") {
		return contract.Question{}, false
	}
	topic := "implementation"
	if strings.Contains(lower, "auth") || strings.Contains(lower, "login") {
		topic = "authentication"
	}
	return contract.Question{
		Question: "Which " + topic + " option should I implement?",
		Choices: []contract.QuestionChoice{
			{Label: "First option", Description: "Use the first option named in the request.", Recommended: true},
			{Label: "Second option", Description: "Use the second option named in the request."},
		},
	}, true
}

func (e *Engine) prepareTaskPrompt(ctx context.Context, prompt string, profile EffortProfile, assessment Assessment) (string, error) {
	decision := DecideOnboarding(prompt, assessment)
	if decision.Action == OnboardingAskDirect && e.callbacks.Ask != nil {
		answers, err := e.callbacks.Ask(ctx, decision.Questions)
		if err == nil {
			return PromptWithAnswers(prompt, answers), nil
		}
		return prompt, nil
	}
	if decision.Action != OnboardingGenerate || !profile.Onboarding || e.callbacks.Ask == nil {
		return prompt, nil
	}
	admission := EvaluateAuxiliaryAdmission(AuxiliaryAdmissionInput{
		Kind: AuxiliaryOnboarding, MaterialDecision: true, ExpectedTokenCost: 800, ExpectedMainSavings: 1_600,
		RemainingRequests: e.remainingAuxiliaryRequests(assessment, contract.ExecutionPhaseOrient), Isolated: true,
	})
	if !admission.Allowed {
		return prompt, nil
	}
	e.callbacks.EmitStatus("Clarifying the task...")
	started := time.Now()
	model := e.utilityModelID()
	questions, usage := GenerateOnboardingQuestions(ctx, e.provider, model, prompt, e.session.ID+":sub:onboarding")
	if err := e.recordUsageAndEmit(func() error {
		return e.recordAttributedAuxUsage(ctx, auxUsageObservation{
			model: model, pin: ":sub:onboarding", usage: usage, durationMS: elapsedMS(started),
			phase: contract.ExecutionPhaseOrient, decisionCode: admission.Code,
		})
	}); err != nil {
		return "", fmt.Errorf("persist onboarding usage: %w", err)
	}
	if len(questions) == 0 {
		return prompt, nil
	}
	answers, err := e.callbacks.Ask(ctx, questions)
	if err != nil {
		return prompt, nil
	}
	return PromptWithAnswers(prompt, answers), nil
}

func ShouldConsiderOnboarding(prompt string) bool {
	text := strings.TrimSpace(prompt)
	if text == "" || strings.HasPrefix(text, "/") || continuationRE.MatchString(text) || len(text) > 700 {
		return false
	}
	if strings.Count(text, "\n") >= 4 || strings.Count(text, "`") >= 2 || len(pathRE.FindAllStringIndex(text, 5)) >= 2 {
		return false
	}
	assessment := Classify(text, "")
	if assessment.Class == ClassChat || assessment.Class == ClassTiny {
		return false
	}
	vague := qualityRE.MatchString(text) || len(text) < 120 || (changeRE.MatchString(text) && !strings.ContainsAny(text, ".:/\\`"))
	return vague
}

func GenerateOnboardingQuestions(parent context.Context, provider contract.Provider, modelID, prompt, sessionID string) ([]contract.Question, contract.Usage) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	// C3/T011: an intentionally COLD, one-shot isolated stream. It uses the sub
	// stream pin and a fresh system+user pair that is never appended to, so it is
	// deliberately outside the main-loop prefix-shape guard — a single throwaway
	// request whose cache miss is expected and bounded to once per onboarding.
	response, err := provider.Chat(ctx, contract.ChatRequest{SessionID: sessionID, ModelID: modelID, Reasoning: contract.ReasoningLow, MaxTokens: 500, Messages: []contract.Message{
		{Role: contract.RoleSystem, Content: "Return JSON only: {\"questions\":[{\"question\":\"...\",\"choices\":[{\"label\":\"...\",\"description\":\"...\",\"recommended\":true}]}]}. Ask at most two high-value implementation questions only when answers materially change the work. Each question needs 2-4 exclusive choices and exactly one recommended choice."},
		{Role: contract.RoleUser, Content: prompt},
	}})
	if err != nil {
		return nil, contract.Usage{}
	}
	text := strings.TrimSpace(response.Content)
	if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	var payload struct {
		Questions []contract.Question `json:"questions"`
	}
	if json.Unmarshal([]byte(text), &payload) != nil {
		return nil, response.Usage
	}
	var result []contract.Question
	for _, question := range payload.Questions {
		question.Question = strings.TrimSpace(question.Question)
		if question.Question == "" || len(question.Choices) < 2 {
			continue
		}
		if len(question.Choices) > 4 {
			question.Choices = question.Choices[:4]
		}
		recommended := -1
		for i := range question.Choices {
			question.Choices[i].Label = strings.TrimSpace(question.Choices[i].Label)
			if question.Choices[i].Recommended && recommended < 0 {
				recommended = i
			} else {
				question.Choices[i].Recommended = false
			}
		}
		if recommended < 0 {
			question.Choices[0].Recommended = true
		}
		result = append(result, question)
		if len(result) == 2 {
			break
		}
	}
	return result, response.Usage
}

func PromptWithAnswers(prompt string, answers []contract.Answer) string {
	if len(answers) == 0 {
		return prompt
	}
	lines := []string{prompt, "", "[confirmed task choices]"}
	for _, answer := range answers {
		line := "- " + answer.Question + ": " + answer.Choice.Label
		if answer.Choice.Description != "" {
			line += " — " + answer.Choice.Description
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
