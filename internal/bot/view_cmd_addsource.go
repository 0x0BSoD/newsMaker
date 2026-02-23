package bot

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/SlyMarbo/rss"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
	"github.com/0x0BSoD/newsMaker/internal/model"
)

const feedProbeTimeout = 15 * time.Second

type SourceStorage interface {
	Add(ctx context.Context, source model.Source) (int64, error)
}

func ViewCmdAddSource(storage SourceStorage) botkit.ViewFunc {
	type addSourceArgs struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Priority int    `json:"priority"`
		Insecure bool   `json:"insecure"`
	}

	return func(ctx context.Context, bot *tgbotapi.BotAPI, update tgbotapi.Update) error {
		args, err := botkit.ParseJSON[addSourceArgs](update.Message.CommandArguments())
		if err != nil {
			return err
		}

		probeClient := &http.Client{Timeout: feedProbeTimeout}
		if args.Insecure {
			probeClient.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			}
		}
		if _, err := rss.FetchByClient(args.URL, probeClient); err != nil {
			reply := tgbotapi.NewMessage(update.Message.Chat.ID,
				fmt.Sprintf("Не удалось получить фид по указанному URL: %v", err))
			_, _ = bot.Send(reply)
			return nil
		}

		source := model.Source{
			Name:     args.Name,
			FeedURL:  args.URL,
			Priority: args.Priority,
			Insecure: args.Insecure,
		}

		sourceID, err := storage.Add(ctx, source)
		if err != nil {
			return err
		}

		var (
			msgText = fmt.Sprintf(
				"Источник добавлен с ID: `%d`\\. Используйте этот ID для обновления источника или удаления\\.",
				sourceID,
			)
			reply = tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
		)

		reply.ParseMode = parseModeMarkdownV2

		if _, err := bot.Send(reply); err != nil {
			return err
		}

		return nil
	}
}
