package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit"
)

type DigestRunner interface {
	RunTest(ctx context.Context, channelID int64) error
}

func ViewCmdTestDigest(d DigestRunner, testChannelID int64) botkit.ViewFunc {
	return func(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) error {
		chatID := update.Message.Chat.ID

		notice := tgbotapi.NewMessage(chatID, "Running test digest, please wait…")
		if _, err := api.Send(notice); err != nil {
			return err
		}

		if err := d.RunTest(ctx, testChannelID); err != nil {
			reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Test digest failed: %v", err))
			_, _ = api.Send(reply)
			return err
		}

		reply := tgbotapi.NewMessage(chatID, "Test digest sent.")
		_, _ = api.Send(reply)
		return nil
	}
}
