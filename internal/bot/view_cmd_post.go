package bot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
)

// Poster prepares a post from a URL and publishes it to the channel.
type Poster interface {
	Prepare(ctx context.Context, pageURL, comment string) (string, error)
	Publish(text string) error
	SendTo(chatID int64, text string) error
}

// ViewCmdPost handles "/post <url> [comment]": fetches and summarizes the
// page, shows a preview in the issuing chat and publishes to the channel
// after the admin confirms with "yes".
func ViewCmdPost(poster Poster, b *botkit.Bot) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		chatID := update.Message.Chat.ID

		args := strings.Fields(update.Message.CommandArguments())
		if len(args) == 0 || (!strings.HasPrefix(args[0], "http://") && !strings.HasPrefix(args[0], "https://")) {
			_, err := api.Send(tgbotapi.NewMessage(chatID,
				"Использование: /post <url> [комментарий]\nКомментарий будет передан LLM как заметка редактора."))
			return err
		}
		pageURL := args[0]
		comment := strings.Join(args[1:], " ")

		if _, err := api.Send(tgbotapi.NewMessage(chatID, "Готовлю пост, это может занять пару минут...")); err != nil {
			return err
		}

		draft, err := poster.Prepare(ctx, pageURL, comment)
		if err != nil {
			_, sendErr := api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Не удалось подготовить пост: %v", err)))
			if sendErr != nil {
				return sendErr
			}
			return nil
		}

		if err := poster.SendTo(chatID, draft); err != nil {
			return err
		}
		if _, err := api.Send(tgbotapi.NewMessage(chatID,
			"Превью выше. Ответьте «yes», чтобы опубликовать в канал; любой другой ответ — отмена.")); err != nil {
			return err
		}

		b.RegisterMsgHandler(chatID, confirmPostHandler(poster, b, chatID, draft))
		return nil
	}
}

func confirmPostHandler(poster Poster, b *botkit.Bot, chatID int64, draft string) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		b.ClearMsgHandler(chatID)

		answer := strings.ToLower(strings.TrimSpace(update.Message.Text))
		if answer != "yes" && answer != "y" && answer != "да" {
			_, err := api.Send(tgbotapi.NewMessage(chatID, "Отменено."))
			return err
		}

		if err := poster.Publish(draft); err != nil {
			return err
		}

		_, err := api.Send(tgbotapi.NewMessage(chatID, "Опубликовано в канал."))
		return err
	}
}
