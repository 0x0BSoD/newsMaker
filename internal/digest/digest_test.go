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

func TestSelectTopRepos(t *testing.T) {
	prev := 100
	results := []topicResult{
		{
			topic:    "go",
			newRepos: []model.GitHubRepo{{FullName: "a/new", Stars: 300}},
			trending: []model.GitHubRepo{{FullName: "b/hot", Stars: 150, StarsAtLastDigest: &prev}}, // delta 50
		},
		{
			topic:    "k8s",
			newRepos: []model.GitHubRepo{{FullName: "a/new", Stars: 300}}, // duplicate across topics
			trending: []model.GitHubRepo{{FullName: "c/big", Stars: 600, StarsAtLastDigest: &prev}}, // delta 500
		},
	}

	top := selectTopRepos(results, 2)

	names := make([]string, len(top))
	for i, r := range top {
		names[i] = r.FullName
	}
	// c/big (delta 500) beats a/new (delta 300 = all stars, new repo); b/hot (50) cut by n=2.
	assert.Equal(t, []string{"c/big", "a/new"}, names)
}

func TestBuildWeeklyTopInput(t *testing.T) {
	prev := 100
	repos := []model.GitHubRepo{
		{FullName: "b/hot", HTMLURL: "https://github.com/b/hot", Stars: 150, StarsAtLastDigest: &prev, Topic: "go", Description: "growing"},
	}

	out := buildWeeklyTopInput(repos)

	assert.Contains(t, out, "- b/hot | https://github.com/b/hot | 150 stars (+50 this week) | unknown | topic: go")
	assert.Contains(t, out, "growing")
}

func TestBuildTelegramMessage_LLMPost(t *testing.T) {
	results := []topicResult{{topic: "go", pageURL: "https://telegra.ph/go"}}
	out := buildTelegramMessage("Топ недели!", nil, results)

	assert.Contains(t, out, "Топ недели!")
	assert.Contains(t, out, `<a href="https://telegra.ph/go">go</a>`)
}

func TestBuildTelegramMessage_Fallback(t *testing.T) {
	top := []model.GitHubRepo{
		{FullName: "a/new", HTMLURL: "https://github.com/a/new", Stars: 300, Description: "fresh"},
	}
	out := buildTelegramMessage("", top, nil)

	assert.Contains(t, out, "GitHub Digest")
	assert.Contains(t, out, `<a href="https://github.com/a/new">a/new</a> ⭐300 (+300)`)
	assert.Contains(t, out, "fresh")
}
