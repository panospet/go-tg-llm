package llm

// Message is a single turn in a conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// LLM is the abstraction over any large-language-model provider used by the
// bot. Implementations receive the full conversation history and return the
// next assistant turn ready to be sent to the user.
type LLM interface {
	Chat(messages []Message) (string, error)
}

// Ask is a convenience wrapper that sends a single user question to the
// provider as a fresh one-turn conversation.
func Ask(l LLM, question string) (string, error) {
	return l.Chat([]Message{{Role: "user", Content: question}})
}
