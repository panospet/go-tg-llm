package llm

// Message is a single turn in a conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Usage holds the token counts and estimated cost for a single API call.
type Usage struct {
	InputTokens  int
	OutputTokens int
	// CostUSD is the estimated cost in US dollars based on the provider's
	// published pricing at the time the provider package was written.
	// Treat it as an approximation; check the provider's pricing page for
	// exact figures.
	CostUSD float64
}

// LLM is the abstraction over any large-language-model provider used by the
// bot. Implementations receive the full conversation history and return the
// next assistant turn together with token usage metadata.
type LLM interface {
	Chat(messages []Message) (string, Usage, error)
}

// Ask is a convenience wrapper that sends a single user question to the
// provider as a fresh one-turn conversation.
func Ask(l LLM, question string) (string, Usage, error) {
	return l.Chat([]Message{{Role: "user", Content: question}})
}
