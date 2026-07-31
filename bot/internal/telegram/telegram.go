package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPIBase = "https://api.telegram.org/bot"

// maxMessageRunes is Telegram's sendMessage text cap (4096). A message
// over the limit is rejected wholesale with 400, so long reports must be
// split rather than silently dropped. The margin leaves room for the
// continuation marker.
const maxMessageRunes = 4000

// errorBodyLimit caps how much of a Telegram error body we read before
// putting it in a log line.
const errorBodyLimit = 512

// Retry pacing for the getUpdates loop.
const (
	pollBackoffMin = 5 * time.Second
	pollBackoffMax = 2 * time.Minute
)

// Bot is a minimal Telegram Bot API client supporting multiple chat IDs.
type Bot struct {
	token   string
	chatIDs []int64
	client  *http.Client
	offset  int64 // getUpdates offset
	apiBase string
}

func NewBot(token string, chatIDs []int64) *Bot {
	return &Bot{
		token:   token,
		chatIDs: chatIDs,
		client:  &http.Client{Timeout: 30 * time.Second},
		apiBase: defaultAPIBase,
	}
}

// SetAPIBase overrides the Telegram API base URL. Used only by the
// end-to-end test to point the bot at a local httptest server. The
// trailing "/bot" is part of the base — callers should pass a URL that
// ends there (e.g. "http://127.0.0.1:8080/bot").
func (b *Bot) SetAPIBase(base string) {
	b.apiBase = base
}

// redactTransportError prevents Telegram bot tokens from leaking through
// net/http errors. Telegram embeds the token in every Bot API request path,
// and url.Error includes the full request URL in Error().
func redactTransportError(err error, token string) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	if token != "" {
		message = strings.ReplaceAll(message, token, "[REDACTED]")
	}
	return errors.New(message)
}

// SendMessage sends a text message to all configured chats.
func (b *Bot) SendMessage(text string) error {
	var lastErr error
	for _, chatID := range b.chatIDs {
		if err := b.SendMessageTo(chatID, text); err != nil {
			log.Printf("sendMessage to %d failed: %v", chatID, err)
			lastErr = err
		}
	}
	return lastErr
}

func (b *Bot) ChatIDs() []int64 {
	ids := make([]int64, len(b.chatIDs))
	copy(ids, b.chatIDs)
	return ids
}

func (b *Bot) SendMessageTo(chatID int64, text string) error {
	for _, chunk := range splitMessage(text, maxMessageRunes) {
		if err := b.sendChunk(chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) sendChunk(chatID int64, text string) error {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {text},
		"parse_mode": {"HTML"},
	}

	resp, err := b.client.PostForm(b.apiBase+b.token+"/sendMessage", params)
	if err != nil {
		return fmt.Errorf("sendMessage: %w", redactTransportError(err, b.token))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Telegram puts the actionable part in the body ("can't parse
		// entities: ...", "message is too long"). Reporting only the
		// status code turns every formatting bug into a mystery.
		return fmt.Errorf("sendMessage: status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

// splitMessage breaks text into chunks of at most limit runes, preferring
// newline boundaries. Messages are HTML-formatted but no tag in this bot
// spans a newline, so line-aligned splits keep every chunk parseable.
func splitMessage(text string, limit int) []string {
	if limit <= 0 || len([]rune(text)) <= limit {
		return []string{text}
	}

	var chunks []string
	var current strings.Builder
	currentLen := 0

	flush := func() {
		if currentLen > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
			currentLen = 0
		}
	}

	for _, line := range strings.SplitAfter(text, "\n") {
		for _, piece := range hardSplit(line, limit) {
			pieceLen := len([]rune(piece))
			if currentLen > 0 && currentLen+pieceLen > limit {
				flush()
			}
			current.WriteString(piece)
			currentLen += pieceLen
		}
	}
	flush()

	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// hardSplit chops a single oversized line into limit-sized rune slices so
// that no piece can ever exceed the cap on its own.
func hardSplit(line string, limit int) []string {
	runes := []rune(line)
	if len(runes) <= limit {
		return []string{line}
	}
	var pieces []string
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		pieces = append(pieces, string(runes[start:end]))
	}
	return pieces
}

func readErrorBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, errorBodyLimit))
	if err != nil || len(raw) == 0 {
		return "<no body>"
	}
	return strings.TrimSpace(string(raw))
}

// Update represents a Telegram update containing a message.
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// CommandHandler is called when a command is received. Returns the reply text.
type CommandHandler func(chatID int64, command string) string

// PollCommands long-polls for updates and dispatches commands.
// Only responds to messages from the configured chat IDs.
// Blocks until stop is closed.
func (b *Bot) PollCommands(handler CommandHandler, stop <-chan struct{}) {
	backoff := pollBackoffMin

	for {
		select {
		case <-stop:
			return
		default:
		}

		updates, err := b.getUpdates()
		if err != nil {
			// A flat 5 s retry hammers Telegram through exactly the
			// outages it should back away from: a 429 stays a 429 and a
			// network blackout produces a log line every 5 s for hours.
			log.Printf("getUpdates error (retry in %s): %v", backoff, err)
			if sleepOrStop(backoff, stop) {
				return
			}
			backoff *= 2
			if backoff > pollBackoffMax {
				backoff = pollBackoffMax
			}
			continue
		}
		backoff = pollBackoffMin

		for _, u := range updates {
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}

			if u.Message == nil || !b.isAllowedChat(u.Message.Chat.ID) {
				continue
			}

			text := strings.TrimSpace(u.Message.Text)
			if !strings.HasPrefix(text, "/") {
				continue
			}

			reply := handler(u.Message.Chat.ID, text)
			if reply != "" {
				// Reply to the specific chat that sent the command
				if err := b.SendMessageTo(u.Message.Chat.ID, reply); err != nil {
					log.Printf("Failed to send reply: %v", err)
				}
			}
		}
	}
}

// sleepOrStop waits for d, returning true if the bot was asked to shut
// down first. A bare time.Sleep in the poll loop delayed SIGTERM handling
// by the full retry interval.
func sleepOrStop(d time.Duration, stop <-chan struct{}) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-stop:
		return true
	case <-timer.C:
		return false
	}
}

func (b *Bot) isAllowedChat(chatID int64) bool {
	for _, id := range b.chatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func (b *Bot) getUpdates() ([]Update, error) {
	params := url.Values{
		"offset":  {strconv.FormatInt(b.offset, 10)},
		"timeout": {"10"},
	}

	resp, err := b.client.PostForm(b.apiBase+b.token+"/getUpdates", params)
	if err != nil {
		return nil, fmt.Errorf("getUpdates: %w", redactTransportError(err, b.token))
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool     `json:"ok"`
		ErrorCode   int      `json:"error_code"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("getUpdates: decode: %w", err)
	}
	if !result.OK {
		// error_code distinguishes the cases that need different responses:
		// 409 means a second bot instance is polling the same token, 429
		// means we are being rate limited. "response not ok" hid both.
		description := result.Description
		if description == "" {
			description = "no description"
		}
		return nil, fmt.Errorf("getUpdates: telegram error %d: %s", result.ErrorCode, description)
	}
	return result.Result, nil
}
