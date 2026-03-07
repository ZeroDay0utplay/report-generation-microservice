package gotenberg

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

const (
	convertPath     = "/forms/chromium/convert/html"
	mergePath       = "/forms/pdfengines/merge"
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
	bodyReader, contentType := buildMultipartBody(func(writer *multipart.Writer) error {
		part, err := writer.CreateFormFile("files", "index.html")
		if err != nil {
			return fmt.Errorf("create multipart file: %w", err)
		}
		if _, err := io.WriteString(part, html); err != nil {
			return fmt.Errorf("write html to multipart: %w", err)
		}
		return nil
	})

	endpoint := c.baseURL + convertPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

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

func (c *Client) MergePDFs(ctx context.Context, pdfs []io.Reader) (io.ReadCloser, error) {
	bodyReader, contentType := buildMultipartBody(func(writer *multipart.Writer) error {
		for i, pdf := range pdfs {
			part, err := writer.CreateFormFile("files", fmt.Sprintf("%04d.pdf", i))
			if err != nil {
				return fmt.Errorf("create merge part %d: %w", i, err)
			}
			if _, err := io.Copy(part, pdf); err != nil {
				return fmt.Errorf("copy merge part %d: %w", i, err)
			}
		}
		return nil
	})

	endpoint := c.baseURL + mergePath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build merge request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call gotenberg merge: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
		return nil, &ConvertError{Status: resp.StatusCode, BodySnippet: string(snippet)}
	}

	return resp.Body, nil
}

func buildMultipartBody(writeParts func(writer *multipart.Writer) error) (io.Reader, string) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	contentType := writer.FormDataContentType()

	go func() {
		err := writeParts(writer)
		if closeErr := writer.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		_ = pw.CloseWithError(err)
	}()

	return pr, contentType
}
