package gotenberg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

const (
	convertPath     = "/forms/chromium/convert/html"
	maxErrorSnippet = 4 * 1024
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type ConvertError struct {
	Status      int
	BodySnippet string
}

func (e *ConvertError) Error() string {
	return fmt.Sprintf("gotenberg conversion failed with status %d", e.Status)
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := io.WriteString(part, html); err != nil {
		return nil, fmt.Errorf("write html to multipart: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	endpoint := c.baseURL + convertPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call gotenberg: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
		return nil, &ConvertError{Status: resp.StatusCode, BodySnippet: string(snippet)}
	}

	return resp.Body, nil
}
