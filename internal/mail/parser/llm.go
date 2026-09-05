package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrLLMDisabled = errors.New("lan ai parser is disabled")
	ErrSSRFBlocked = errors.New("lan ai endpoint rejected: must be a private lan or loopback ip address")
)

type LANAIClient struct {
	enabled    bool
	endpoint   string
	model      string
	timeout    time.Duration
	httpClient *http.Client
}

func NewLANAIClient(enabled bool, endpoint, model string, timeoutSeconds int) (*LANAIClient, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 3
	}
	timeout := time.Duration(timeoutSeconds) * time.Second

	client := &LANAIClient{
		enabled:  enabled,
		endpoint: endpoint,
		model:    model,
		timeout:  timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}

	if enabled {
		if err := client.validateEndpoint(endpoint); err != nil {
			return nil, err
		}
	}

	return client, nil
}

// validateEndpoint verifies that the AI endpoint is strictly on a private LAN or loopback to prevent SSRF.
func (c *LANAIClient) validateEndpoint(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid endpoint url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid endpoint scheme: %s", u.Scheme)
	}

	hostname := u.Hostname()
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If hostname doesn't resolve yet, check if it's literal loopback or localhost
		if hostname == "localhost" || hostname == "127.0.0.1" {
			return nil
		}
		return fmt.Errorf("resolve endpoint hostname %s: %w", hostname, err)
	}

	for _, ip := range ips {
		if !isPrivateOrLoopback(ip) {
			return fmt.Errorf("%w: %s resolves to non-private ip %s", ErrSSRFBlocked, hostname, ip.String())
		}
	}

	return nil
}

func isPrivateOrLoopback(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	return false
}

type llmResponsePayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type extractedEvent struct {
	Title      string  `json:"title"`
	StartAt    string  `json:"start_at"`
	EndAt      string  `json:"end_at"`
	AllDay     bool    `json:"all_day"`
	Location   string  `json:"location"`
	Confidence float64 `json:"confidence"`
}

// Parse extracts candidate event details via local LAN LLM with strict bounds.
func (c *LANAIClient) Parse(ctx context.Context, text, timezoneName string) (*ParsedEvent, error) {
	if !c.enabled {
		return nil, ErrLLMDisabled
	}

	// Bound input text to prevent resource abuse
	if len(text) > 2048 {
		text = text[:2048]
	}

	prompt := fmt.Sprintf(`Extract calendar event details from the following text in timezone %s. Return ONLY valid JSON with keys: "title", "start_at" (ISO 8601), "end_at" (ISO 8601), "all_day" (bool), "location" (string), "confidence" (0.0 to 1.0). Text:\n%s`,
		timezoneName, text)

	reqPayload := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a concise calendar extraction tool. Output only JSON."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.1,
	}

	reqData, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	reqURL := strings.TrimRight(c.endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lan ai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lan ai returned status %d", resp.StatusCode)
	}

	var llmResp llmResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, fmt.Errorf("decode lan ai response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return nil, errors.New("empty response from lan ai")
	}

	content := strings.TrimSpace(llmResp.Choices[0].Message.Content)
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var extracted extractedEvent
	if err := json.Unmarshal([]byte(content), &extracted); err != nil {
		return nil, fmt.Errorf("decode structured json from ai: %w", err)
	}

	loc, _ := time.LoadLocation(timezoneName)
	if loc == nil {
		loc = time.Local
	}

	start, err := time.ParseInLocation(time.RFC3339, extracted.StartAt, loc)
	if err != nil {
		start, _ = time.ParseInLocation("2006-01-02", extracted.StartAt, loc)
	}
	end, err := time.ParseInLocation(time.RFC3339, extracted.EndAt, loc)
	if err != nil {
		end, _ = time.ParseInLocation("2006-01-02", extracted.EndAt, loc)
	}
	if end.IsZero() && !start.IsZero() {
		if extracted.AllDay {
			end = start.AddDate(0, 0, 1)
		} else {
			end = start.Add(1 * time.Hour)
		}
	}

	return &ParsedEvent{
		Title:      extracted.Title,
		Location:   extracted.Location,
		StartAt:    start,
		EndAt:      end,
		Timezone:   timezoneName,
		AllDay:     extracted.AllDay,
		Confidence: extracted.Confidence,
	}, nil
}
