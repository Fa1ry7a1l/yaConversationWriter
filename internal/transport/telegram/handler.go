package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

const maxTelegramMessageRunes = 4000

type fileDownloader interface {
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

type Handler struct {
	application ports.MeetingApplication
	downloader  fileDownloader
	logger      *slog.Logger
}

func NewHandler(application ports.MeetingApplication, downloader fileDownloader, logger *slog.Logger) (*Handler, error) {
	if application == nil {
		return nil, errors.New("meeting application is required")
	}
	if downloader == nil {
		return nil, errors.New("Telegram file downloader is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Handler{application: application, downloader: downloader, logger: logger}, nil
}

// Handle converts one Telegram message into calls to the transport-facing
// application contract. It returns plain-text replies for the listener to send.
func (h *Handler) Handle(ctx context.Context, message Message) []string {
	if message.Chat.Type != "" && message.Chat.Type != "private" {
		return []string{"Для защиты данных используйте бота только в личном чате."}
	}
	if message.From == nil {
		return []string{"Не удалось определить отправителя сообщения."}
	}
	if message.From.IsBot {
		return nil
	}
	externalUserID := telegramUserID(message.From.ID)

	switch {
	case message.Voice != nil:
		return h.handleVoice(ctx, externalUserID, *message.Voice)
	case message.Audio != nil:
		return h.handleAudio(ctx, externalUserID, *message.Audio)
	case message.File != nil:
		return h.handleDocument(ctx, externalUserID, *message.File)
	case strings.TrimSpace(message.Text) != "":
		return h.handleCommand(ctx, externalUserID, message.Text)
	default:
		return []string{"Поддерживаются текстовые команды, голосовые сообщения и аудиофайлы.\n\n" + helpText()}
	}
}

func (h *Handler) handleCommand(ctx context.Context, externalUserID, text string) []string {
	command, err := ParseCommand(text)
	if err != nil {
		if errors.Is(err, ErrUnknownCommand) {
			return []string{"Неизвестная команда.\n\n" + helpText()}
		}
		return []string{commandUsage(err) + "\n\n" + helpText()}
	}

	switch command.Kind {
	case CommandStart:
		if _, err := h.application.RegisterUser(ctx, externalUserID); err != nil {
			return h.errorReply("register", err)
		}
		return []string{"Вы зарегистрированы. Отправьте голосовое сообщение или аудиофайл — обработка начнётся в фоне.\n\n" + helpText()}
	case CommandHelp:
		return []string{helpText()}
	case CommandList:
		records, err := h.application.ListMeetings(ctx, externalUserID)
		if err != nil {
			return h.errorReply("list", err)
		}
		return splitTelegramText(formatMeetingList(records))
	case CommandStatus:
		job, err := h.application.GetStatus(ctx, externalUserID, domain.MeetingID(command.MeetingID))
		if err != nil {
			return h.errorReply("status", err)
		}
		return []string{formatStatus(job)}
	case CommandGet:
		transcript, err := h.application.GetTranscript(ctx, externalUserID, domain.MeetingID(command.MeetingID))
		if err != nil {
			return h.errorReply("get", err)
		}
		return splitTelegramText(fmt.Sprintf("Транскрипция %s:\n%s", transcript.MeetingID, transcript.Text))
	case CommandFind:
		results, err := h.application.FindMeetings(ctx, externalUserID, command.Text)
		if err != nil {
			return h.errorReply("find", err)
		}
		return splitTelegramText(formatSearchResults(results))
	case CommandChat:
		answer, err := h.application.Chat(ctx, externalUserID, domain.MeetingID(command.MeetingID), command.Text)
		if err != nil {
			return h.errorReply("chat", err)
		}
		return splitTelegramText("Ответ:\n" + answer)
	default:
		return []string{"Неизвестная команда.\n\n" + helpText()}
	}
}

func (h *Handler) handleVoice(ctx context.Context, externalUserID string, voice VoiceFile) []string {
	mimeType := strings.TrimSpace(voice.MIMEType)
	if mimeType == "" {
		mimeType = "audio/ogg"
	}
	return h.upload(ctx, externalUserID, uploadFile{
		fileID:   voice.FileID,
		fileName: "voice.ogg",
		mimeType: mimeType,
		duration: time.Duration(voice.Duration) * time.Second,
		fileSize: voice.FileSize,
	})
}

func (h *Handler) handleAudio(ctx context.Context, externalUserID string, audio AudioFile) []string {
	fileName := strings.TrimSpace(audio.FileName)
	if fileName == "" {
		fileName = "audio"
	}
	mimeType := strings.TrimSpace(audio.MIMEType)
	if mimeType == "" {
		mimeType = "audio/octet-stream"
	}
	return h.upload(ctx, externalUserID, uploadFile{
		fileID:   audio.FileID,
		fileName: fileName,
		mimeType: mimeType,
		duration: time.Duration(audio.Duration) * time.Second,
		fileSize: audio.FileSize,
	})
}

func (h *Handler) handleDocument(ctx context.Context, externalUserID string, file DocumentFile) []string {
	if !isAudioMIME(file.MIMEType) {
		return []string{"Этот тип файла не поддерживается. Отправьте голосовое сообщение или аудиофайл."}
	}
	return h.handleAudio(ctx, externalUserID, AudioFile{
		FileID: file.FileID, FileName: file.FileName, MIMEType: file.MIMEType, FileSize: file.FileSize,
	})
}

type uploadFile struct {
	fileID   string
	fileName string
	mimeType string
	duration time.Duration
	fileSize int64
}

func (h *Handler) upload(ctx context.Context, externalUserID string, file uploadFile) []string {
	if strings.TrimSpace(file.fileID) == "" {
		return []string{"Telegram не передал идентификатор файла. Отправьте файл ещё раз."}
	}
	if file.fileSize > defaultMaxFileSize {
		return []string{fileTooLargeText()}
	}
	content, err := h.downloader.DownloadFile(ctx, file.fileID)
	if err != nil {
		if errors.Is(err, errFileTooLarge) {
			return []string{fileTooLargeText()}
		}
		return h.errorReply("download", err)
	}

	result, err := h.application.UploadMeeting(ctx, ports.UploadMeetingRequest{
		ExternalUserID: externalUserID,
		File: domain.FileMetadata{
			ExternalFileID: file.fileID,
			FileName:       file.fileName,
			MIMEType:       file.mimeType,
			SizeBytes:      int64(len(content)),
			Duration:       file.duration,
		},
		Content: content,
	})
	if err != nil {
		h.logger.Error("Telegram application request failed", "operation", "upload", "error", err)
		if result.Meeting.ID != "" {
			return []string{fmt.Sprintf("Встреча %s создана, но поставить её в очередь не удалось: %s", result.Meeting.ID, userError(err))}
		}
		return []string{userError(err)}
	}
	return []string{fmt.Sprintf("Файл принят. Встреча: %s. Статус: %s.", result.Meeting.ID, result.Job.Status)}
}

func (h *Handler) errorReply(operation string, err error) []string {
	h.logger.Error("Telegram application request failed", "operation", operation, "error", err)
	return []string{userError(err)}
}

func telegramUserID(id int64) string {
	return fmt.Sprintf("telegram:%d", id)
}

func isAudioMIME(mimeType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "audio/")
}

func helpText() string {
	return strings.Join([]string{
		"Команды:",
		"/start — регистрация",
		"list — список встреч",
		"status <id> — статус обработки",
		"get <id> — транскрипция",
		"find <текст> — поиск",
		"chat <id> <вопрос> — вопрос по встрече",
	}, "\n")
}

func commandUsage(err error) string {
	message := err.Error()
	if _, usage, found := strings.Cut(message, "use "); found {
		return "Неверный формат. Используйте: " + usage
	}
	return "Неверный формат команды."
}

func userError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "Операция отменена или превысила допустимое время. Попробуйте ещё раз."
	case errors.Is(err, domain.ErrNotFound):
		return "Встреча не найдена. Проверьте идентификатор."
	case errors.Is(err, domain.ErrUnsupportedFile):
		return "Этот тип файла не поддерживается. Отправьте голосовое сообщение или аудиофайл."
	case errors.Is(err, domain.ErrNotReady):
		return "Встреча ещё не обработана. Проверьте её командой status <id>."
	case errors.Is(err, domain.ErrQueueFull):
		return "очередь обработки заполнена; попробуйте отправить файл позже"
	case errors.Is(err, domain.ErrShuttingDown):
		return "Сервис завершает работу. Попробуйте позже."
	case errors.Is(err, domain.ErrInvalidArgument):
		return "Проверьте аргументы команды."
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInvalidTransition):
		return "Операция сейчас недоступна из-за состояния встречи."
	default:
		return "Не удалось выполнить операцию. Попробуйте позже."
	}
}

func fileTooLargeText() string {
	return fmt.Sprintf("Файл слишком большой. Максимальный размер — %d МБ.", defaultMaxFileSize/(1<<20))
}

func formatMeetingList(records []domain.MeetingRecord) string {
	if len(records) == 0 {
		return "У вас пока нет встреч."
	}
	var builder strings.Builder
	builder.WriteString("Ваши встречи:")
	for _, record := range records {
		description := strings.TrimSpace(record.Meeting.File.FileName)
		if record.Summary != nil && strings.TrimSpace(record.Summary.Text) != "" {
			description = oneLine(record.Summary.Text)
		}
		if description == "" {
			description = "без описания"
		}
		fmt.Fprintf(&builder, "\n\n%s\n%s · %s\n%s", record.Meeting.ID, formatTime(record.Meeting.CreatedAt), record.Job.Status, description)
	}
	return builder.String()
}

func formatStatus(job domain.ProcessingJob) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Встреча: %s\nСтатус: %s\nСоздана: %s\nОбновлена: %s", job.MeetingID, job.Status, formatTime(job.CreatedAt), formatTime(job.UpdatedAt))
	if job.StartedAt != nil {
		fmt.Fprintf(&builder, "\nОбработка начата: %s", formatTime(*job.StartedAt))
	}
	if job.CompletedAt != nil {
		fmt.Fprintf(&builder, "\nОбработка завершена: %s", formatTime(*job.CompletedAt))
	}
	if strings.TrimSpace(job.Error) != "" {
		fmt.Fprintf(&builder, "\nОшибка: %s", oneLine(job.Error))
	}
	return builder.String()
}

func formatSearchResults(results []domain.SearchResult) string {
	if len(results) == 0 {
		return "Совпадений не найдено."
	}
	var builder strings.Builder
	builder.WriteString("Результаты поиска:")
	for _, result := range results {
		fmt.Fprintf(&builder, "\n\n%s\n%s · %s\n%s", result.Record.Meeting.ID, formatTime(result.Record.Meeting.CreatedAt), result.Record.Job.Status, oneLine(result.Snippet))
	}
	return builder.String()
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.UTC().Format(time.RFC3339)
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func splitTelegramText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{"Нет данных."}
	}
	if utf8.RuneCountInString(text) <= maxTelegramMessageRunes {
		return []string{text}
	}
	runes := []rune(text)
	parts := make([]string, 0, (len(runes)/maxTelegramMessageRunes)+1)
	for len(runes) > 0 {
		end := min(maxTelegramMessageRunes, len(runes))
		if end < len(runes) {
			for split := end; split > maxTelegramMessageRunes/2; split-- {
				if runes[split-1] == '\n' {
					end = split
					break
				}
			}
		}
		parts = append(parts, strings.TrimSpace(string(runes[:end])))
		runes = runes[end:]
	}
	return parts
}
