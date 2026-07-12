// Copyright (c) 2024, 0x0BSoD. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	"github.com/0x0BSoD/newsMaker/internal/poster"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/summary"
	"github.com/0x0BSoD/newsMaker/internal/telegraph"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	configPath := flag.String("config", "", "Path to the configuration file")
	flag.Parse()

	cfg, err := config.Get(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	summaryInputDir := cfg.SummaryInputDir
	if summaryInputDir == "" {
		if exe, err := os.Executable(); err == nil {
			summaryInputDir = filepath.Dir(exe)
		} else {
			slog.Warn("could not resolve binary dir, using working directory", "err", err)
			summaryInputDir = "."
		}
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.Telegram.BotToken)
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

	githubClient := github.NewClient(cfg.GitHub.Token)
	telegraphClient := telegraph.NewClient(cfg.Telegram.TelegraphToken)
	repoStorage := storage.NewGitHubRepoStorage(db)

	var (
		digestSummarizer     summary.Summarizer
		newsDigestSummarizer summary.Summarizer
		postSummarizer       summary.Summarizer
		reviewSummarizer     summary.Summarizer
	)

	switch cfg.LLM.Type {
	case "openai":
		if cfg.LLM.Key == "" {
			slog.Error("ai_key is required when ai_type is openai")
			return
		}
		digestSummarizer = summary.NewOpenAISummarizer(
			cfg.LLM.BaseURL,
			cfg.LLM.Key,
			cfg.Digest.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		newsDigestSummarizer = summary.NewOpenAISummarizer(
			cfg.LLM.BaseURL,
			cfg.LLM.Key,
			cfg.News.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		postSummarizer = summary.NewOpenAISummarizer(
			cfg.LLM.BaseURL,
			cfg.LLM.Key,
			cfg.Post.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		reviewSummarizer = summary.NewOpenAISummarizer(
			cfg.LLM.BaseURL,
			cfg.LLM.Key,
			cfg.Review.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		slog.Info("summarizers ready", "type", "openai", "model", cfg.LLM.Model)
	default:
		if cfg.LLM.BaseURL == "" {
			slog.Error("ai_base_url is required when ai_type is ollama")
			return
		}
		digestSummarizer = summary.NewOllamaSummarizer(
			cfg.LLM.BaseURL,
			cfg.Digest.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		newsDigestSummarizer = summary.NewOllamaSummarizer(
			cfg.LLM.BaseURL,
			cfg.News.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		postSummarizer = summary.NewOllamaSummarizer(
			cfg.LLM.BaseURL,
			cfg.Post.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		reviewSummarizer = summary.NewOllamaSummarizer(
			cfg.LLM.BaseURL,
			cfg.Review.Prompt,
			cfg.LLM.Model,
			cfg.LLM.Timeout,
		)
		slog.Info("summarizers ready", "type", "ollama", "model", cfg.LLM.Model)
	}

	if cfg.Review.Enabled {
		digestSummarizer = summary.NewReviewed(digestSummarizer, reviewSummarizer)
		newsDigestSummarizer = summary.NewReviewed(newsDigestSummarizer, reviewSummarizer)
		postSummarizer = summary.NewReviewed(postSummarizer, reviewSummarizer)
		slog.Info("LLM review stage enabled for all posts")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rep := reporter.New(botAPI, cfg.Telegram.AdminChatID)

	var (
		articleStorage = storage.NewArticleStorage(db)
		sourceStorage  = storage.NewSourceStorage(db)
		notifier       = notifier.New(
			articleStorage,
			newsDigestSummarizer,
			botAPI,
			rep,
			notifier.Config{
				ChannelID:       cfg.Telegram.ChannelID,
				MorningHour:     cfg.News.MorningHour,
				NoonHour:        cfg.News.NoonHour,
				EveningHour:     cfg.News.EveningHour,
				Lookback:        cfg.News.Lookback,
				MaxArticles:     cfg.News.MaxArticles,
				RetryInterval:   cfg.News.RetryInterval,
				MaxRetries:      cfg.News.MaxRetries,
				SummaryInputDir: summaryInputDir,
				MaxInputDataLen: cfg.News.MaxDataLen,
			},
		)
		fetcher = fetcher.New(
			articleStorage,
			sourceStorage,
			cfg.FetchInterval,
			cfg.FilterKeywords,
			rep,
			githubClient,
		)
	)

	// Fall back to the admin chat if no dedicated test channel is configured.
	testChannelID := cfg.Telegram.TestChannelID
	if testChannelID == 0 {
		testChannelID = cfg.Telegram.AdminChatID
	}

	newsBot := botkit.New(botAPI)

	linkPoster := poster.New(postSummarizer, botAPI, cfg.Telegram.ChannelID, cfg.Post.MaxContentLen)

	newsBot.RegisterCmdView(
		"post",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdPost(linkPoster, newsBot),
		),
	)
	newsBot.RegisterCmdView(
		"testnews",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdTestNews(notifier, testChannelID),
		),
	)
	newsBot.RegisterCmdView(
		"repostnews",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdRepostNews(notifier),
		),
	)
	newsBot.RegisterCmdView(
		"addsource",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdAddSource(sourceStorage, newsBot),
		),
	)
	newsBot.RegisterCmdView(
		"setpriority",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdSetPriority(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"getsource",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdGetSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"listsources",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdListSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"deletesource",
		middleware.AdminsOnly(
			cfg.Telegram.ChannelID,
			bot.ViewCmdDeleteSource(sourceStorage),
		),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()

		if err := db.PingContext(pingCtx); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	if cfg.Digest.Enabled {
		digest := digest.New(
			githubClient,
			telegraphClient,
			botAPI,
			repoStorage,
			digestSummarizer,
			digest.Config{
				ChannelID:       cfg.Telegram.ChannelID,
				Topics:          cfg.GitHub.Topics,
				Interval:        cfg.Digest.Interval,
				TopCount:        cfg.Digest.TopCount,
				SummaryInputDir: summaryInputDir,
			},
		)
		newsBot.RegisterCmdView(
			"testdigest",
			middleware.AdminsOnly(
				cfg.Telegram.ChannelID,
				bot.ViewCmdTestDigest(digest, testChannelID),
			),
		)
		go func(ctx context.Context) {
			if err := digest.Start(ctx); err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Error("digest stopped unexpectedly", "err", err)
					rep.Notify(fmt.Sprintf("Digest stopped: %v", err))
					return
				}
				slog.Error("digest stopped", "err", err)
			}
		}(ctx)
	}

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

	healthSrv := &http.Server{Addr: "127.0.0.1:8088", Handler: mux}

	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server stopped unexpectedly", "err", err)
		}
	}()

	go func(ctx context.Context) {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := healthSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("http server shutdown failed", "err", err)
		}
	}(ctx)

	if err := newsBot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("botkit stopped unexpectedly", "err", err)
	}
}
