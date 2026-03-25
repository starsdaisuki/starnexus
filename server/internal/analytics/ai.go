package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const mistralAPI = "https://api.mistral.ai/v1/chat/completions"

type mistralRequest struct {
	Model    string           `json:"model"`
	Messages []mistralMessage `json:"messages"`
}

type mistralMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type mistralResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// CallMistral sends a prompt to the Mistral API and returns the response text.
func CallMistral(apiKey, prompt string) (string, error) {
	reqBody := mistralRequest{
		Model: "mistral-small-latest",
		Messages: []mistralMessage{
			{Role: "system", Content: "You are a server operations analyst. Analyze VPS monitoring metrics and provide concise insights, trends, warnings, and recommendations. Be direct and actionable. Reply in 3-5 sentences."},
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", mistralAPI, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("mistral API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result mistralResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return result.Choices[0].Message.Content, nil
}
