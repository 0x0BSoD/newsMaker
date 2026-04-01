package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
)

type NewsDigestRunner interface {
	SendTestDigest(ctx context.Context, channelID int64) error
}

func ViewCmdTestNews(n NewsDigestRunner, testChannelID int64) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		chatID := update.Message.Chat.ID

		if _, err := api.Send(tgbotapi.NewMessage(chatID, "Running test news digest, please wait…")); err != nil {
			return err
		}

		if err := n.SendTestDigest(ctx, testChannelID); err != nil {
			reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Test news digest failed: %v", err))
			_, _ = api.Send(reply)
			return err
		}

		_, _ = api.Send(tgbotapi.NewMessage(chatID, "Test news digest sent."))
		return nil
	}
}
