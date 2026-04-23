package main

import (
	"log/slog"
	"os"

	"github.com/caarlos0/env/v11"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"go-tg-llm/internal/gemini"
	"go-tg-llm/internal/llm"
	"go-tg-llm/internal/perplexity"
)

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
		sendMarkdown(bot, update.Message.Chat.ID, answer)
	}
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		slog.Error("send plain message failed", "chat_id", chatID, "err", err)
	}
}

// sendMarkdown sends text to Telegram using the legacy Markdown parse mode and
// transparently falls back to plain text if Telegram rejects the formatting
// (e.g. unbalanced `*` / `_` characters the model may produce).
func sendMarkdown(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.DisableWebPagePreview = true
	if _, err := bot.Send(msg); err != nil {
		slog.Warn("markdown send failed, retrying as plain text", "chat_id", chatID, "err", err)
		reply(bot, chatID, text)
	}
}
