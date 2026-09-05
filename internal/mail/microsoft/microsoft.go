package microsoft

import (
	"context"
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
		baseURL = "https://graph.microsoft.com/v1.0/me"
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
	return "microsoft"
}

type listMessagesResponse struct {
	Value []struct {
		ID               string `json:"id"`
		Subject          string `json:"subject"`
		ReceivedDateTime string `json:"receivedDateTime"`
		From             struct {
			EmailAddress struct {
				Address string `json:"address"`
				Name    string `json:"name"`
			} `json:"emailAddress"`
		} `json:"from"`
	} `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

func (c *Client) FetchMessages(ctx context.Context, accessToken string, filterMode, filterConfig, lastCursor string, limit int) ([]mail.RawEmail, string, error) {
	endpoint := fmt.Sprintf("%s/mailFolders/inbox/messages", c.baseURL)
	if lastCursor != "" {
		endpoint = lastCursor
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, "", err
	}

	q := u.Query()
	if lastCursor == "" {
		if limit <= 0 {
			limit = 20
		}
		q.Set("$top", fmt.Sprintf("%d", limit))
		q.Set("$select", "id,subject,receivedDateTime,from")
		if filterConfig != "" {
			q.Set("$filter", filterConfig)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("list ms graph messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("list ms graph messages returned %d: %s", resp.StatusCode, string(body))
	}

	var list listMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, "", fmt.Errorf("decode ms graph list: %w", err)
	}

	var rawEmails []mail.RawEmail
	for _, m := range list.Value {
		rawMIME, err := c.fetchRawMIME(ctx, accessToken, m.ID)
		if err != nil {
			continue
		}
		received, err := time.Parse(time.RFC3339, m.ReceivedDateTime)
		if err != nil {
			received = time.Now()
		}

		rawEmails = append(rawEmails, mail.RawEmail{
			Provider:          "microsoft",
			ProviderMessageID: m.ID,
			Sender:            m.From.EmailAddress.Address,
			Subject:           m.Subject,
			ReceivedAt:        received,
			RawMIME:           rawMIME,
		})
	}

	return rawEmails, list.NextLink, nil
}

func (c *Client) fetchRawMIME(ctx context.Context, accessToken, messageID string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/messages/%s/$value", c.baseURL, url.PathEscape(messageID))
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
		return nil, fmt.Errorf("fetch raw mime %s returned status %d", messageID, resp.StatusCode)
	}

	// 10 MB limit reader
	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}
