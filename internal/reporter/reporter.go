package reporter

import (
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Reporter sends short error notification messages to a Telegram admin chat.
// It is nil-safe: if adminID is 0 or the receiver is nil, Notify is a no-op.
type Reporter struct {
	bot     *tgbotapi.BotAPI
	adminID int64
}

func New(bot *tgbotapi.BotAPI, adminID int64) *Reporter {
	return &Reporter{bot: bot, adminID: adminID}
}

func (r *Reporter) Notify(msg string) {
	if r == nil || r.adminID == 0 {
		return
	}
	if _, err := r.bot.Send(tgbotapi.NewMessage(r.adminID, msg)); err != nil {
		slog.Error("failed to send error notification", "err", err)
	}
}
