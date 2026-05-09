package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go-tg-llm/internal/llm"
)

const (
	// EndpointTemplate is the Google Generative Language REST endpoint.
	// The model slug is interpolated into the URL.
	EndpointTemplate = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
	DefaultModel     = "gemini-2.5-flash"

	// Pricing for gemini-2.5-flash (≤200K token context window).
	// Source: https://ai.google.dev/pricing — verify for the latest rates.
	priceInputPer1M  = 0.15 // USD per 1M input tokens
	priceOutputPer1M = 0.60 // USD per 1M output tokens
)

// Service is a Gemini-backed implementation of llm.LLM.
type Service struct {
	apiKey string
	model  string
	client *http.Client
}

// NewService constructs a Gemini client. If model is empty, DefaultModel is used.
func NewService(apiKey, model string) *Service {
	if model == "" {
		model = DefaultModel
	}
	return &Service{
		apiKey: apiKey,
		model:  model,
		client: http.DefaultClient,
	}
}

type Request struct {
	// SystemInstruction is injected once and applies to the whole conversation.
	SystemInstruction *Content  `json:"system_instruction,omitempty"`
	Contents          []Content `json:"contents"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type Response struct {
	Candidates []struct {
		Content struct {
			Parts []Part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata,omitempty"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// Chat sends the full conversation history to Gemini and returns the next
// assistant turn together with token usage. The Telegram formatting system
// prompt is injected once via the systemInstruction field. Role "assistant"
// is mapped to "model" as required by the Gemini API.
func (s *Service) Chat(messages []llm.Message) (string, llm.Usage, error) {
	contents := make([]Content, 0, len(messages))
	for _, m := range messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, Content{
			Role:  role,
			Parts: []Part{{Text: m.Content}},
		})
	}

	reqBody, err := json.Marshal(Request{
		SystemInstruction: &Content{
			Parts: []Part{{Text: llm.SystemPrompt}},
		},
		Contents: contents,
	})
	if err != nil {
		return "", llm.Usage{}, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(EndpointTemplate, s.model)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", llm.Usage{}, err
	}
	req.Header.Set("x-goog-api-key", s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", llm.Usage{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", llm.Usage{}, err
	}
	llm.LogResponse("gemini", resp.StatusCode, bodyBytes)

	var result Response
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", llm.Usage{}, err
	}

	if result.Error != nil {
		return "", llm.Usage{}, fmt.Errorf("gemini: %s (%s)", result.Error.Message, result.Error.Status)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", llm.Usage{}, fmt.Errorf("gemini: unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var usage llm.Usage
	if m := result.UsageMetadata; m != nil {
		usage.InputTokens = m.PromptTokenCount
		usage.OutputTokens = m.CandidatesTokenCount
		usage.CostUSD = (float64(m.PromptTokenCount)/1_000_000)*priceInputPer1M +
			(float64(m.CandidatesTokenCount)/1_000_000)*priceOutputPer1M
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		var buf bytes.Buffer
		for _, p := range result.Candidates[0].Content.Parts {
			buf.WriteString(p.Text)
		}
		return buf.String(), usage, nil
	}

	return "No answer received.", usage, nil
}
