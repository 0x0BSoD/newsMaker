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
	"syscall"

	"github.com/0x0BSoD/newsMaker/internal/bot"
	"github.com/0x0BSoD/newsMaker/internal/bot/middleware"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
	"github.com/0x0BSoD/newsMaker/internal/config"
	"github.com/0x0BSoD/newsMaker/internal/fetcher"
	"github.com/0x0BSoD/newsMaker/internal/notifier"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/summary"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Get()

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

	rep := reporter.New(botAPI, cfg.TelegramAdminChatID)

	var (
		articleStorage = storage.NewArticleStorage(db)
		sourceStorage  = storage.NewSourceStorage(db)
		fetcher        = fetcher.New(
			articleStorage,
			sourceStorage,
			cfg.FetchInterval,
			cfg.FilterKeywords,
			rep,
		)
	)

	var summarizer notifier.Summarizer
	switch cfg.AIType {
	case "openai":
		if cfg.AIKey == "" {
			slog.Error("ai_key is required when ai_type is openai")
			return
		}
		summarizer = summary.NewOpenAISummarizer(
			cfg.AIBaseURL,
			cfg.AIKey,
			cfg.AIPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		slog.Info("summarizer ready", "type", "openai", "model", cfg.AIModel)
	default:
		if cfg.AIBaseURL == "" {
			slog.Error("ai_base_url is required when ai_type is ollama")
			return
		}
		summarizer = summary.NewOllamaSummarizer(
			cfg.AIBaseURL,
			cfg.AIPrompt,
			cfg.AIModel,
			cfg.AITimeout,
		)
		slog.Info("summarizer ready", "type", "ollama", "model", cfg.AIModel)
	}

	var (
		notifier = notifier.New(
			articleStorage,
			sourceStorage,
			summarizer,
			botAPI,
			cfg.NotificationInterval,
			2*cfg.FetchInterval,
			cfg.TelegramChannelID,
			rep,
		)
	)

	newsBot := botkit.New(botAPI)
	newsBot.RegisterCmdView(
		"addsource",
		middleware.AdminsOnly(
			cfg.TelegramChannelID,
			bot.ViewCmdAddSource(sourceStorage),
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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
