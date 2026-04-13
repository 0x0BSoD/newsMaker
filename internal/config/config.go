package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/cristalhq/aconfig"
	"github.com/cristalhq/aconfig/aconfighcl"
)

type Config struct {
	TelegramBotToken        string        `hcl:"telegram_bot_token" env:"TELEGRAM_BOT_TOKEN" required:"true"`
	TelegramChannelID       int64         `hcl:"telegram_channel_id" env:"TELEGRAM_CHANNEL_ID" required:"true"`
	TelegramAdminChatID     int64         `hcl:"telegram_admin_chat_id" env:"TELEGRAM_ADMIN_CHAT_ID"`
	TelegramTestChannelID   int64         `hcl:"telegram_test_channel_id" env:"TELEGRAM_TEST_CHANNEL_ID"`
	GitHubToken             string        `hcl:"github_token" env:"GITHUB_TOKEN"`
	GitHubTopics            []string      `hcl:"github_topics" env:"GITHUB_TOPICS"`
	TelegraphToken          string        `hcl:"telegraph_token" env:"TELEGRAPH_TOKEN"`
	DigestInterval          time.Duration `hcl:"digest_interval" env:"DIGEST_INTERVAL" default:"168h"`
	DigestSummaryPrompt     string        `hcl:"digest_summary_prompt" env:"DIGEST_SUMMARY_PROMPT" default:"Summarize these GitHub repositories in 2-3 sentences, highlighting key trends and notable projects:"`
	NewsDigestMorningHour   int           `hcl:"news_digest_morning_hour" env:"NEWS_DIGEST_MORNING_HOUR" default:"9"`
	NewsDigestNoonHour      int           `hcl:"news_digest_noon_hour" env:"NEWS_DIGEST_NOON_HOUR" default:"12"`
	NewsDigestEveningHour   int           `hcl:"news_digest_evening_hour" env:"NEWS_DIGEST_EVENING_HOUR" default:"18"`
	NewsDigestLookback      time.Duration `hcl:"news_digest_lookback" env:"NEWS_DIGEST_LOOKBACK" default:"12h"`
	NewsDigestMaxArticles   int           `hcl:"news_digest_max_articles" env:"NEWS_DIGEST_MAX_ARTICLES" default:"30"`
	NewsDigestRetryInterval time.Duration `hcl:"news_digest_retry_interval" env:"NEWS_DIGEST_RETRY_INTERVAL" default:"5m"`
	NewsDigestMaxRetries    int           `hcl:"news_digest_max_retries" env:"NEWS_DIGEST_MAX_RETRIES" default:"3"`
	NewsDigestMaxDataLen    int           `hcl:"news_digest_max_data_len" env:"NEWS_DIGEST_MAX_DATA_LEN" default:"500"`
	SummaryInputDir         string        `hcl:"summary_input_dir" env:"SUMMARY_INPUT_DIR" default:""`
	NewsDigestPrompt        string        `hcl:"news_digest_prompt" env:"NEWS_DIGEST_PROMPT" default:"You are a tech news digest writer for a Telegram channel. Given articles grouped by topic and time of day, write an engaging news digest in Telegram HTML format. Start with 'Good morning!' or 'Good evening!' matching the time of day indicated in the input. Briefly introduce what is happening, then for each topic write a short bold header using <b>Topic</b> and list articles as bullet points using the format: • <a href='URL'>Title</a> — one sentence description. Keep it concise and friendly. Output only the final message text, no extra commentary."`
	DatabaseDSN             string        `hcl:"database_dsn" env:"DATABASE_DSN" default:"postgres://postgres:postgres@localhost:5432/news?sslmode=disable"`
	FetchInterval           time.Duration `hcl:"fetch_interval" env:"FETCH_INTERVAL" default:"10m"`
	NotificationInterval    time.Duration `hcl:"notification_interval" env:"NOTIFICATION_INTERVAL" default:"1m"`
	FilterKeywords          []string      `hcl:"filter_keywords" env:"FILTER_KEYWORDS"`
	AIType                  string        `hcl:"ai_type" env:"AI_TYPE" default:"ollama"`
	AIBaseURL               string        `hcl:"ai_base_url" env:"AI_BASE_URL"`
	AIKey                   string        `hcl:"ai_key" env:"AI_KEY"`
	AIPrompt                string        `hcl:"ai_prompt" env:"AI_PROMPT"`
	AIModel                 string        `hcl:"ai_model" env:"AI_MODEL" default:"llama3"`
	AITimeout               time.Duration `hcl:"ai_timeout" env:"AI_TIMEOUT" default:"30m"`
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
