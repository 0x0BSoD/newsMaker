package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/cristalhq/aconfig"
	"github.com/cristalhq/aconfig/aconfighcl"
)

type Config struct {
	TelegramBotToken     string        `hcl:"telegram_bot_token" env:"TELEGRAM_BOT_TOKEN" required:"true"`
	TelegramChannelID    int64         `hcl:"telegram_channel_id" env:"TELEGRAM_CHANNEL_ID" required:"true"`
	TelegramAdminChatID  int64         `hcl:"telegram_admin_chat_id" env:"TELEGRAM_ADMIN_CHAT_ID"`
	GitHubToken          string        `hcl:"github_token" env:"GITHUB_TOKEN"`
	GitHubTopics         []string      `hcl:"github_topics" env:"GITHUB_TOPICS"`
	TelegraphToken       string        `hcl:"telegraph_token" env:"TELEGRAPH_TOKEN"`
	DigestInterval       time.Duration `hcl:"digest_interval" env:"DIGEST_INTERVAL" default:"168h"`
	DigestSummaryPrompt  string        `hcl:"digest_summary_prompt" env:"DIGEST_SUMMARY_PROMPT" default:"Summarize these GitHub repositories in 2-3 sentences, highlighting key trends and notable projects:"`
	DatabaseDSN          string        `hcl:"database_dsn" env:"DATABASE_DSN" default:"postgres://postgres:postgres@localhost:5432/news?sslmode=disable"`
	FetchInterval        time.Duration `hcl:"fetch_interval" env:"FETCH_INTERVAL" default:"10m"`
	NotificationInterval time.Duration `hcl:"notification_interval" env:"NOTIFICATION_INTERVAL" default:"1m"`
	FilterKeywords       []string      `hcl:"filter_keywords" env:"FILTER_KEYWORDS"`
	AIType               string        `hcl:"ai_type" env:"AI_TYPE" default:"ollama"`
	AIBaseURL            string        `hcl:"ai_base_url" env:"AI_BASE_URL"`
	AIKey                string        `hcl:"ai_key" env:"AI_KEY"`
	AIPrompt             string        `hcl:"ai_prompt" env:"AI_PROMPT"`
	AIModel              string        `hcl:"ai_model" env:"AI_MODEL" default:"llama3"`
	AITimeout            time.Duration `hcl:"ai_timeout" env:"AI_TIMEOUT" default:"30m"`
}

var (
	cfg  Config
	once sync.Once
)

func Get() Config {
	once.Do(func() {
		loader := aconfig.LoaderFor(&cfg, aconfig.Config{
			EnvPrefix: "NFB",
			Files:     []string{"./config.hcl", "./config.local.hcl", "$HOME/.config/news-feed-bot/config.hcl"},
			FileDecoders: map[string]aconfig.FileDecoder{
				".hcl": aconfighcl.New(),
			},
		})

		if err := loader.Load(); err != nil {
			slog.Error("failed to load config", "err", err)
		}
	})

	return cfg
}
