package postgres

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"yaConversationWriter/internal/domain"
)

func TestEscapeLikeTreatsWildcardsAsText(t *testing.T) {
	if got, want := escapeLike(`100%_\ready`), `100\%\_\\ready`; got != want {
		t.Fatalf("escapeLike() = %q, want %q", got, want)
	}
}

func TestTextSnippetPreservesUTF8(t *testing.T) {
	value := strings.Repeat("я", 161)
	result := textSnippet(value)
	if !strings.HasSuffix(result, "…") || len([]rune(result)) != 161 {
		t.Fatalf("unexpected snippet length or suffix: %q", result)
	}
}

func TestMapErrorClassifiesAndHidesDriverErrors(t *testing.T) {
	repository := &Repository{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	driverError := &pgconn.PgError{Code: "23505", ConstraintName: "users_external_id_key", Detail: "sensitive detail"}
	err := repository.mapError("insert user", driverError)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("mapError() error = %v", err)
	}
	var exposed *pgconn.PgError
	if errors.As(err, &exposed) {
		t.Fatal("repository error exposed a pgx driver type")
	}
	if strings.Contains(err.Error(), driverError.Detail) {
		t.Fatalf("repository error exposed database detail: %v", err)
	}
}
