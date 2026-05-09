# go-tg-llm

A lightweight Telegram bot written in Go that routes user questions to one or more LLM providers — currently **Google Gemini** and **Perplexity AI** — and sends the answers back as Telegram-formatted messages.

---

## Features

- **Multi-provider support** — use Gemini, Perplexity, or both simultaneously. Providers are enabled at startup based on which API keys are present in the environment.
- **Provider-specific commands** — target a specific model with `/gemini` (or `/gem`) and `/perplexity` (or `/perp`), or let the bot pick the default with `/ask`.
- **Telegram-optimised output** — responses are formatted using Telegram's legacy Markdown subset (bold, italic, inline code, fenced code blocks, hyperlinks) and automatically split into ≤ 4 000-rune chunks to stay within Telegram's message size limit, with graceful fallback to plain text if Markdown is rejected.
- **Zero external state** — stateless single binary; no database required.
- **Docker-ready** — multi-stage Dockerfile produces a minimal Alpine image; a `docker-compose.yml` is included for quick deployment.

---

## Bot Commands

| Command | Alias | Description |
|---|---|---|
| `/ask <question>` | — | Ask the default provider (Gemini when available, otherwise Perplexity). |
| `/gemini <question>` | `/gem` | Ask Google Gemini specifically. |
| `/perplexity <question>` | `/perp` | Ask Perplexity AI specifically. |

If a provider is not configured (missing API key), its command will reply with *"Provider not configured. Check server environment."*

---

## Architecture

```
main.go                  ← Telegram polling loop, command dispatch, message chunking
internal/
  llm/
    llm.go               ← LLM interface (Ask(question) → string)
    prompt.go            ← Shared Telegram-friendly system prompt
    log.go               ← HTTP response logging helper
  gemini/
    service.go           ← Google Generative Language REST client
  perplexity/
    service.go           ← Perplexity chat-completions REST client
```

Both provider packages implement the `llm.LLM` interface, so adding a new provider is as simple as creating a new package that satisfies `Ask(string) (string, error)`.

---

## Configuration

Copy `env.example` to `.env` (or export the variables directly) and fill in the values:

```env
# Required
TELEGRAM_BOT_TOKEN=your-telegram-bot-token

# At least one of the following must be set
PERPLEXITY_API_KEY=your-perplexity-api-key
GEMINI_API_KEY=your-gemini-api-key

# Optional – leave empty to use the defaults shown below
PERPLEXITY_MODEL=   # default: sonar
GEMINI_MODEL=       # default: gemini-2.5-flash
```

> **Note:** The bot exits at startup if `TELEGRAM_BOT_TOKEN` is missing, or if neither LLM API key is provided.

---

## Getting Started

### Prerequisites

- Go 1.26+
- A [Telegram Bot Token](https://core.telegram.org/bots#botfather)
- At least one of: a [Gemini API key](https://aistudio.google.com/app/apikey) or a [Perplexity API key](https://www.perplexity.ai/settings/api)

### Run locally

```bash
# Clone the repo
git clone https://github.com/panospet/go-tg-llm.git
cd go-tg-llm

# Set environment variables
cp env.example .env
# edit .env with your keys

export $(cat .env | xargs)

# Build and run
make build
./bin/telegram-llm-bot
```

### Run with Docker

```bash
# Build the image
make container

# Run (keys injected via environment)
docker run --rm \
  -e TELEGRAM_BOT_TOKEN=... \
  -e GEMINI_API_KEY=... \
  telegram-llm-bot
```

### Run with Docker Compose

```bash
# Set TELEGRAM_BOT_TOKEN and PERPLEXITY_API_KEY in your shell or a .env file
docker compose up -d
```

### Makefile targets

| Target | Description |
|---|---|
| `make build` | Compile a static binary to `./bin/telegram-llm-bot` |
| `make container` | Build the Docker image (`telegram-llm-bot`) |
| `make container-push` | Push the image to `REGISTRY_URL` |

Set `REGISTRY_URL` to push to a private registry:

```bash
make container-push REGISTRY_URL=registry.example.com/myorg
```

---

## License

[MIT](LICENSE)
