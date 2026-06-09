package botkit

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type ViewFunc func(ctx context.Context, bot *tgbotapi.BotAPI, update tgbotapi.Update) error

type Bot struct {
	api         *tgbotapi.BotAPI
	cmdViews    map[string]ViewFunc
	msgHandlers map[int64]ViewFunc
	mu          sync.Mutex
}

func New(api *tgbotapi.BotAPI) *Bot {
	return &Bot{
		api:         api,
		msgHandlers: make(map[int64]ViewFunc),
	}
}

func (b *Bot) RegisterCmdView(cmd string, view ViewFunc) {
	if b.cmdViews == nil {
		b.cmdViews = make(map[string]ViewFunc)
	}

	b.cmdViews[cmd] = view
}

// RegisterMsgHandler registers a one-shot message handler for a specific chat.
// It will be called for the next non-command message received in that chat.
func (b *Bot) RegisterMsgHandler(chatID int64, handler ViewFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgHandlers[chatID] = handler
}

// ClearMsgHandler removes any pending message handler for a specific chat.
func (b *Bot) ClearMsgHandler(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.msgHandlers, chatID)
}

// updateTimeout bounds a single update's handling. It is generous because
// LLM-backed commands (/testnews, /testdigest) can run for many minutes; the
// summarizers enforce their own tighter timeouts on top.
const updateTimeout = 30 * time.Minute

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case update := <-updates:
			// Handle concurrently so a slow command (e.g. an LLM-backed test
			// digest) does not block every other command.
			go func(update tgbotapi.Update) {
				updateCtx, updateCancel := context.WithTimeout(ctx, updateTimeout)
				defer updateCancel()
				b.handleUpdate(updateCtx, update)
			}(update)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panic recovered", "panic", p, "stack", string(debug.Stack()))
		}
	}()

	if update.Message == nil && update.CallbackQuery == nil {
		return
	}

	if update.Message != nil && !update.Message.IsCommand() {
		b.mu.Lock()
		handler, ok := b.msgHandlers[update.Message.Chat.ID]
		b.mu.Unlock()

		if ok {
			if err := handler(ctx, b.api, update); err != nil {
				slog.Error("message handler failed", "err", err)
				if _, err := b.api.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Internal error")); err != nil {
					slog.Error("failed to send error reply", "err", err)
				}
			}
		}
		return
	}

	if update.Message == nil || !update.Message.IsCommand() {
		return
	}

	// A new command clears any pending conversation state for this chat.
	b.ClearMsgHandler(update.Message.Chat.ID)

	cmd := update.Message.Command()

	cmdView, ok := b.cmdViews[cmd]
	if !ok {
		return
	}

	if err := cmdView(ctx, b.api, update); err != nil {
		slog.Error("command view failed", "cmd", cmd, "err", err)

		if _, err := b.api.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Internal error")); err != nil {
			slog.Error("failed to send error reply", "err", err)
		}
	}
}
