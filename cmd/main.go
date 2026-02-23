// Copyright (c) 2024, 0x0BSoD. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"context"
	"errors"
	"log"
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
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/summary"
)

func main() {
	botAPI, err := tgbotapi.NewBotAPI(config.Get().TelegramBotToken)
	if err != nil {
		log.Printf("[ERROR] failed to create botAPI: %v", err)
		return
	}

	db, err := sqlx.Connect("postgres", config.Get().DatabaseDSN)
	if err != nil {
		log.Printf("[ERROR] failed to connect to db: %v", err)
		return
	}
	defer db.Close()

	var (
		articleStorage = storage.NewArticleStorage(db)
		sourceStorage  = storage.NewSourceStorage(db)
		fetcher        = fetcher.New(
			articleStorage,
			sourceStorage,
			config.Get().FetchInterval,
			config.Get().FilterKeywords,
		)
	)

	var summarizer notifier.Summarizer
	switch config.Get().AIType {
	case "openai":
		if config.Get().AIKey == "" {
			log.Printf("[ERROR] ai_key is required when ai_type is \"openai\"")
			return
		}
		summarizer = summary.NewOpenAISummarizer(
			config.Get().AIBaseURL,
			config.Get().AIKey,
			config.Get().AIPrompt,
			config.Get().AIModel,
			config.Get().AITimeout,
		)
		log.Printf("[INFO] using OpenAI-compatible summarizer (model: %s)", config.Get().AIModel)
	default:
		if config.Get().AIBaseURL == "" {
			log.Printf("[ERROR] ai_base_url is required when ai_type is \"ollama\"")
			return
		}
		summarizer = summary.NewOllamaSummarizer(
			config.Get().AIBaseURL,
			config.Get().AIPrompt,
			config.Get().AIModel,
			config.Get().AITimeout,
		)
		log.Printf("[INFO] using Ollama summarizer (model: %s)", config.Get().AIModel)
	}

	var (
		notifier = notifier.New(
			articleStorage,
			sourceStorage,
			summarizer,
			botAPI,
			config.Get().NotificationInterval,
			2*config.Get().FetchInterval,
			config.Get().TelegramChannelID,
		)
	)

	newsBot := botkit.New(botAPI)
	newsBot.RegisterCmdView(
		"addsource",
		middleware.AdminsOnly(
			config.Get().TelegramChannelID,
			bot.ViewCmdAddSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"setpriority",
		middleware.AdminsOnly(
			config.Get().TelegramChannelID,
			bot.ViewCmdSetPriority(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"getsource",
		middleware.AdminsOnly(
			config.Get().TelegramChannelID,
			bot.ViewCmdGetSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"listsources",
		middleware.AdminsOnly(
			config.Get().TelegramChannelID,
			bot.ViewCmdListSource(sourceStorage),
		),
	)
	newsBot.RegisterCmdView(
		"deletesource",
		middleware.AdminsOnly(
			config.Get().TelegramChannelID,
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
				log.Printf("[ERROR] failed to run fetcher: %v", err)
				return
			}

			log.Printf("[INFO] fetcher stopped")
		}
	}(ctx)

	go func(ctx context.Context) {
		if err := notifier.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("[ERROR] failed to run notifier: %v", err)
				return
			}

			log.Printf("[INFO] notifier stopped")
		}
	}(ctx)

	go func(ctx context.Context) {
		if err := http.ListenAndServe("127.0.0.1:8088", mux); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("[ERROR] failed to run http server: %v", err)
				return
			}

			log.Printf("[INFO] http server stopped")
		}
	}(ctx)

	if err := newsBot.Run(ctx); err != nil {
		log.Printf("[ERROR] failed to run botkit: %v", err)
	}
}
