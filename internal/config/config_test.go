package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleConfigLoads guards config.example.yaml against drifting from the
// Config struct: every key in the example must parse into the right field.
func TestExampleConfigLoads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	require.NoError(t, err)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "config.yaml"), data, 0o644))
	t.Chdir(tmp)

	var c Config
	require.NoError(t, load(&c, ""))

	assert.Equal(t, "123456789:AAExampleTokenReplaceMe", c.Telegram.BotToken)
	assert.Equal(t, int64(-1001234567890), c.Telegram.ChannelID)
	assert.Equal(t, int64(123456789), c.Telegram.AdminChatID)
	assert.Equal(t, int64(-1009876543210), c.Telegram.TestChannelID)

	assert.Equal(t, []string{"kubernetes", "golang", "platform-engineering"}, c.GitHub.Topics)

	assert.Equal(t, "ollama", c.LLM.Type)
	assert.Equal(t, "localhost:11434", c.LLM.BaseURL)
	assert.Equal(t, "llama3", c.LLM.Model)
	assert.Equal(t, 30*time.Minute, c.LLM.Timeout)

	assert.Equal(t, 9, c.News.MorningHour)
	assert.Equal(t, 12, c.News.NoonHour)
	assert.Equal(t, 18, c.News.EveningHour)
	assert.Equal(t, 12*time.Hour, c.News.Lookback)
	assert.Equal(t, 30, c.News.MaxArticles)
	assert.Equal(t, 5*time.Minute, c.News.RetryInterval)
	assert.Equal(t, 3, c.News.MaxRetries)
	assert.Equal(t, 500, c.News.MaxDataLen)
	assert.NotEmpty(t, c.News.Prompt)

	assert.False(t, c.Digest.Enabled)
	assert.Equal(t, 168*time.Hour, c.Digest.Interval)
	assert.NotEmpty(t, c.Digest.Prompt)

	assert.Equal(t, 10*time.Minute, c.FetchInterval)
	assert.Equal(t, []string{"leetcode", "sponsored"}, c.FilterKeywords)
	assert.NotEmpty(t, c.DatabaseDSN)
}

// TestExplicitConfigPath checks that a path given via -config wins over
// config.yaml in the working directory and that a missing path is an error.
func TestExplicitConfigPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	require.NoError(t, err)

	tmp := t.TempDir()
	explicit := filepath.Join(tmp, "custom.yaml")
	require.NoError(t, os.WriteFile(explicit, data, 0o644))

	// Decoy config.yaml in the working directory must be ignored.
	cwd := t.TempDir()
	decoy := "telegram:\n  bot_token: decoy\n  channel_id: 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "config.yaml"), []byte(decoy), 0o644))
	t.Chdir(cwd)

	var c Config
	require.NoError(t, load(&c, explicit))
	assert.Equal(t, "123456789:AAExampleTokenReplaceMe", c.Telegram.BotToken)

	var missing Config
	assert.Error(t, load(&missing, filepath.Join(tmp, "does-not-exist.yaml")))
}
