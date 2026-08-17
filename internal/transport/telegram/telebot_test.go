package telegram

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func TestTelebotAdapterMapsTextVoiceAndAudio(t *testing.T) {
	bot, err := tele.NewBot(tele.Settings{Token: "test", Offline: true, Synchronous: true})
	if err != nil {
		t.Fatalf("tele.NewBot() error = %v", err)
	}
	adapter := &telebotAdapter{bot: bot, token: "test", maxFileBytes: defaultMaxFileSize}
	var messages []Message
	adapter.Handle(func(message Message) { messages = append(messages, message) })

	bot.ProcessUpdate(tele.Update{Message: &tele.Message{
		ID: 1, Sender: &tele.User{ID: 42}, Chat: &tele.Chat{ID: 7, Type: tele.ChatPrivate}, Text: "/start",
	}})
	bot.ProcessUpdate(tele.Update{Message: &tele.Message{
		ID: 2, Sender: &tele.User{ID: 42}, Chat: &tele.Chat{ID: 7, Type: tele.ChatPrivate},
		Voice: &tele.Voice{File: tele.File{FileID: "voice", FileSize: 12}, Duration: 3, MIME: "audio/ogg"},
	}})
	bot.ProcessUpdate(tele.Update{Message: &tele.Message{
		ID: 3, Sender: &tele.User{ID: 42}, Chat: &tele.Chat{ID: 7, Type: tele.ChatPrivate},
		Audio: &tele.Audio{File: tele.File{FileID: "audio", FileSize: 15}, FileName: "meeting.mp3", Duration: 4, MIME: "audio/mpeg"},
	}})
	bot.ProcessUpdate(tele.Update{Message: &tele.Message{
		ID: 4, Sender: &tele.User{ID: 42}, Chat: &tele.Chat{ID: 7, Type: tele.ChatPrivate},
		Document: &tele.Document{File: tele.File{FileID: "document", FileSize: 20}, FileName: "meeting.flac", MIME: "audio/flac"},
	}})

	if len(messages) != 4 {
		t.Fatalf("mapped messages = %+v", messages)
	}
	if messages[0].Text != "/start" || messages[0].From == nil || messages[0].From.ID != 42 || messages[0].Chat.Type != "private" {
		t.Fatalf("mapped text message = %+v", messages[0])
	}
	if messages[1].Voice == nil || messages[1].Voice.FileID != "voice" || messages[1].Voice.Duration != 3 || messages[1].Voice.FileSize != 12 {
		t.Fatalf("mapped voice message = %+v", messages[1])
	}
	if messages[2].Audio == nil || messages[2].Audio.FileName != "meeting.mp3" || messages[2].Audio.MIMEType != "audio/mpeg" {
		t.Fatalf("mapped audio message = %+v", messages[2])
	}
	if messages[3].File == nil || messages[3].File.FileID != "document" || messages[3].File.MIMEType != "audio/flac" {
		t.Fatalf("mapped document message = %+v", messages[3])
	}
}

func TestRedactRemovesBotToken(t *testing.T) {
	const token = "123456:secret"
	if got := redact(token, "request https://api.telegram.org/bot"+token+"/getMe failed"); got == "" || strings.Contains(got, token) {
		t.Fatalf("redact() = %q", got)
	}
}
