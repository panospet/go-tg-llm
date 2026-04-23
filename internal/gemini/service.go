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
	DefaultModel     = "gemini-3-flash"
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
	Contents []Content `json:"contents"`
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
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (s *Service) Ask(question string) (string, error) {
	reqBody, err := json.Marshal(
		Request{
			Contents: []Content{
				{
					Role:  "user",
					Parts: []Part{{Text: llm.FormatQuestionForTg(question)}},
				},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(EndpointTemplate, s.model)
	req, err := http.NewRequest(
		http.MethodPost,
		url,
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-goog-api-key", s.apiKey)
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
	llm.LogResponse("gemini", resp.StatusCode, bodyBytes)

	var result Response
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", err
	}

	if result.Error != nil {
		return "", fmt.Errorf("gemini: %s (%s)", result.Error.Message, result.Error.Status)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini: unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		var buf bytes.Buffer
		for _, p := range result.Candidates[0].Content.Parts {
			buf.WriteString(p.Text)
		}
		return buf.String(), nil
	}

	return "No answer received.", nil
}
