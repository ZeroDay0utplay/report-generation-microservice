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
	mergePath        = "/forms/pdfengines/merge"
	maxErrorSnippet  = 4 * 1024
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
	req.Header.Set("Gotenberg-Api-Timeout", "90")

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

// ConvertHTMLReaderToPDF streams htmlReader into a Gotenberg request via an
// io.Pipe + goroutine. The multipart body is built concurrently while the HTTP
// request is already in-flight — Chrome starts receiving bytes before the
// template engine finishes rendering, eliminating the intermediate HTML buffer.
func (c *Client) ConvertHTMLReaderToPDF(ctx context.Context, htmlReader io.Reader) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		var closeErr error
		defer func() {
			mw.Close()
			pw.CloseWithError(closeErr)
		}()

		part, err := mw.CreateFormFile("files", "index.html")
		if err != nil {
			closeErr = fmt.Errorf("create multipart file: %w", err)
			return
		}
		if _, err := io.Copy(part, htmlReader); err != nil {
			closeErr = fmt.Errorf("stream html to multipart: %w", err)
			return
		}
		if err := mw.WriteField("preferCssPageSize", "true"); err != nil {
			closeErr = fmt.Errorf("write preferCssPageSize: %w", err)
			return
		}
		if err := mw.WriteField("printBackground", "true"); err != nil {
			closeErr = fmt.Errorf("write printBackground: %w", err)
			return
		}
	}()

	endpoint := c.baseURL + convertPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		_ = pr.CloseWithError(err)
		return nil, fmt.Errorf("build gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		_ = pr.CloseWithError(err)
		return nil, fmt.Errorf("call gotenberg: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
		return nil, &ConvertError{Status: resp.StatusCode, BodySnippet: string(snippet)}
	}

	return resp.Body, nil
}

// MergePDFs sends multiple PDF bytes to Gotenberg's merge endpoint.
// Files are named 0001.pdf, 0002.pdf, … so Gotenberg preserves order.
func (c *Client) MergePDFs(ctx context.Context, chunks [][]byte) (io.ReadCloser, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for i, chunk := range chunks {
		filename := fmt.Sprintf("%04d.pdf", i+1)
		part, err := writer.CreateFormFile("files", filename)
		if err != nil {
			return nil, fmt.Errorf("create merge file %s: %w", filename, err)
		}
		if _, err := part.Write(chunk); err != nil {
			return nil, fmt.Errorf("write merge file %s: %w", filename, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close merge multipart: %w", err)
	}

	endpoint := c.baseURL + mergePath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("build merge request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Gotenberg-Api-Timeout", "120")

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
