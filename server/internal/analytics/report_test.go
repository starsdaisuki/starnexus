package analytics

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"testing"
)

// The daily report is delivered to Telegram with parse_mode=HTML, so any
// model-written "<" or "&" must be escaped before it reaches the message.
func TestAIAnalysisIsHTMLEscaped(t *testing.T) {
	analysis := "CPU stays <1% overnight; disk & swap are healthy. Watch <b>node-a</b>."
	escaped := html.EscapeString(analysis)

	if strings.Contains(escaped, "<1%") {
		t.Fatalf("bare '<' survived escaping: %q", escaped)
	}
	if strings.Contains(escaped, "&s") && !strings.Contains(escaped, "&amp;") {
		t.Fatalf("bare '&' survived escaping: %q", escaped)
	}
	if !strings.Contains(escaped, "&lt;1%") || !strings.Contains(escaped, "&amp;") {
		t.Fatalf("expected entities in %q", escaped)
	}

	// Sanity check the shape the report actually emits.
	block := fmt.Sprintf("<b>AI Analysis</b>\n%s\n", escaped)
	if strings.Count(block, "<b>") != strings.Count(block, "</b>") {
		t.Fatalf("unbalanced tags in emitted block: %q", block)
	}
}

func TestMistralFailureReasonSeparatesPermanentFromTransient(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New(`mistral API returned 401: {"detail":"Unauthorized"}`), "API key rejected"},
		{errors.New("mistral API returned 403: forbidden"), "API key rejected"},
		{errors.New("mistral API returned 429: slow down"), "rate limited"},
		{errors.New("mistral API returned 503: upstream down"), "provider error"},
		{errors.New(`http: Post "https://api.mistral.ai": context deadline exceeded`), "timed out"},
		{errors.New("decode: unexpected EOF"), "request failed"},
		{nil, "unknown"},
	}
	for _, tc := range cases {
		if got := mistralFailureReason(tc.err); got != tc.want {
			t.Fatalf("mistralFailureReason(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
