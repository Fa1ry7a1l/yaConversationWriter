package telegram

import (
	"errors"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Command
	}{
		{name: "start", input: "/start", want: Command{Kind: CommandStart}},
		{name: "start payload", input: "/start invite-code", want: Command{Kind: CommandStart}},
		{name: "bot mention", input: "/list@meeting_bot", want: Command{Kind: CommandList}},
		{name: "bare list", input: "list", want: Command{Kind: CommandList}},
		{name: "status", input: " status meeting-1 ", want: Command{Kind: CommandStatus, MeetingID: "meeting-1"}},
		{name: "get", input: "/get meeting-2", want: Command{Kind: CommandGet, MeetingID: "meeting-2"}},
		{name: "find phrase", input: "find   запуск проекта", want: Command{Kind: CommandFind, Text: "запуск проекта"}},
		{name: "chat question", input: "chat meeting-3   Что решили?", want: Command{Kind: CommandChat, MeetingID: "meeting-3", Text: "Что решили?"}},
		{name: "help", input: "/HELP", want: Command{Kind: CommandHelp}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCommand(test.input)
			if err != nil {
				t.Fatalf("ParseCommand() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseCommand() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseCommandRejectsUnknownAndIncompleteInput(t *testing.T) {
	unknown, err := ParseCommand("something")
	if !errors.Is(err, ErrUnknownCommand) || unknown != (Command{}) {
		t.Fatalf("unknown command=%+v error=%v", unknown, err)
	}

	for _, input := range []string{"", "status", "status one two", "get", "find", "chat id", "chat  question", "list extra"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseCommand(input)
			if !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("ParseCommand(%q) error = %v", input, err)
			}
		})
	}
}
