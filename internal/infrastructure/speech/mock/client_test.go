package mock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
	"yaConversationWriter/internal/ports"
)

func TestClientReturnsConfiguredTranscriptAndRecordsClonedInput(t *testing.T) {
	client := speechmock.New(speechmock.Config{Transcript: "prepared transcript"})
	input := ports.AudioInput{MeetingID: domain.MeetingID("meeting"), Content: []byte("audio")}

	result, err := client.Transcribe(context.Background(), input)
	if err != nil || result != "prepared transcript" {
		t.Fatalf("Transcribe() result=%q error=%v", result, err)
	}
	input.Content[0] = 'X'
	calls := client.Calls()
	if len(calls) != 1 || string(calls[0].Content) != "audio" {
		t.Fatalf("Calls() = %+v", calls)
	}
	calls[0].Content[0] = 'Y'
	if string(client.Calls()[0].Content) != "audio" {
		t.Fatal("Calls() exposed mutable internal data")
	}
}

func TestClientSupportsErrorsAndCancellation(t *testing.T) {
	configuredErr := errors.New("speech unavailable")
	client := speechmock.New(speechmock.Config{Delay: time.Hour, Err: configuredErr})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := client.Transcribe(ctx, ports.AudioInput{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Transcribe() cancellation error = %v", err)
	}

	client = speechmock.New(speechmock.Config{Err: configuredErr})
	_, err = client.Transcribe(context.Background(), ports.AudioInput{})
	if !errors.Is(err, configuredErr) {
		t.Fatalf("Transcribe() configured error = %v", err)
	}
}
