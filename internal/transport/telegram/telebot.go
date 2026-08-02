package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
)

const (
	telebotPollTimeout = 5 * time.Second
	telebotHTTPTimeout = 8 * time.Second
	defaultMaxFileSize = 20 << 20
)

var errFileTooLarge = errors.New("Telegram file is too large")

// botAPI is the transport-side seam around telebot. Production uses
// telebotAdapter; listener and handler tests use an in-memory fake.
type botAPI interface {
	Handle(func(Message))
	Start()
	Stop()
	SendMessage(ctx context.Context, chatID int64, text string) error
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

type botFactory func() (botAPI, error)

type telebotAdapter struct {
	bot          *tele.Bot
	token        string
	maxFileBytes int64
}

func newTelebotAdapter(token string, logger *slog.Logger) (botAPI, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("Telegram token is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	bot, err := tele.NewBot(tele.Settings{
		Token: token,
		Poller: &tele.LongPoller{
			Timeout:        telebotPollTimeout,
			AllowedUpdates: []string{"message"},
		},
		Synchronous: true,
		Client:      &http.Client{Timeout: telebotHTTPTimeout},
		OnError: func(err error, _ tele.Context) {
			logger.Error("telebot handler failed", "error", redact(token, err.Error()))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize telebot: %s", redact(token, err.Error()))
	}
	return &telebotAdapter{bot: bot, token: token, maxFileBytes: defaultMaxFileSize}, nil
}

func (a *telebotAdapter) Handle(handler func(Message)) {
	callback := func(ctx tele.Context) error {
		if message := ctx.Message(); message != nil {
			handler(fromTelebotMessage(message))
		}
		return nil
	}
	for _, endpoint := range []string{tele.OnText, tele.OnVoice, tele.OnAudio, tele.OnDocument} {
		a.bot.Handle(endpoint, callback)
	}
}

func (a *telebotAdapter) Start() { a.bot.Start() }

func (a *telebotAdapter) Stop() { a.bot.Stop() }

func (a *telebotAdapter) SendMessage(ctx context.Context, chatID int64, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := a.bot.Send(tele.ChatID(chatID), text)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("telebot send: %s", redact(a.token, err.Error()))
	}
	return nil
}

func (a *telebotAdapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := a.bot.FileByID(fileID)
	if err != nil {
		return nil, fmt.Errorf("telebot get file: %s", redact(a.token, err.Error()))
	}
	if file.FileSize > a.maxFileBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", errFileTooLarge, a.maxFileBytes)
	}
	reader, err := a.bot.File(&file)
	if err != nil {
		return nil, fmt.Errorf("telebot download file: %s", redact(a.token, err.Error()))
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, a.maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Telegram file: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if int64(len(content)) > a.maxFileBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", errFileTooLarge, a.maxFileBytes)
	}
	return content, nil
}

func fromTelebotMessage(source *tele.Message) Message {
	message := Message{ID: int64(source.ID), Text: source.Text}
	if source.Chat != nil {
		message.Chat = Chat{ID: source.Chat.ID, Type: string(source.Chat.Type)}
	}
	if source.Sender != nil {
		message.From = &User{ID: source.Sender.ID, IsBot: source.Sender.IsBot}
	}
	if source.Voice != nil {
		message.Voice = &VoiceFile{
			FileID:   source.Voice.FileID,
			Duration: source.Voice.Duration,
			MIMEType: source.Voice.MIME,
			FileSize: source.Voice.FileSize,
		}
	}
	if source.Audio != nil {
		message.Audio = &AudioFile{
			FileID:   source.Audio.FileID,
			FileName: source.Audio.FileName,
			Duration: source.Audio.Duration,
			MIMEType: source.Audio.MIME,
			FileSize: source.Audio.FileSize,
		}
	}
	if source.Document != nil {
		message.File = &DocumentFile{
			FileID:   source.Document.FileID,
			FileName: source.Document.FileName,
			MIMEType: source.Document.MIME,
			FileSize: source.Document.FileSize,
		}
	}
	return message
}

func redact(secret, value string) string {
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}
