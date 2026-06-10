package config

import (
	"sync"
	"time"

	"github.com/cristalhq/aconfig"
	"github.com/cristalhq/aconfig/aconfigyaml"
)

type Config struct {
	Telegram struct {
		BotToken       string `yaml:"bot_token" env:"TELEGRAM_BOT_TOKEN" required:"true"`
		ChannelID      int64  `yaml:"channel_id" env:"TELEGRAM_CHANNEL_ID" required:"true"`
		AdminChatID    int64  `yaml:"admin_chat_id" env:"TELEGRAM_ADMIN_CHAT_ID"`
		TestChannelID  int64  `yaml:"test_channel_id" env:"TELEGRAM_TEST_CHANNEL_ID"`
		TelegraphToken string `yaml:"telegraph_token" env:"TELEGRAPH_TOKEN"`
	} `yaml:"telegram"`
	GitHub struct {
		Token  string   `yaml:"token" env:"GITHUB_TOKEN"`
		Topics []string `yaml:"topics" env:"GITHUB_TOPICS"`
	} `yaml:"github"`
	LLM struct {
		Type    string        `yaml:"type" env:"AI_TYPE" default:"ollama"`
		BaseURL string        `yaml:"base_url" env:"AI_BASE_URL"`
		Key     string        `yaml:"key" env:"AI_KEY"`
		Model   string        `yaml:"model" env:"AI_MODEL" default:"llama3"`
		Timeout time.Duration `yaml:"timeout" env:"AI_TIMEOUT" default:"30m"`
	} `yaml:"LLM"`
	Digest struct {
		Enabled  bool          `yaml:"enabled" env:"DIGEST_ENABLED" default:"false"`
		Interval time.Duration `yaml:"interval" env:"DIGEST_INTERVAL" default:"168h"`
		Prompt   string        `yaml:"prompt" env:"DIGEST_SUMMARY_PROMPT" default:"Summarize these GitHub repositories in 2-3 sentences, highlighting key trends and notable projects:"`
	} `yaml:"digest"`
	News struct {
		MorningHour   int           `yaml:"morning_hour" env:"NEWS_DIGEST_MORNING_HOUR" default:"9"`
		NoonHour      int           `yaml:"noon_hour" env:"NEWS_DIGEST_NOON_HOUR" default:"12"`
		EveningHour   int           `yaml:"evening_hour" env:"NEWS_DIGEST_EVENING_HOUR" default:"18"`
		Lookback      time.Duration `yaml:"lookback" env:"NEWS_DIGEST_LOOKBACK" default:"12h"`
		MaxArticles   int           `yaml:"max_articles" env:"NEWS_DIGEST_MAX_ARTICLES" default:"30"`
		RetryInterval time.Duration `yaml:"retry_interval" env:"NEWS_DIGEST_RETRY_INTERVAL" default:"5m"`
		MaxRetries    int           `yaml:"max_retries" env:"NEWS_DIGEST_MAX_RETRIES" default:"3"`
		MaxDataLen    int           `yaml:"max_data_len" env:"NEWS_DIGEST_MAX_DATA_LEN" default:"500"`
		Prompt        string        `yaml:"prompt" env:"NEWS_DIGEST_PROMPT" default:"You are a tech news digest writer for a Telegram channel. Given articles grouped by topic and time of day, write an engaging news digest in Telegram HTML format. Start with 'Good morning!' or 'Good evening!' matching the time of day indicated in the input. Briefly introduce what is happening, then for each topic write a short bold header using <b>Topic</b> and list articles as bullet points using the format: • <a href='URL'>Title</a> — one sentence description. Keep it concise and friendly. Output only the final message text, no extra commentary."`
	} `yaml:"news"`
	Post struct {
		MaxContentLen int    `yaml:"max_content_len" env:"POST_MAX_CONTENT_LEN" default:"8000"`
		Prompt        string `yaml:"prompt" env:"POST_PROMPT" default:"You are a tech editor for a Telegram channel. Given an article (URL, title, content) and an optional editor's note, write a short engaging post about it in Telegram HTML format: a bold one-line header using <b>, then 2-4 sentences summarizing the key points, weaving in the editor's note if present. Include a link to the article using <a href='URL'>. Keep it concise. Output only the final message text, no extra commentary."`
	} `yaml:"post"`
	SummaryInputDir string        `yaml:"summary_input_dir" env:"SUMMARY_INPUT_DIR" default:""`
	DatabaseDSN     string        `yaml:"database_dsn" env:"DATABASE_DSN" default:"postgres://postgres:postgres@localhost:5432/news?sslmode=disable"`
	FetchInterval   time.Duration `yaml:"fetch_interval" env:"FETCH_INTERVAL" default:"10m"`
	FilterKeywords  []string      `yaml:"filter_keywords" env:"FILTER_KEYWORDS"`
}

var (
	cfg     Config
	loadErr error
	once    sync.Once
)

func load(c *Config, configPath string) error {
	// aconfig stops at the first file that exists, so an explicit path must
	// replace the search list, not extend it — and it must exist.
	places := []string{"./config.yaml", "./config.local.yaml", "$HOME/.config/news-feed-bot/config.yaml"}
	failOnNotFound := false
	if configPath != "" {
		places = []string{configPath}
		failOnNotFound = true
	}
	loader := aconfig.LoaderFor(c, aconfig.Config{
		SkipFlags:          true, // config comes from YAML and NFB_* env vars only
		EnvPrefix:          "NFB",
		Files:              places,
		FailOnFileNotFound: failOnNotFound,
		FileDecoders: map[string]aconfig.FileDecoder{
			".yaml": aconfigyaml.New(),
			".yml":  aconfigyaml.New(),
		},
	})

	return loader.Load()
}

func Get(configPath string) (Config, error) {
	once.Do(func() {
		loadErr = load(&cfg, configPath)
	})

	return cfg, loadErr
}
