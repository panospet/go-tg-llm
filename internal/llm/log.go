package llm

import (
	"bytes"
	"encoding/json"
	"log/slog"
)

// LogResponse logs a provider HTTP response via slog. If the body is valid
// JSON it is compacted so pretty-printed payloads don't clutter the logs;
// otherwise the raw bytes are logged verbatim.
func LogResponse(provider string, status int, body []byte) {
	payload := string(body)
	var buf bytes.Buffer
	if err := json.Compact(&buf, body); err == nil {
		payload = buf.String()
	}
	slog.Info("provider response",
		"provider", provider,
		"status", status,
		"body", payload,
	)
}
