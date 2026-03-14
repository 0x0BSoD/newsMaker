# newsMaker

A Telegram bot that aggregates news from RSS feeds and GitHub, summarizes articles via a local or cloud LLM, and posts them to a Telegram channel.

## Features

- **RSS feed fetcher** — polls configurable RSS sources on a ticker, filters by keywords, deduplicates on article link
- **Notifier** — picks unposted articles, extracts full text via `go-readability`, summarizes with an LLM, posts to Telegram (MarkdownV2)
- **GitHub digest** — periodically searches GitHub for top and recently-created repos by topic, generates an AI summary, publishes a [Telegraph](https://telegra.ph) page, and posts the digest to Telegram
- **Bot commands** — admin-only Telegram commands for managing RSS sources
- **Health check** — HTTP endpoint at `127.0.0.1:8088/healthz`

## Requirements

- Go 1.25+
- PostgreSQL
- Ollama (local) **or** an OpenAI-compatible API endpoint

## Quick start

```bash
# Start dev database
docker compose -f docker-compose.dev.yaml up -d

# Build
go build -o main ./cmd/

# Run (config via HCL or env vars — see Configuration below)
./main
```

## Configuration

Config is loaded in order from:
1. `./config.hcl` or `./config.local.hcl`
2. `$HOME/.config/news-feed-bot/config.hcl`
3. Environment variables with prefix `NFB_`

| Field / Env var | Default | Description |
|---|---|---|
| `telegram_bot_token` / `NFB_TELEGRAM_BOT_TOKEN` | **required** | Telegram bot token |
| `telegram_channel_id` / `NFB_TELEGRAM_CHANNEL_ID` | **required** | Channel to post articles to |
| `telegram_admin_chat_id` / `NFB_TELEGRAM_ADMIN_CHAT_ID` | — | Admin chat for error reports |
| `database_dsn` / `NFB_DATABASE_DSN` | `postgres://postgres:postgres@localhost:5432/news?sslmode=disable` | PostgreSQL DSN |
| `fetch_interval` / `NFB_FETCH_INTERVAL` | `10m` | How often to poll RSS feeds |
| `notification_interval` / `NFB_NOTIFICATION_INTERVAL` | `1m` | How often to post an article |
| `filter_keywords` / `NFB_FILTER_KEYWORDS` | — | Keyword allowlist for articles |
| `ai_type` / `NFB_AI_TYPE` | `ollama` | `ollama` or `openai` |
| `ai_base_url` / `NFB_AI_BASE_URL` | **required** | Ollama base URL or OpenAI-compatible endpoint |
| `ai_key` / `NFB_AI_KEY` | — | API key (required for `openai`) |
| `ai_model` / `NFB_AI_MODEL` | `llama3` | Model name |
| `ai_prompt` / `NFB_AI_PROMPT` | — | System prompt for article summarization |
| `ai_timeout` / `NFB_AI_TIMEOUT` | `30m` | LLM request timeout |
| `github_token` / `NFB_GITHUB_TOKEN` | — | GitHub API token (for digest) |
| `github_topics` / `NFB_GITHUB_TOPICS` | — | Topics to search (e.g. `["go", "rust"]`) |
| `telegraph_token` / `NFB_TELEGRAPH_TOKEN` | — | Telegraph account token (for digest pages) |
| `digest_interval` / `NFB_DIGEST_INTERVAL` | `168h` | How often to post the GitHub digest |
| `digest_summary_prompt` / `NFB_DIGEST_SUMMARY_PROMPT` | *(default prompt)* | LLM prompt for digest summaries |

## Bot commands (admin-only)

| Command | Description |
|---|---|
| `/addsource` | Add an RSS feed source |
| `/deletesource` | Remove a source |
| `/getsource` | Show a source's details |
| `/listsources` | List all sources |
| `/setpriority` | Change a source's posting priority |

## Architecture

```
RSS sources ──▶ Fetcher ──▶ PostgreSQL ──▶ Notifier ──▶ LLM (Ollama/OpenAI) ──▶ Telegram channel
                                                ▲
GitHub API ──▶ Digest ──▶ Telegraph page ───────┘
```

- All major components depend on interfaces; mocks generated with [moq](https://github.com/matryer/moq)
- Articles deduplicate on `link` UNIQUE constraint (`INSERT ... ON CONFLICT DO NOTHING`)
- Notifier only considers articles newer than `2 × fetch_interval`
- Source `priority` field influences posting order (higher = posted sooner)
- DB migrations managed with [goose](https://github.com/pressly/goose) in `internal/storage/migrations/`

## Development

```bash
# Run all tests
go test ./...

# Run a specific package's tests
go test ./internal/fetcher/...

# Run a specific test
go test ./internal/fetcher/ -run TestFetcher_Fetch

# Regenerate mocks (requires moq)
go generate ./internal/fetcher/...
```
