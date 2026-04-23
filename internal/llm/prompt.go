package llm

import "fmt"

// DefaultPrompt is a provider-agnostic instruction wrapped around the user's
// question. It targets Telegram's legacy "Markdown" parse mode, which is a
// safe subset supported uniformly across clients.
const DefaultPrompt = "You are answering a user question that will be rendered inside a Telegram chat.\n" +
	"Follow these rules strictly:\n\n" +
	"1. Format the answer using Telegram's legacy *Markdown* syntax only:\n" +
	"   - `*bold*` for key terms or emphasis\n" +
	"   - `_italic_` for subtle emphasis\n" +
	"   - `` `inline code` `` for identifiers, commands, filenames or short snippets\n" +
	"   - triple-backtick fenced blocks for multi-line code\n" +
	"   - `[label](https://url)` for hyperlinks\n" +
	"2. Do NOT use Markdown headings (`#`), tables, HTML tags, or MarkdownV2-specific escaping (`\\`, `~`, `||`, etc.).\n" +
	"3. Keep the answer concise and well structured. Prefer short paragraphs. For lists, use `• ` (bullet) or `1. ` (numbered) at the start of the line.\n" +
	"4. If you rely on external sources, append a final section exactly titled `*Sources*` followed by one link per line as `[1](url)`, `[2](url)`, ... Omit this section entirely when no sources are used.\n" +
	"5. Answer in the same language the question was asked in.\n" +
	"6. Never wrap the whole reply in a code block and never prefix it with explanations like \"Here is the answer\".\n\n" +
	"Question: %q"

// FormatQuestionForTg wraps the raw user question with the default
// Telegram-friendly, provider-agnostic prompt.
func FormatQuestionForTg(question string) string {
	return fmt.Sprintf(DefaultPrompt, question)
}
