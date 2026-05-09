package perplexity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go-tg-llm/internal/llm"
)

const (
	Endpoint     = "https://api.perplexity.ai/chat/completions"
	DefaultModel = "sonar"
)

// Service is a Perplexity-backed implementation of llm.LLM.
type Service struct {
	apiKey string
	model  string
	client *http.Client
}

// NewService constructs a Perplexity client. If model is empty, DefaultModel is used.
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
	Model    string `json:"model"`
	Messages []Msg  `json:"messages"`
}

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Chat sends the full conversation history to Perplexity and returns the next
// assistant turn. The Telegram formatting system prompt is prepended as a
// dedicated system role message before the conversation turns.
func (s *Service) Chat(messages []llm.Message) (string, error) {
	msgs := make([]Msg, 0, len(messages)+1)
	msgs = append(msgs, Msg{Role: "system", Content: llm.SystemPrompt})
	for _, m := range messages {
		msgs = append(msgs, Msg{Role: m.Role, Content: m.Content})
	}

	reqBody, err := json.Marshal(Request{
		Model:    s.model,
		Messages: msgs,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, Endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	llm.LogResponse("perplexity", resp.StatusCode, bodyBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("perplexity: unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result Response
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", err
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "No answer received.", nil
}
