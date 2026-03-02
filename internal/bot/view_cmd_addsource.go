package bot

import (
	"context"
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

func ViewCmdAddSource(storage SourceStorage, b *botkit.Bot) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		chatID := update.Message.Chat.ID

		if _, err := api.Send(tgbotapi.NewMessage(chatID, "Введите название источника:")); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, askURLHandler(storage, b, chatID))
		return nil
	}
}

func askURLHandler(storage SourceStorage, b *botkit.Bot, chatID int64) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		name := update.Message.Text

		if _, err := api.Send(tgbotapi.NewMessage(chatID, "Введите URL RSS-фида:")); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, saveSourceHandler(storage, b, chatID, name))
		return nil
	}
}

func saveSourceHandler(storage SourceStorage, b *botkit.Bot, chatID int64, name string) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		feedURL := update.Message.Text

		probeClient := &http.Client{Timeout: feedProbeTimeout}
		if _, err := rss.FetchByClient(feedURL, probeClient); err != nil {
			// Ask for URL again on bad feed.
			reply := tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Не удалось получить фид по указанному URL: %v\n\nВведите другой URL или отправьте команду для отмены.", err))
			_, _ = api.Send(reply)
			b.RegisterMsgHandler(chatID, saveSourceHandler(storage, b, chatID, name))
			return nil
		}

		sourceID, err := storage.Add(ctx, model.Source{Name: name, FeedURL: feedURL})
		if err != nil {
			b.ClearMsgHandler(chatID)
			return err
		}

		b.ClearMsgHandler(chatID)

		msgText := fmt.Sprintf(
			"Источник добавлен с ID: `%d`\\. Используйте этот ID для обновления источника или удаления\\.",
			sourceID,
		)
		reply := tgbotapi.NewMessage(chatID, msgText)
		reply.ParseMode = parseModeMarkdownV2

		if _, err := api.Send(reply); err != nil {
			return err
		}

		return nil
	}
}
