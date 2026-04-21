package telegram

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.telegram.org/bot"

// Bot is a minimal Telegram Bot API client supporting multiple chat IDs.
type Bot struct {
	token   string
	chatIDs []int64
	client  *http.Client
	offset  int64 // getUpdates offset
}

func NewBot(token string, chatIDs []int64) *Bot {
	return &Bot{
		token:   token,
		chatIDs: chatIDs,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
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
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {text},
		"parse_mode": {"HTML"},
	}

	resp, err := b.client.PostForm(apiBase+b.token+"/sendMessage", params)
	if err != nil {
		return fmt.Errorf("sendMessage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sendMessage: status %d", resp.StatusCode)
	}
	return nil
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
	for {
		select {
		case <-stop:
			return
		default:
		}

		updates, err := b.getUpdates()
		if err != nil {
			log.Printf("getUpdates error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

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

	resp, err := b.client.PostForm(apiBase+b.token+"/getUpdates", params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates: response not ok")
	}
	return result.Result, nil
}
