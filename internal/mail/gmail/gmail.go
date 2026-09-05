package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/mail"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = "https://gmail.googleapis.com/gmail/v1/users/me"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) Name() string {
	return "gmail"
}

type listMessagesResponse struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
}

type getMessageResponse struct {
	ID           string `json:"id"`
	Raw          string `json:"raw"` // Base64URL-encoded raw RFC 822 MIME
	InternalDate string `json:"internalDate"`
}

func (c *Client) FetchMessages(ctx context.Context, accessToken string, filterMode, filterConfig, lastCursor string, limit int) ([]mail.RawEmail, string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/messages", c.baseURL))
	if err != nil {
		return nil, "", err
	}

	q := u.Query()
	if limit <= 0 {
		limit = 20
	}
	q.Set("maxResults", fmt.Sprintf("%d", limit))

	// Restrictive query filter: Default to FamilyBoard label or user-defined query
	queryParts := []string{}
	if filterMode == "label" && filterConfig != "" {
		queryParts = append(queryParts, fmt.Sprintf("label:%s", filterConfig))
	} else if filterConfig != "" {
		queryParts = append(queryParts, filterConfig)
	} else {
		queryParts = append(queryParts, "label:FamilyBoard")
	}

	q.Set("q", strings.Join(queryParts, " "))
	if lastCursor != "" {
		q.Set("pageToken", lastCursor)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("list gmail messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("list gmail messages returned %d: %s", resp.StatusCode, string(body))
	}

	var list listMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, "", fmt.Errorf("decode gmail list: %w", err)
	}

	var rawEmails []mail.RawEmail
	for _, m := range list.Messages {
		raw, err := c.fetchRawMessage(ctx, accessToken, m.ID)
		if err != nil {
			continue
		}
		rawEmails = append(rawEmails, *raw)
	}

	return rawEmails, list.NextPageToken, nil
}

func (c *Client) fetchRawMessage(ctx context.Context, accessToken, messageID string) (*mail.RawEmail, error) {
	endpoint := fmt.Sprintf("%s/messages/%s?format=raw", c.baseURL, url.PathEscape(messageID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch raw message %s returned %d", messageID, resp.StatusCode)
	}

	var msgResp getMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, err
	}

	rawBytes, err := base64.URLEncoding.DecodeString(msgResp.Raw)
	if err != nil {
		return nil, fmt.Errorf("decode raw base64url: %w", err)
	}

	return &mail.RawEmail{
		Provider:          "gmail",
		ProviderMessageID: messageID,
		ReceivedAt:        time.Now(),
		RawMIME:           rawBytes,
	}, nil
}
