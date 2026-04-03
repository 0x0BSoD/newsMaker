// Copyright (c) 2024, 0x0BSoD. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/0x0BSoD/newsMaker/internal/bot"
	"github.com/0x0BSoD/newsMaker/internal/bot/middleware"
	"github.com/0x0BSoD/newsMaker/internal/botkit"
	"github.com/0x0BSoD/newsMaker/internal/config"
	"github.com/0x0BSoD/newsMaker/internal/digest"
	"github.com/0x0BSoD/newsMaker/internal/fetcher"
	"github.com/0x0BSoD/newsMaker/internal/github"
	"github.com/0x0BSoD/newsMaker/internal/notifier"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/summary"
	"github.com/0x0BSoD/newsMaker/internal/telegraph"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Get()

	summaryInputDir := cfg.SummaryInputDir
	if summaryInputDir == "" {
		if exe, err := os.Executable(); err == nil {
			summaryInputDir = filepath.Dir(exe)
		} else {
			slog.Warn("could not resolve binary dir, using working directory", "err", err)
			summaryInputDir = "."
		}
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		slog.Error("failed to create bot API", "err", err)
		return
	}

	db, err := sqlx.Connect("postgres", cfg.DatabaseDSN)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		return
	}
	defer db.Close()

	githubClient := github.NewClient(cfg.GitHubToken)
	telegraphClient := telegraph.NewClient(cfg.TelegraphToken)
	repoStorage := storage.NewGitHubRepoStorage(db)

	var (
		digestSummarizer     notifier.Summarizer
		newsDigestSummarizer notifier.Summarizer
	)

	switch cfg.AIType {
	case "openai":
		if cfg.AIKey == "" {
			slog.Error("ai_key is required when ai_type is openai")
			return
		}
		digestSummarizer = summary.NewOpenAISummarizer(
			cfg.AIBaseURL,
			cfg.AIKey,
			cfg.DigestSummaryPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		newsDigestSummarizer = summary.NewOpenAISummarizer(
			cfg.AIBaseURL,
			cfg.AIKey,
			cfg.NewsDigestPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		slog.Info("summarizers ready", "type", "openai", "model", cfg.AIModel)
	default:
		if cfg.AIBaseURL == "" {
			slog.Error("ai_base_url is required when ai_type is ollama")
			return
		}
		digestSummarizer = summary.NewOllamaSummarizer(
			cfg.AIBaseURL,
			cfg.DigestSummaryPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		newsDigestSummarizer = summary.NewOllamaSummarizer(
			cfg.AIBaseURL,
			cfg.NewsDigestPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		slog.Info("summarizers ready", "type", "ollama", "model", cfg.AIModel)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rep := reporter.New(botAPI, cfg.TelegramAdminChatID)

	var (
		articleStorage = storage.NewArticleStorage(db)
		sourceStorage  = storage.NewSourceStorage(db)
		notifier       = notifier.New(
			articleStorage,
			newsDigestSummarizer,
			botAPI,
			cfg.NewsDigestMorningHour,
			cfg.NewsDigestNoonHour,
			cfg.NewsDigestEveningHour,
			cfg.NewsDigestLookback,
			cfg.NewsDigestMaxArticles,
			cfg.TelegramChannelID,
			rep,
			cfg.NewsDigestRetryInterval,
			cfg.NewsDigestMaxRetries,
			summaryInputDir,
		)
		fetcher = fetcher.New(
			articleStorage,
			sourceStorage,
			cfg.FetchInterval,
			cfg.FilterKeywords,
			rep,
		)
		digest = digest.New(
			githubClient,
			telegraphClient,
			botAPI,
			repoStorage,
			digestSummarizer,
			cfg.TelegramChannelID,
			cfg.GitHubTopics,
			cfg.DigestInterval,
			summaryInputDir,
		)
	)

	// Fall back to the admin chat if no dedicated test channel is configured.
	testChannelID := cfg.TelegramTestChannelID
	if testChannelID == 0 {
		testChannelID = cfg.TelegramAdminChatID
	}

	newsBot := botkit.New(botAPI)
	newsBot.RegisterCmdView(
		"testdigest",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdTestDigest(digest, testChannelID),
		),
	)
	newsBot.RegisterCmdView(
		"testnews",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdTestNews(notifier, testChannelID),
		),
	)
	newsBot.RegisterCmdView(
		"repostnews",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdRepostNews(notifier),
		),
	)
	newsBot.RegisterCmdView(
		"addsource",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdAddSource(sourceStorage, newsBot),
		),
	)
	newsBot.RegisterCmdView(
		"setpriority",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdSetPriority(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"getsource",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdGetSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"listsources",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdListSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"deletesource",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdDeleteSource(sourceStorage),
		),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	go func(ctx context.Context) {
		if err := digest.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("digest stopped unexpectedly", "err", err)
				rep.Notify(fmt.Sprintf("Notifier stopped: %v", err))
				return
			}
			slog.Error("digest stopped", "err", err)
		}
	}(ctx)

	go func(ctx context.Context) {
		if err := fetcher.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("fetcher stopped unexpectedly", "err", err)
				rep.Notify(fmt.Sprintf("Fetcher stopped: %v", err))
				return
			}
			slog.Info("fetcher stopped")
		}
	}(ctx)

	go func(ctx context.Context) {
		if err := notifier.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("notifier stopped unexpectedly", "err", err)
				rep.Notify(fmt.Sprintf("Notifier stopped: %v", err))
				return
			}
			slog.Info("notifier stopped")
		}
	}(ctx)

	go func(ctx context.Context) {
		if err := http.ListenAndServe("127.0.0.1:8088", mux); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("http server stopped unexpectedly", "err", err)
			}
		}
	}(ctx)

	if err := newsBot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("botkit stopped unexpectedly", "err", err)
	}
}
