package services

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// TestRecorderForwardsFlush pins the writer contract the reverse proxy relies on.
func TestRecorderForwardsFlush(t *testing.T) {
	raw := httptest.NewRecorder()
	rec := &recorder{ResponseWriter: raw, body: bytes.Buffer{}}
	resp := echo.NewResponse(rec, slog.New(slog.DiscardHandler))

	resp.Flush()

	if !raw.Flushed {
		t.Fatal("flush did not reach the underlying writer")
	}
	if rec.body.Len() != 0 {
		t.Fatalf("flush wrote %d body bytes, want 0", rec.body.Len())
	}
}
