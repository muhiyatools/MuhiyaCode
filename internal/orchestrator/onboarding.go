package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

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
