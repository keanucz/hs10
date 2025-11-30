package promptcoach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// Coach provides prompt analysis and rewrites.
type Coach struct {
	client *openai.Client
}

// Result holds the structured response returned to the UI.
type Result struct {
	Analysis       string `json:"analysis"`
	ImprovedPrompt string `json:"improved_prompt"`
}

// New creates a Coach backed by OpenAI (reusing the existing API key).
func New() *Coach {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return &Coach{}
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Coach{client: &client}
}

// ImprovePrompt critiques the provided prompt and offers a refined alternative.
func (c *Coach) ImprovePrompt(ctx context.Context, prompt string) (*Result, error) {
	cleaned := strings.TrimSpace(prompt)
	if cleaned == "" {
		return nil, errors.New("prompt required")
	}

	if c.client == nil {
		return fallbackResult(cleaned), nil
	}

	coachSystemPrompt := `You are Clippy, a friendly but direct prompt coach.
You must respond strictly with a compact JSON object matching:
{"analysis":"one sentence critique","improved_prompt":"rewritten prompt"}
Keep the improved prompt actionable and under 120 words.`

	resp, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: openai.ResponsesModel(openai.ChatModelGPT4oMini),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam{
			responses.ResponseInputItemParamOfMessage(coachSystemPrompt, responses.EasyInputMessageRoleSystem),
			responses.ResponseInputItemParamOfMessage(cleaned, responses.EasyInputMessageRoleUser),
		}},
		MaxOutputTokens: openai.Int(400),
		Temperature:     openai.Float(0.3),
	})
	if err != nil {
		return nil, fmt.Errorf("prompt coach failed: %w", err)
	}

	payload := strings.TrimSpace(resp.OutputText())
	if payload == "" {
		return fallbackResult(cleaned), nil
	}

	var parsed Result
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse coach response: %w", err)
	}

	parsed.Analysis = fallbackString(parsed.Analysis, "Clippy couldn't find anything to change, but here's a quick tidy-up.")
	parsed.ImprovedPrompt = fallbackString(strings.TrimSpace(parsed.ImprovedPrompt), cleaned)

	return &parsed, nil
}

func fallbackResult(original string) *Result {
	return &Result{
		Analysis:       "Clippy is offline, so here's your original prompt.",
		ImprovedPrompt: original,
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// TimeoutContext ensures prompt analysis doesn't hang the request for too long.
func TimeoutContext(parent context.Context) (context.Context, context.CancelFunc) {
	deadline := 15 * time.Second
	if dl, ok := parent.Deadline(); ok {
		remaining := time.Until(dl)
		if remaining > 0 && remaining < deadline {
			deadline = remaining
		}
	}
	return context.WithTimeout(parent, deadline)
}
