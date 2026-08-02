package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

func TestHandlerRegistersTelegramUser(t *testing.T) {
	application := &fakeApplication{}
	handler := newTestHandler(t, application, &fakeDownloader{})

	replies := handler.Handle(context.Background(), Message{
		Chat: Chat{ID: 7}, From: &User{ID: 42}, Text: "/start",
	})

	if application.registeredUserID != "telegram:42" {
		t.Fatalf("RegisterUser() external ID = %q", application.registeredUserID)
	}
	assertReplyContains(t, replies, "Вы зарегистрированы")
}

func TestHandlerRoutesVoiceUploadThroughApplication(t *testing.T) {
	downloader := &fakeDownloader{content: []byte("voice bytes")}
	application := &fakeApplication{
		uploadResult: ports.UploadMeetingResult{
			Meeting: domain.Meeting{ID: "meeting-voice"},
			Job:     domain.ProcessingJob{Status: domain.StatusCreated},
		},
	}
	handler := newTestHandler(t, application, downloader)

	replies := handler.Handle(context.Background(), Message{
		Chat: Chat{ID: 7},
		From: &User{ID: 42},
		Voice: &VoiceFile{
			FileID: "telegram-file", Duration: 17, MIMEType: "audio/ogg", FileSize: 100,
		},
	})

	request := application.uploadRequest
	if downloader.fileID != "telegram-file" || request.ExternalUserID != "telegram:42" {
		t.Fatalf("download file=%q upload user=%q", downloader.fileID, request.ExternalUserID)
	}
	if request.File.ExternalFileID != "telegram-file" || request.File.FileName != "voice.ogg" || request.File.MIMEType != "audio/ogg" {
		t.Fatalf("upload metadata = %+v", request.File)
	}
	if request.File.Duration != 17*time.Second || request.File.SizeBytes != int64(len("voice bytes")) || string(request.Content) != "voice bytes" {
		t.Fatalf("upload request = %+v", request)
	}
	assertReplyContains(t, replies, "meeting-voice")
}

func TestHandlerRoutesAudioUploadAndAppliesFallbacks(t *testing.T) {
	downloader := &fakeDownloader{content: []byte("audio")}
	application := &fakeApplication{
		uploadResult: ports.UploadMeetingResult{
			Meeting: domain.Meeting{ID: "meeting-audio"},
			Job:     domain.ProcessingJob{Status: domain.StatusCreated},
		},
	}
	handler := newTestHandler(t, application, downloader)

	handler.Handle(context.Background(), Message{
		From:  &User{ID: 9},
		Audio: &AudioFile{FileID: "audio-file", Duration: 3},
	})

	if application.uploadRequest.File.FileName != "audio" || application.uploadRequest.File.MIMEType != "audio/octet-stream" {
		t.Fatalf("fallback metadata = %+v", application.uploadRequest.File)
	}
}

func TestHandlerAcceptsAudioDocumentAndRejectsOtherDocuments(t *testing.T) {
	downloader := &fakeDownloader{content: []byte("audio")}
	application := &fakeApplication{
		uploadResult: ports.UploadMeetingResult{Meeting: domain.Meeting{ID: "meeting-document"}, Job: domain.ProcessingJob{Status: domain.StatusCreated}},
	}
	handler := newTestHandler(t, application, downloader)

	replies := handler.Handle(context.Background(), Message{
		From: &User{ID: 9},
		File: &DocumentFile{FileID: "document-audio", FileName: "meeting.flac", MIMEType: "audio/flac"},
	})
	assertReplyContains(t, replies, "meeting-document")
	if application.uploadRequest.File.FileName != "meeting.flac" || application.uploadRequest.File.MIMEType != "audio/flac" {
		t.Fatalf("document metadata = %+v", application.uploadRequest.File)
	}

	downloader.calls = 0
	replies = handler.Handle(context.Background(), Message{
		From: &User{ID: 9},
		File: &DocumentFile{FileID: "text", FileName: "notes.txt", MIMEType: "text/plain"},
	})
	assertReplyContains(t, replies, "не поддерживается")
	if downloader.calls != 0 {
		t.Fatalf("unsupported document downloads = %d", downloader.calls)
	}
}

func TestHandlerRoutesMandatoryCommands(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	application := &fakeApplication{
		listResult: []domain.MeetingRecord{{
			Meeting: domain.Meeting{ID: "meeting-list", CreatedAt: now, File: domain.FileMetadata{FileName: "planning.ogg"}},
			Job:     domain.ProcessingJob{Status: domain.StatusCompleted},
			Summary: &domain.Summary{Text: "Краткая выжимка"},
		}},
		statusResult:     domain.ProcessingJob{MeetingID: "meeting-status", Status: domain.StatusProcessing, CreatedAt: now, UpdatedAt: now},
		transcriptResult: domain.Transcript{MeetingID: "meeting-get", Text: "Полная транскрипция"},
		findResult: []domain.SearchResult{{
			Record:  domain.MeetingRecord{Meeting: domain.Meeting{ID: "meeting-find", CreatedAt: now}, Job: domain.ProcessingJob{Status: domain.StatusCompleted}},
			Snippet: "Найденный фрагмент",
		}},
		chatResult: "Подготовленный ответ",
	}
	handler := newTestHandler(t, application, &fakeDownloader{})

	tests := []struct {
		input string
		want  string
	}{
		{input: "list", want: "meeting-list"},
		{input: "status meeting-status", want: "Статус: processing"},
		{input: "get meeting-get", want: "Полная транскрипция"},
		{input: "find запуск", want: "meeting-find"},
		{input: "chat meeting-chat Что решили?", want: "Подготовленный ответ"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			replies := handler.Handle(context.Background(), Message{From: &User{ID: 42}, Text: test.input})
			assertReplyContains(t, replies, test.want)
		})
	}

	if application.listUserID != "telegram:42" || application.statusMeetingID != "meeting-status" || application.transcriptMeetingID != "meeting-get" {
		t.Fatalf("routed IDs: list=%q status=%q get=%q", application.listUserID, application.statusMeetingID, application.transcriptMeetingID)
	}
	if application.findKeyword != "запуск" || application.chatMeetingID != "meeting-chat" || application.chatQuestion != "Что решили?" {
		t.Fatalf("find/chat routing: keyword=%q meeting=%q question=%q", application.findKeyword, application.chatMeetingID, application.chatQuestion)
	}
}

func TestHandlerMapsErrorsAndRejectsUnsupportedMessages(t *testing.T) {
	application := &fakeApplication{statusErr: fmt.Errorf("lookup: %w", domain.ErrNotFound)}
	handler := newTestHandler(t, application, &fakeDownloader{})

	assertReplyContains(t, handler.Handle(context.Background(), Message{From: &User{ID: 1}, Text: "status hidden"}), "Встреча не найдена")
	assertReplyContains(t, handler.Handle(context.Background(), Message{From: &User{ID: 1}, Text: "status"}), "Неверный формат")
	assertReplyContains(t, handler.Handle(context.Background(), Message{From: &User{ID: 1}, Text: "unknown"}), "Неизвестная команда")
	assertReplyContains(t, handler.Handle(context.Background(), Message{From: &User{ID: 1}}), "Поддерживаются")

	if replies := handler.Handle(context.Background(), Message{From: &User{ID: 1, IsBot: true}, Text: "/start"}); len(replies) != 0 {
		t.Fatalf("bot message replies = %v", replies)
	}
	assertReplyContains(t, handler.Handle(context.Background(), Message{Chat: Chat{Type: "group"}, From: &User{ID: 1}, Text: "list"}), "личном чате")
}

func TestHandlerRejectsOversizedFileBeforeDownload(t *testing.T) {
	downloader := &fakeDownloader{}
	handler := newTestHandler(t, &fakeApplication{}, downloader)

	replies := handler.Handle(context.Background(), Message{
		From:  &User{ID: 1},
		Audio: &AudioFile{FileID: "large", FileSize: defaultMaxFileSize + 1},
	})

	assertReplyContains(t, replies, "слишком большой")
	if downloader.calls != 0 {
		t.Fatalf("DownloadFile() calls = %d", downloader.calls)
	}
}

func TestHandlerPreservesMeetingIDWhenQueueIsFull(t *testing.T) {
	application := &fakeApplication{
		uploadResult: ports.UploadMeetingResult{Meeting: domain.Meeting{ID: "failed-meeting"}},
		uploadErr:    domain.ErrQueueFull,
	}
	handler := newTestHandler(t, application, &fakeDownloader{content: []byte("audio")})

	replies := handler.Handle(context.Background(), Message{
		From:  &User{ID: 1},
		Voice: &VoiceFile{FileID: "file"},
	})

	assertReplyContains(t, replies, "failed-meeting")
	assertReplyContains(t, replies, "очередь обработки заполнена")
}

func TestSplitTelegramTextHonorsRuneLimit(t *testing.T) {
	input := strings.Repeat("я", maxTelegramMessageRunes*2+1)
	parts := splitTelegramText(input)
	if len(parts) != 3 {
		t.Fatalf("parts = %d", len(parts))
	}
	if strings.Join(parts, "") != input {
		t.Fatal("split text did not preserve Unicode input")
	}
	for _, part := range parts {
		if len([]rune(part)) > maxTelegramMessageRunes {
			t.Fatalf("part has %d runes", len([]rune(part)))
		}
	}
}

func newTestHandler(t *testing.T, application ports.MeetingApplication, downloader fileDownloader) *Handler {
	t.Helper()
	handler, err := NewHandler(application, downloader, testTelegramLogger())
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func assertReplyContains(t *testing.T, replies []string, want string) {
	t.Helper()
	if !strings.Contains(strings.Join(replies, "\n"), want) {
		t.Fatalf("replies = %q, want substring %q", replies, want)
	}
}

type fakeDownloader struct {
	fileID  string
	content []byte
	err     error
	calls   int
}

func (f *fakeDownloader) DownloadFile(_ context.Context, fileID string) ([]byte, error) {
	f.calls++
	f.fileID = fileID
	return append([]byte(nil), f.content...), f.err
}

type fakeApplication struct {
	registeredUserID string
	registerErr      error

	uploadRequest ports.UploadMeetingRequest
	uploadResult  ports.UploadMeetingResult
	uploadErr     error
	uploadHook    func()

	listUserID string
	listResult []domain.MeetingRecord
	listErr    error

	statusUserID    string
	statusMeetingID domain.MeetingID
	statusResult    domain.ProcessingJob
	statusErr       error

	transcriptUserID    string
	transcriptMeetingID domain.MeetingID
	transcriptResult    domain.Transcript
	transcriptErr       error

	findUserID  string
	findKeyword string
	findResult  []domain.SearchResult
	findErr     error

	chatUserID    string
	chatMeetingID domain.MeetingID
	chatQuestion  string
	chatResult    string
	chatErr       error
}

func (f *fakeApplication) RegisterUser(_ context.Context, externalUserID string) (domain.User, error) {
	f.registeredUserID = externalUserID
	return domain.User{ExternalID: externalUserID}, f.registerErr
}

func (f *fakeApplication) UploadMeeting(_ context.Context, request ports.UploadMeetingRequest) (ports.UploadMeetingResult, error) {
	f.uploadRequest = request
	if f.uploadHook != nil {
		f.uploadHook()
	}
	return f.uploadResult, f.uploadErr
}

func (f *fakeApplication) ListMeetings(_ context.Context, externalUserID string) ([]domain.MeetingRecord, error) {
	f.listUserID = externalUserID
	return f.listResult, f.listErr
}

func (f *fakeApplication) GetStatus(_ context.Context, externalUserID string, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	f.statusUserID = externalUserID
	f.statusMeetingID = meetingID
	return f.statusResult, f.statusErr
}

func (f *fakeApplication) GetTranscript(_ context.Context, externalUserID string, meetingID domain.MeetingID) (domain.Transcript, error) {
	f.transcriptUserID = externalUserID
	f.transcriptMeetingID = meetingID
	return f.transcriptResult, f.transcriptErr
}

func (f *fakeApplication) FindMeetings(_ context.Context, externalUserID, keyword string) ([]domain.SearchResult, error) {
	f.findUserID = externalUserID
	f.findKeyword = keyword
	return f.findResult, f.findErr
}

func (f *fakeApplication) Chat(_ context.Context, externalUserID string, meetingID domain.MeetingID, question string) (string, error) {
	f.chatUserID = externalUserID
	f.chatMeetingID = meetingID
	f.chatQuestion = question
	return f.chatResult, f.chatErr
}

func TestUserErrorHandlesDownloadFailureWithoutLeakingDetails(t *testing.T) {
	handler := newTestHandler(t, &fakeApplication{}, &fakeDownloader{err: errors.New("secret internal URL")})
	replies := handler.Handle(context.Background(), Message{From: &User{ID: 1}, Voice: &VoiceFile{FileID: "file"}})
	if strings.Contains(strings.Join(replies, " "), "secret internal URL") {
		t.Fatalf("reply leaked internal error: %v", replies)
	}
}
