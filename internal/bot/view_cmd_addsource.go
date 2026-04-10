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

		b.RegisterMsgHandler(chatID, askTypeHandler(storage, b, chatID))
		return nil
	}
}

func askTypeHandler(storage SourceStorage, b *botkit.Bot, chatID int64) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		name := update.Message.Text

		msg := tgbotapi.NewMessage(chatID, "Выберите тип источника: rss или web")
		if _, err := api.Send(msg); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, askURLHandler(storage, b, chatID, name))
		return nil
	}
}

func askURLHandler(storage SourceStorage, b *botkit.Bot, chatID int64, name string) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		sourceType := update.Message.Text
		if sourceType != model.SourceTypeRSS && sourceType != model.SourceTypeWeb {
			reply := tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Неверный тип %q. Введите rss или web:", sourceType))
			_, _ = api.Send(reply)
			b.RegisterMsgHandler(chatID, askURLHandler(storage, b, chatID, name))
			return nil
		}

		prompt := "Введите URL RSS-фида:"
		if sourceType == model.SourceTypeWeb {
			prompt = "Введите URL страницы-листинга (например, https://platformengineering.org/blog):"
		}
		if _, err := api.Send(tgbotapi.NewMessage(chatID, prompt)); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, saveSourceHandler(storage, b, chatID, name, sourceType))
		return nil
	}
}

func saveSourceHandler(storage SourceStorage, b *botkit.Bot, chatID int64, name, sourceType string) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		feedURL := update.Message.Text

		source := model.Source{
			Name:       name,
			FeedURL:    feedURL,
			SourceType: sourceType,
		}

		switch sourceType {
		case model.SourceTypeRSS:
			probeClient := &http.Client{Timeout: feedProbeTimeout}
			if _, err := rss.FetchByClient(feedURL, probeClient); err != nil {
				reply := tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Не удалось получить фид по указанному URL: %v\n\nВведите другой URL или отправьте команду для отмены.", err))
				_, _ = api.Send(reply)
				b.RegisterMsgHandler(chatID, saveSourceHandler(storage, b, chatID, name, sourceType))
				return nil
			}

		case model.SourceTypeWeb:
			if _, err := api.Send(tgbotapi.NewMessage(chatID,
				"Введите CSS-селектор для ссылок (например, a[href^='/blog/']):")); err != nil {
				return err
			}
			b.RegisterMsgHandler(chatID, saveWebSourceHandler(storage, b, chatID, source))
			return nil
		}

		return storeSource(ctx, api, storage, b, chatID, source)
	}
}

func saveWebSourceHandler(storage SourceStorage, b *botkit.Bot, chatID int64, source model.Source) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		linkSelector := update.Message.Text

		if _, err := api.Send(tgbotapi.NewMessage(chatID,
			"Введите базовый URL (например, https://platformengineering.org):")); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, saveWebSourceBaseURLHandler(storage, b, chatID, source, linkSelector))
		return nil
	}
}

func saveWebSourceBaseURLHandler(storage SourceStorage, b *botkit.Bot, chatID int64, source model.Source, linkSelector string) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		baseURL := update.Message.Text

		probeClient := &http.Client{Timeout: feedProbeTimeout}
		resp, err := probeClient.Get(source.FeedURL) //nolint:noctx
		if err != nil || resp.StatusCode != http.StatusOK {
			errMsg := fmt.Sprintf("URL недоступен: %v", err)
			if err == nil {
				errMsg = fmt.Sprintf("URL вернул статус %d", resp.StatusCode)
			}
			reply := tgbotapi.NewMessage(chatID, errMsg+"\n\nВведите другой URL или отправьте команду для отмены.")
			_, _ = api.Send(reply)
			b.RegisterMsgHandler(chatID, saveWebSourceBaseURLHandler(storage, b, chatID, source, linkSelector))
			return nil
		}
		resp.Body.Close()

		source.ScraperConfig = &model.ScraperConfig{
			LinkSelector: linkSelector,
			BaseURL:      baseURL,
		}

		return storeSource(ctx, api, storage, b, chatID, source)
	}
}

func storeSource(ctx context.Context, api *tgbotapi.BotAPI, storage SourceStorage, b *botkit.Bot, chatID int64, source model.Source) error {
	sourceID, err := storage.Add(ctx, source)
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
