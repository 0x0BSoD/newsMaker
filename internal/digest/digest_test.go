package digest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/0x0BSoD/newsMaker/internal/github"
	"github.com/0x0BSoD/newsMaker/internal/model"
)

func TestDedup(t *testing.T) {
	top := []github.Repo{
		{FullName: "a/one"},
		{FullName: "b/two"},
	}
	recent := []github.Repo{
		{FullName: "b/two"},
		{FullName: "c/three"},
	}

	got := dedup(top, recent)

	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.FullName
	}
	assert.Equal(t, []string{"a/one", "b/two", "c/three"}, names)
}

func TestIsReadable(t *testing.T) {
	assert.True(t, isReadable("plain ASCII text 123"))
	assert.True(t, isReadable("Кириллица тоже ок"))
	assert.True(t, isReadable("emoji 🚀 fine"))
	assert.False(t, isReadable("中文说明"))
	assert.False(t, isReadable("日本語"))
}

func TestBuildSummaryInput(t *testing.T) {
	prev := 100
	newRepos := []model.GitHubRepo{
		{FullName: "a/new", Stars: 42, Language: "Go", Description: "fresh"},
	}
	trending := []model.GitHubRepo{
		{FullName: "b/hot", Stars: 150, StarsAtLastDigest: &prev, Description: "growing"},
	}

	out := buildSummaryInput("kubernetes", newRepos, trending)

	assert.Contains(t, out, "Topic: kubernetes")
	assert.Contains(t, out, "- a/new (42 stars, Go): fresh")
	assert.Contains(t, out, "- b/hot (150 stars +50%, unknown): growing")
}

func TestBuildTelegramMessage_AllTopicsEmpty(t *testing.T) {
	results := []topicResult{{topic: "go"}, {topic: "rust"}}
	assert.Equal(t, "empty", buildTelegramMessage(results, 0, 0, true))
}
