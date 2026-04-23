package llm

// LLM is the abstraction over any large-language-model provider used by the
// bot. Implementations are expected to take a raw user question, apply any
// provider-specific wiring (prompt formatting, auth, HTTP call, parsing) and
// return a final answer string ready to be sent to the user.
type LLM interface {
	Ask(question string) (string, error)
}
