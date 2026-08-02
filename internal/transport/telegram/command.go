package telegram

import (
	"errors"
	"fmt"
	"strings"
)

type CommandKind string

const (
	CommandStart  CommandKind = "start"
	CommandHelp   CommandKind = "help"
	CommandList   CommandKind = "list"
	CommandStatus CommandKind = "status"
	CommandGet    CommandKind = "get"
	CommandFind   CommandKind = "find"
	CommandChat   CommandKind = "chat"
)

var (
	ErrUnknownCommand = errors.New("unknown Telegram command")
	ErrInvalidCommand = errors.New("invalid Telegram command")
)

type Command struct {
	Kind      CommandKind
	MeetingID string
	Text      string
}

func ParseCommand(input string) (Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Command{}, fmt.Errorf("%w: command is empty", ErrInvalidCommand)
	}

	name, rest, _ := strings.Cut(input, " ")
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if commandName, _, found := strings.Cut(name, "@"); found {
		name = commandName
	}
	name = strings.ToLower(name)
	rest = strings.TrimSpace(rest)

	switch CommandKind(name) {
	case CommandStart:
		// Telegram deep links may add an optional /start payload. Registration
		// does not depend on it, so the adapter intentionally ignores it.
		return Command{Kind: CommandStart}, nil
	case CommandHelp:
		if rest != "" {
			return Command{}, usageError(CommandHelp)
		}
		return Command{Kind: CommandHelp}, nil
	case CommandList:
		if rest != "" {
			return Command{}, usageError(CommandList)
		}
		return Command{Kind: CommandList}, nil
	case CommandStatus, CommandGet:
		parts := strings.Fields(rest)
		if len(parts) != 1 {
			return Command{}, usageError(CommandKind(name))
		}
		return Command{Kind: CommandKind(name), MeetingID: parts[0]}, nil
	case CommandFind:
		if rest == "" {
			return Command{}, usageError(CommandFind)
		}
		return Command{Kind: CommandFind, Text: rest}, nil
	case CommandChat:
		meetingID, question, found := strings.Cut(rest, " ")
		question = strings.TrimSpace(question)
		if !found || strings.TrimSpace(meetingID) == "" || question == "" {
			return Command{}, usageError(CommandChat)
		}
		return Command{Kind: CommandChat, MeetingID: strings.TrimSpace(meetingID), Text: question}, nil
	default:
		return Command{}, fmt.Errorf("%w: %q", ErrUnknownCommand, name)
	}
}

func usageError(kind CommandKind) error {
	var usage string
	switch kind {
	case CommandHelp:
		usage = "help"
	case CommandList:
		usage = "list"
	case CommandStatus:
		usage = "status <meeting-id>"
	case CommandGet:
		usage = "get <meeting-id>"
	case CommandFind:
		usage = "find <keyword>"
	case CommandChat:
		usage = "chat <meeting-id> <question>"
	default:
		usage = string(kind)
	}
	return fmt.Errorf("%w: use %s", ErrInvalidCommand, usage)
}
