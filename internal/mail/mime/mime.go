package mime

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var (
	ErrMessageTooLarge = errors.New("email message exceeds maximum allowable size limit")
	ErrNestingTooDeep  = errors.New("nested multipart depth exceeds safe execution bounds")
)

type ParsedMessage struct {
	Subject     string
	Sender      string
	ReceivedAt  time.Time
	TextBody    string
	ICSContents [][]byte
}

type Parser struct {
	maxTotalBytes int64
	maxBodyBytes  int64
	maxDepth      int
}

func NewParser(maxTotalBytes, maxBodyBytes int64) *Parser {
	if maxTotalBytes <= 0 {
		maxTotalBytes = 10 * 1024 * 1024 // 10 MB default
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = 5 * 1024 * 1024 // 5 MB default
	}
	return &Parser{
		maxTotalBytes: maxTotalBytes,
		maxBodyBytes:  maxBodyBytes,
		maxDepth:      10,
	}
}

// Parse parses raw RFC 822 / MIME bytes safely with strict resource bounds.
func (p *Parser) Parse(rawMIME []byte) (*ParsedMessage, error) {
	if int64(len(rawMIME)) > p.maxTotalBytes {
		return nil, ErrMessageTooLarge
	}

	reader := bytes.NewReader(rawMIME)
	msg, err := mail.ReadMessage(reader)
	if err != nil {
		return nil, fmt.Errorf("read mime message: %w", err)
	}

	wordDecoder := &mime.WordDecoder{}
	subject, _ := wordDecoder.DecodeHeader(msg.Header.Get("Subject"))
	from, _ := wordDecoder.DecodeHeader(msg.Header.Get("From"))
	dateStr := msg.Header.Get("Date")
	receivedAt, err := mail.ParseDate(dateStr)
	if err != nil {
		receivedAt = time.Now()
	}

	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	parsed := &ParsedMessage{
		Subject:    subject,
		Sender:     from,
		ReceivedAt: receivedAt,
	}

	var textParts []string
	var htmlParts []string

	err = p.parsePart(msg.Body, contentType, 0, &textParts, &htmlParts, &parsed.ICSContents)
	if err != nil {
		return nil, err
	}

	if len(textParts) > 0 {
		parsed.TextBody = strings.Join(textParts, "\n\n")
	} else if len(htmlParts) > 0 {
		combinedHTML := strings.Join(htmlParts, "\n\n")
		parsed.TextBody = StripHTML(combinedHTML)
	}

	// Bound final body size
	if int64(len(parsed.TextBody)) > p.maxBodyBytes {
		parsed.TextBody = parsed.TextBody[:p.maxBodyBytes]
	}

	return parsed, nil
}

func (p *Parser) parsePart(body io.Reader, contentTypeHeader string, depth int, textParts, htmlParts *[]string, icsParts *[][]byte) error {
	if depth > p.maxDepth {
		return ErrNestingTooDeep
	}

	mediaType, params, err := mime.ParseMediaType(contentTypeHeader)
	if err != nil {
		mediaType = "text/plain"
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil
		}
		mr := multipart.NewReader(body, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil // Stop cleanly on malformed boundaries
			}

			partCT := part.Header.Get("Content-Type")
			if partCT == "" {
				partCT = "text/plain"
			}

			// Check for ICS attachment filename
			disp := part.Header.Get("Content-Disposition")
			_, dispParams, _ := mime.ParseMediaType(disp)
			filename := strings.ToLower(dispParams["filename"])

			if strings.HasSuffix(filename, ".ics") || strings.EqualFold(mediaType, "text/calendar") {
				icsData, _ := io.ReadAll(io.LimitReader(part, p.maxBodyBytes))
				*icsParts = append(*icsParts, icsData)
				continue
			}

			_ = p.parsePart(part, partCT, depth+1, textParts, htmlParts, icsParts)
		}
		return nil
	}

	if strings.EqualFold(mediaType, "text/calendar") {
		data, err := io.ReadAll(io.LimitReader(body, p.maxBodyBytes))
		if err == nil && len(data) > 0 {
			*icsParts = append(*icsParts, data)
		}
		return nil
	}

	if strings.EqualFold(mediaType, "text/plain") {
		data, err := io.ReadAll(io.LimitReader(body, p.maxBodyBytes))
		if err == nil && len(data) > 0 {
			*textParts = append(*textParts, string(data))
		}
		return nil
	}

	if strings.EqualFold(mediaType, "text/html") {
		data, err := io.ReadAll(io.LimitReader(body, p.maxBodyBytes))
		if err == nil && len(data) > 0 {
			*htmlParts = append(*htmlParts, string(data))
		}
		return nil
	}

	return nil
}

var (
	scriptRegex = regexp.MustCompile(`(?is)<(script|style|noscript|iframe|object|embed)[^>]*>.*?</(?:script|style|noscript|iframe|object|embed)>`)
	tagRegex    = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRegex  = regexp.MustCompile(`[ \t\r]+`)
	lineRegex   = regexp.MustCompile(`\n{3,}`)
)

// StripHTML converts HTML strings into sanitized, safe plain text.
func StripHTML(input string) string {
	// 1. Remove dangerous script/style tags and contents
	clean := scriptRegex.ReplaceAllString(input, " ")

	// 2. Convert block elements to newlines
	clean = strings.ReplaceAll(clean, "<br>", "\n")
	clean = strings.ReplaceAll(clean, "<br/>", "\n")
	clean = strings.ReplaceAll(clean, "<br />", "\n")
	clean = strings.ReplaceAll(clean, "</p>", "\n\n")
	clean = strings.ReplaceAll(clean, "</div>", "\n")
	clean = strings.ReplaceAll(clean, "</tr>", "\n")
	clean = strings.ReplaceAll(clean, "</li>", "\n")

	// 3. Strip remaining tags
	clean = tagRegex.ReplaceAllString(clean, "")

	// 4. Unescape HTML entities
	clean = html.UnescapeString(clean)

	// 5. Normalize whitespace
	clean = spaceRegex.ReplaceAllString(clean, " ")
	clean = lineRegex.ReplaceAllString(clean, "\n\n")

	return strings.TrimSpace(clean)
}
