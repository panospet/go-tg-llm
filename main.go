package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

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
	var defaultProvider llm.LLM
	if p, ok := providers["gemini"]; ok {
		defaultProvider = p
	} else {
		defaultProvider = providers["perplexity"]
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	slog.Info("listening to bot messages")

	for update := range updates {
		if update.Message == nil || !update.Message.IsCommand() {
			continue
		}

		var provider llm.LLM
		var providerName string
		switch update.Message.Command() {
		case "perplexity", "perp":
			provider = providers["perplexity"]
			providerName = "Perplexity"
		case "gemini", "gem":
			provider = providers["gemini"]
			providerName = "Gemini"
		case "ask":
			provider = defaultProvider
			providerName = "LLM"
		default:
			continue
		}

		if provider == nil {
			reply(bot, update.Message.Chat.ID, "Provider not configured. Check server environment.")
			continue
		}

		question := update.Message.CommandArguments()
		if len(question) == 0 {
			reply(bot, update.Message.Chat.ID, "Please provide a question.")
			continue
		}

		reply(bot, update.Message.Chat.ID, "Thinking...")
		answer, err := provider.Ask(question)
		if err != nil {
			slog.Error("provider ask failed", "provider", providerName, "err", err)
			reply(bot, update.Message.Chat.ID, "Error contacting "+providerName+": "+err.Error())
			continue
		}
		sendLong(bot, update.Message.Chat.ID, answer)
	}
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		slog.Error("send plain message failed", "chat_id", chatID, "err", err)
	}
}

// sendLong splits the answer into chunks that fit Telegram's per-message
// limit and sends each chunk sequentially using the legacy Markdown parse
// mode. Per chunk, if Telegram rejects the Markdown (e.g. unbalanced `*`/`_`
// or a code fence split across chunks), it falls back to plain text for that
// chunk only.
func sendLong(bot *tgbotapi.BotAPI, chatID int64, text string) {
	chunks := splitForTelegram(text, telegramChunkRunes)
	for i, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.DisableWebPagePreview = true
		if _, err := bot.Send(msg); err != nil {
			slog.Warn("markdown send failed, retrying as plain text",
				"chat_id", chatID,
				"chunk", i+1,
				"of", len(chunks),
				"err", err,
			)
			reply(bot, chatID, chunk)
		}
	}
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
