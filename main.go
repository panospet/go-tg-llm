package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"go-tg-llm/internal/conversation"
	"go-tg-llm/internal/gemini"
	"go-tg-llm/internal/llm"
	"go-tg-llm/internal/perplexity"
)

// telegramChunkRunes is the per-message length budget (Telegram's hard limit
// is 4096 characters; we keep headroom because the server counts UTF-16 code
// units, so emoji/surrogate-pair-heavy text can overrun a naive rune count).
const telegramChunkRunes = 4000

type config struct {
	PerplexityAPIKey string `env:"PERPLEXITY_API_KEY"`
	PerplexityModel  string `env:"PERPLEXITY_MODEL"`
	GeminiAPIKey     string `env:"GEMINI_API_KEY"`
	GeminiModel      string `env:"GEMINI_MODEL"`
	TelegramBotToken string `env:"TELEGRAM_BOT_TOKEN,required"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	var cfg config
	if err := env.Parse(&cfg); err != nil {
		slog.Error("parse config", "err", err)
		os.Exit(1)
	}

	if cfg.PerplexityAPIKey == "" && cfg.GeminiAPIKey == "" {
		slog.Error("at least one of PERPLEXITY_API_KEY or GEMINI_API_KEY must be set")
		os.Exit(1)
	}

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		slog.Error("create telegram bot", "err", err)
		os.Exit(1)
	}

	providers := map[string]llm.LLM{}
	if cfg.PerplexityAPIKey != "" {
		providers["perplexity"] = perplexity.NewService(cfg.PerplexityAPIKey, cfg.PerplexityModel)
	}
	if cfg.GeminiAPIKey != "" {
		providers["gemini"] = gemini.NewService(cfg.GeminiAPIKey, cfg.GeminiModel)
	}

	// defaultProvider is used by the neutral /ask command. Gemini wins when
	// available, otherwise Perplexity.
	var defaultProviderName string
	if _, ok := providers["gemini"]; ok {
		defaultProviderName = "gemini"
	} else {
		defaultProviderName = "perplexity"
	}

	// Start conversation store with a daily cleanup goroutine.
	store := conversation.NewStore(conversation.DefaultMaxTurns)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go store.StartCleanup(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	slog.Info("listening to bot messages")

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// --- Conversation continuation: user replied to a bot message ---
		if isReplyToBot(bot, update.Message) {
			handleContinuation(bot, update.Message, store, providers)
			continue
		}

		// --- Fresh command ---
		if !update.Message.IsCommand() {
			continue
		}

		var providerName string
		switch update.Message.Command() {
		case "perplexity", "perp":
			providerName = "perplexity"
		case "gemini", "gem":
			providerName = "gemini"
		case "ask":
			providerName = defaultProviderName
		default:
			continue
		}

		provider, ok := providers[providerName]
		if !ok {
			reply(bot, update.Message.Chat.ID, "Provider not configured. Check server environment.")
			continue
		}

		question := update.Message.CommandArguments()
		if len(question) == 0 {
			reply(bot, update.Message.Chat.ID, "Please provide a question.")
			continue
		}

		// Label the user's message with their display name so the LLM can
		// track multiple speakers in a group conversation.
		name := senderName(update.Message.From)
		labelledQuestion := "[" + name + "]: " + question

		answer, usage, err := llm.Ask(provider, labelledQuestion)
		if err != nil {
			slog.Error("provider ask failed", "provider", providerName, "err", err)
			reply(bot, update.Message.Chat.ID, "Error contacting provider: "+err.Error())
			continue
		}

		// Substitute the real cost into the LLM's {{COST}} placeholder.
		displayAnswer := injectCost(answer, usage)

		botMsgID, err := sendLong(bot, update.Message.Chat.ID, displayAnswer, update.Message.MessageID)
		if err != nil {
			slog.Error("send answer failed", "err", err)
			continue
		}

		// Store the substituted answer so history is human-readable.
		store.Save(update.Message.Chat.ID, botMsgID, &conversation.Conversation{
			Messages: []llm.Message{
				{Role: "user", Content: labelledQuestion},
				{Role: "assistant", Content: displayAnswer},
			},
			Provider: providerName,
		})
	}
}

// isReplyToBot returns true when the message is a reply to a message sent by
// this bot instance.
func isReplyToBot(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) bool {
	return msg.ReplyToMessage != nil &&
		msg.ReplyToMessage.From != nil &&
		msg.ReplyToMessage.From.ID == bot.Self.ID
}

// handleContinuation loads the conversation history for the replied-to bot
// message, appends the new user turn, calls the same provider, and saves the
// updated history under the new bot message ID.
func handleContinuation(
	bot *tgbotapi.BotAPI,
	msg *tgbotapi.Message,
	store *conversation.Store,
	providers map[string]llm.LLM,
) {
	chatID := msg.Chat.ID
	repliedToID := msg.ReplyToMessage.MessageID
	userText := strings.TrimSpace(msg.Text)

	if userText == "" {
		return
	}

	conv, ok := store.Get(chatID, repliedToID)
	if !ok {
		reply(bot, chatID, "I've lost the context of this conversation (it may have expired). Please start a new one with /ask.")
		return
	}

	provider, ok := providers[conv.Provider]
	if !ok {
		reply(bot, chatID, "The provider for this conversation is no longer configured.")
		return
	}

	// Label the speaker so the LLM can distinguish participants.
	name := senderName(msg.From)
	labelledText := "[" + name + "]: " + userText

	// Build updated history (store.Save will trim to maxTurns).
	newMessages := make([]llm.Message, len(conv.Messages), len(conv.Messages)+1)
	copy(newMessages, conv.Messages)
	newMessages = append(newMessages, llm.Message{Role: "user", Content: labelledText})

	answer, usage, err := provider.Chat(newMessages)
	if err != nil {
		slog.Error("provider chat failed", "provider", conv.Provider, "err", err)
		reply(bot, chatID, "Error contacting provider: "+err.Error())
		return
	}

	// Substitute the real cost into the LLM's {{COST}} placeholder.
	displayAnswer := injectCost(answer, usage)

	botMsgID, err := sendLong(bot, chatID, displayAnswer, msg.MessageID)
	if err != nil {
		slog.Error("send continuation answer failed", "err", err)
		return
	}

	// Persist updated history under the new bot message ID.
	updatedConv := &conversation.Conversation{
		Messages: append(newMessages, llm.Message{Role: "assistant", Content: displayAnswer}),
		Provider: conv.Provider,
	}
	store.Save(chatID, botMsgID, updatedConv)
	// Also keep the old ID pointing to the same history so any branch reply
	// to the previous bot message still resolves.
	store.Save(chatID, repliedToID, updatedConv)
}

// senderName returns the best available display name for a Telegram user.
// Priority: FirstName (+ LastName if set) → Username → "User".
func senderName(user *tgbotapi.User) string {
	if user == nil {
		return "User"
	}
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name != "" {
		return name
	}
	if user.UserName != "" {
		return user.UserName
	}
	return "User"
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		slog.Error("send plain message failed", "chat_id", chatID, "err", err)
	}
}

// injectCost replaces the {{COST}} placeholder that the LLM writes in its
// roast sentence with the real dollar amount from the API usage metadata.
// If no token data is available the placeholder is removed.
func injectCost(answer string, u llm.Usage) string {
	var costStr string
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		costStr = fmt.Sprintf("$%.6f", u.CostUSD)
	}
	return strings.ReplaceAll(answer, "{{COST}}", costStr)
}


// sendLong splits the answer into chunks that fit Telegram's per-message
// limit and sends each chunk sequentially using the legacy Markdown parse
// mode. The first chunk is sent as a reply to replyToMsgID (0 = no reply).
// Returns the MessageID of the last sent chunk (used as the conversation key).
func sendLong(bot *tgbotapi.BotAPI, chatID int64, text string, replyToMsgID int) (int, error) {
	chunks := splitForTelegram(text, telegramChunkRunes)
	var lastMsgID int
	for i, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.DisableWebPagePreview = true
		if i == 0 && replyToMsgID != 0 {
			msg.ReplyToMessageID = replyToMsgID
		}
		sent, err := bot.Send(msg)
		if err != nil {
			slog.Warn("markdown send failed, retrying as plain text",
				"chat_id", chatID,
				"chunk", i+1,
				"of", len(chunks),
				"err", err,
			)
			plain := tgbotapi.NewMessage(chatID, chunk)
			if i == 0 && replyToMsgID != 0 {
				plain.ReplyToMessageID = replyToMsgID
			}
			sent, err = bot.Send(plain)
			if err != nil {
				slog.Error("send plain message failed", "chat_id", chatID, "err", err)
				continue
			}
		}
		lastMsgID = sent.MessageID
	}
	return lastMsgID, nil
}

// splitForTelegram breaks text into chunks with at most `limit` runes each,
// preferring boundaries (in order): blank-line, newline, sentence end, space.
// If none of those exist within the window, it falls back to a hard cut.
func splitForTelegram(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	if len([]rune(text)) <= limit {
		return []string{text}
	}

	var chunks []string
	remaining := text
	for {
		runes := []rune(remaining)
		if len(runes) <= limit {
			if strings.TrimSpace(remaining) != "" {
				chunks = append(chunks, remaining)
			}
			return chunks
		}

		head := string(runes[:limit])
		splitIdx := -1
		for _, sep := range []string{"\n\n", "\n", ". ", "! ", "? ", " "} {
			if i := strings.LastIndex(head, sep); i > 0 {
				splitIdx = i + len(sep)
				break
			}
		}
		if splitIdx <= 0 {
			splitIdx = len(head)
		}

		chunk := strings.TrimRight(head[:splitIdx], " \t\n")
		if strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, chunk)
		}
		remaining = strings.TrimLeft(remaining[splitIdx:], " \t\n")
		if remaining == "" {
			return chunks
		}
	}
}
