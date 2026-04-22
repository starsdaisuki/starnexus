package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTelegram stands in for api.telegram.org. It records every
// sendMessage call so the test can assert on payloads, and serves a
// controllable queue of Updates for getUpdates polling.
type fakeTelegram struct {
	mu          sync.Mutex
	sentText    []string
	sentChatID  []int64
	pending     []Update
	nextUpdate  int64
	sendCount   atomic.Int64
	updateCount atomic.Int64
}

func newFakeTelegram() *fakeTelegram {
	return &fakeTelegram{}
}

func (f *fakeTelegram) queueMessage(chatID int64, text string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextUpdate++
	u := Update{UpdateID: f.nextUpdate}
	u.Message = &struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	}{}
	u.Message.Chat.ID = chatID
	u.Message.Text = text
	f.pending = append(f.pending, u)
	return f.nextUpdate
}

func (f *fakeTelegram) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sentText))
	copy(out, f.sentText)
	return out
}

func (f *fakeTelegram) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/bot", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bot token missing", http.StatusNotFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/bot")
		switch {
		case strings.HasSuffix(path, "/sendMessage"):
			f.handleSendMessage(w, r)
		case strings.HasSuffix(path, "/getUpdates"):
			f.handleGetUpdates(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}

func (f *fakeTelegram) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	chatID := r.PostForm.Get("chat_id")
	text := r.PostForm.Get("text")
	f.mu.Lock()
	f.sentText = append(f.sentText, text)
	var id int64
	fmt.Sscanf(chatID, "%d", &id)
	f.sentChatID = append(f.sentChatID, id)
	f.mu.Unlock()
	f.sendCount.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true,"result":{}}`))
}

func (f *fakeTelegram) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	f.updateCount.Add(1)
	f.mu.Lock()
	pending := f.pending
	f.pending = nil
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"result": pending,
	})
}

func TestSendMessageHitsMockTelegram(t *testing.T) {
	fake := newFakeTelegram()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	bot := NewBot("TEST-TOKEN", []int64{10001, 10002})
	bot.SetAPIBase(srv.URL + "/bot")

	if err := bot.SendMessage("hello <b>world</b>"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	sent := fake.sent()
	if len(sent) != 2 {
		t.Fatalf("expected 2 sendMessage calls (one per chat), got %d", len(sent))
	}
	for _, text := range sent {
		if text != "hello <b>world</b>" {
			t.Fatalf("unexpected message text: %q", text)
		}
	}
}

func TestPollCommandsDispatchesAndReplies(t *testing.T) {
	fake := newFakeTelegram()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	const allowedChat = int64(20001)
	bot := NewBot("TEST-TOKEN", []int64{allowedChat})
	bot.SetAPIBase(srv.URL + "/bot")
	// Short client timeout so the poll loop doesn't block the test for
	// the full 10s Telegram default. The real bot uses 30s.
	bot.client.Timeout = 2 * time.Second

	// Queue one command from the allowed chat and one from an outsider.
	fake.queueMessage(allowedChat, "/status")
	fake.queueMessage(99999, "/status")

	handlerCalls := make(chan string, 4)
	handler := func(chatID int64, command string) string {
		handlerCalls <- command
		return "pong"
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		bot.PollCommands(handler, stop)
		close(done)
	}()

	// Wait for the one allowed command to be dispatched.
	select {
	case cmd := <-handlerCalls:
		if cmd != "/status" {
			t.Fatalf("handler got %q, want /status", cmd)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called within 3s")
	}

	// The outsider command should NOT dispatch. Give the poll loop a
	// tick to drain queued updates.
	select {
	case cmd := <-handlerCalls:
		t.Fatalf("handler called for outsider chat with %q", cmd)
	case <-time.After(200 * time.Millisecond):
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("PollCommands did not exit after stop")
	}

	// Verify the reply was sent back to the allowed chat only.
	found := false
	for i, text := range fake.sent() {
		if text == "pong" && fake.sentChatID[i] == allowedChat {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'pong' reply to chat %d, got %v", allowedChat, fake.sent())
	}
}

func TestSendMessageSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
		io.Copy(io.Discard, r.Body)
	}))
	defer srv.Close()

	bot := NewBot("TEST-TOKEN", []int64{30001})
	bot.SetAPIBase(srv.URL + "/bot")
	err := bot.SendMessage("boom")
	if err == nil {
		t.Fatal("expected SendMessage to return an error on 403")
	}
}
