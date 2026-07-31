package telegram

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSplitMessageKeepsShortTextIntact(t *testing.T) {
	text := "<b>Status</b>\nall good"
	chunks := splitMessage(text, maxMessageRunes)
	if len(chunks) != 1 || chunks[0] != text {
		t.Fatalf("expected the text unchanged in one chunk, got %#v", chunks)
	}
}

func TestSplitMessageBreaksOnLineBoundaries(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("<b>node</b> line of report text\n")
	}
	chunks := splitMessage(sb.String(), 100)
	if len(chunks) < 2 {
		t.Fatalf("expected the text to be split, got %d chunk(s)", len(chunks))
	}
	for i, chunk := range chunks {
		if got := len([]rune(chunk)); got > 100 {
			t.Fatalf("chunk %d is %d runes, over the limit", i, got)
		}
		// A line-aligned split never cuts one of the bot's HTML tags.
		if strings.Count(chunk, "<b>") != strings.Count(chunk, "</b>") {
			t.Fatalf("chunk %d has unbalanced tags: %q", i, chunk)
		}
	}
	joined := strings.Join(chunks, "\n")
	if strings.Count(joined, "line of report text") != 40 {
		t.Fatalf("split lost content: %q", joined)
	}
}

func TestSplitMessageHardSplitsAnOversizedLine(t *testing.T) {
	line := strings.Repeat("x", 250)
	chunks := splitMessage(line, 100)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if strings.Join(chunks, "") != line {
		t.Fatal("hard split lost content")
	}
}

func TestSplitMessageHandlesMultibyteRunes(t *testing.T) {
	// Rune-based limits, not byte-based: a CJK report must not be cut
	// mid-character.
	line := strings.Repeat("节点", 100)
	for _, chunk := range splitMessage(line, 50) {
		if strings.Contains(chunk, "�") {
			t.Fatalf("split produced an invalid rune: %q", chunk)
		}
		if len([]rune(chunk)) > 50 {
			t.Fatalf("chunk over limit: %d runes", len([]rune(chunk)))
		}
	}
}

func TestSendMessageToSplitsLongTextIntoSeveralRequests(t *testing.T) {
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		received = append(received, r.FormValue("text"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	bot := NewBot("token", []int64{42})
	bot.SetAPIBase(server.URL + "/bot")

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("a long line of daily report output\n")
	}
	if err := bot.SendMessageTo(42, sb.String()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(received) < 2 {
		t.Fatalf("expected the long message to be split, got %d request(s)", len(received))
	}
	for i, text := range received {
		if got := len([]rune(text)); got > maxMessageRunes {
			t.Fatalf("request %d sent %d runes, over Telegram's cap", i, got)
		}
	}
}

func TestSendMessageErrorIncludesTelegramDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
	}))
	defer server.Close()

	bot := NewBot("token", []int64{42})
	bot.SetAPIBase(server.URL + "/bot")

	err := bot.SendMessageTo(42, "CPU stays <1%")
	if err == nil {
		t.Fatal("expected an error on 400")
	}
	if !strings.Contains(err.Error(), "can't parse entities") {
		t.Fatalf("error dropped Telegram's description: %v", err)
	}
}

func TestGetUpdatesErrorIncludesErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`))
	}))
	defer server.Close()

	bot := NewBot("token", []int64{42})
	bot.SetAPIBase(server.URL + "/bot")

	_, err := bot.getUpdates()
	if err == nil {
		t.Fatal("expected an error when ok=false")
	}
	if !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "Conflict") {
		t.Fatalf("error hid the Telegram failure reason: %v", err)
	}
}

func TestPollCommandsStopsWhileBackingOff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	}))
	defer server.Close()

	bot := NewBot("token", []int64{42})
	bot.SetAPIBase(server.URL + "/bot")

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		bot.PollCommands(func(int64, string) string { return "" }, stop)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PollCommands ignored stop while sleeping between retries")
	}
}
