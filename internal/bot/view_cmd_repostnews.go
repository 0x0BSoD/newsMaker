package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
)

type NewsReposter interface {
	Repost(ctx context.Context) error
}

func ViewCmdRepostNews(n NewsReposter) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		chatID := update.Message.Chat.ID

		if _, err := api.Send(tgbotapi.NewMessage(chatID, "Reposting news digest to the channel…")); err != nil {
			return err
		}

		if err := n.Repost(ctx); err != nil {
			reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Repost failed: %v", err))
			_, _ = api.Send(reply)
			return err
		}

		_, _ = api.Send(tgbotapi.NewMessage(chatID, "News digest reposted."))
		return nil
	}
}
