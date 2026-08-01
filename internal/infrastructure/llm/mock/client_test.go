package mock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	"yaConversationWriter/internal/ports"
)

func TestClientOperationsAreIndependentAndRecorded(t *testing.T) {
	client := llmmock.New(llmmock.Config{Summary: "summary", Answer: "answer"})
	summaryRequest := ports.SummaryRequest{Transcript: "transcript"}
	answerRequest := ports.AnswerRequest{Question: "question"}

	summary, err := client.Summarize(context.Background(), summaryRequest)
	if err != nil || summary != "summary" {
		t.Fatalf("Summarize() result=%q error=%v", summary, err)
	}
	answer, err := client.Answer(context.Background(), answerRequest)
	if err != nil || answer != "answer" {
		t.Fatalf("Answer() result=%q error=%v", answer, err)
	}
	if len(client.SummaryCalls()) != 1 || len(client.AnswerCalls()) != 1 {
		t.Fatalf("calls: summaries=%+v answers=%+v", client.SummaryCalls(), client.AnswerCalls())
	}
}

func TestClientSupportsOperationErrorsAndCancellation(t *testing.T) {
	summaryErr := errors.New("summary failed")
	answerErr := errors.New("answer failed")
	client := llmmock.New(llmmock.Config{SummaryErr: summaryErr, AnswerErr: answerErr})
	if _, err := client.Summarize(context.Background(), ports.SummaryRequest{}); !errors.Is(err, summaryErr) {
		t.Fatalf("Summarize() error = %v", err)
	}
	if _, err := client.Answer(context.Background(), ports.AnswerRequest{}); !errors.Is(err, answerErr) {
		t.Fatalf("Answer() error = %v", err)
	}

	client = llmmock.New(llmmock.Config{AnswerDelay: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.Answer(ctx, ports.AnswerRequest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Answer() cancellation error = %v", err)
	}
}
