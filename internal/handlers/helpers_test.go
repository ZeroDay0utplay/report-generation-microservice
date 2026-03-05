package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
)

type mockStorage struct {
	mu            sync.Mutex
	htmlObjects   map[string]string
	pdfObjects    map[string][]byte
	uploadHTMLErr error
	uploadPDFErr  error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		htmlObjects: make(map[string]string),
		pdfObjects:  make(map[string][]byte),
	}
}

func (m *mockStorage) UploadHTML(_ context.Context, key string, html string) error {
	if m.uploadHTMLErr != nil {
		return m.uploadHTMLErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.htmlObjects[key] = html
	return nil
}

func (m *mockStorage) UploadPDF(_ context.Context, key string, reader io.Reader) error {
	if m.uploadPDFErr != nil {
		return m.uploadPDFErr
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pdfObjects[key] = b
	return nil
}

func (m *mockStorage) PublicURL(key string) string {
	return "https://public.example.com/" + key
}

type mockRenderer struct {
	err      error
	pdfBytes []byte
	lastHTML string
}

func (m *mockRenderer) ConvertHTMLToPDF(_ context.Context, html string) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.lastHTML = html
	return io.NopCloser(bytes.NewReader(m.pdfBytes)), nil
}

func samplePayload() models.ReportRequest {
	return models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-2026-0001"),
		InterventionName: "Kitchen renovation",
		Address:          "123 Main St",
		Message:          "<p><strong>Bonjour</strong> chantier.</p>",
		IncludeDates:     true,
		PhotoLayout:      "one_by_row",
		Company: models.Company{
			Name:    "ACME Services",
			Contact: "+216 00 000 000",
			Email:   "hello@acme.tn",
		},
		Pairs: []models.Pair{
			{
				BeforeURL: "https://img.example.com/before.jpg",
				AfterURL:  "https://img.example.com/after.jpg",
				Date:      "2026-02-20",
				Caption:   "Angle 1",
			},
		},
		Trucks: []models.Photo{
			{
				URL:  "https://img.example.com/truck.jpg",
				Date: "2026-02-21",
			},
		},
		Evidences: []models.Photo{
			{
				URL:  "https://img.example.com/evidence.jpg",
				Date: "2026-02-22",
			},
		},
	}
}

type mockStore struct {
	mu      sync.RWMutex
	entries map[string]jobstore.Job
}

func newMockStore() *mockStore {
	return &mockStore{entries: make(map[string]jobstore.Job)}
}

func (m *mockStore) Save(_ context.Context, job jobstore.Job) (jobstore.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.entries[job.ID]; ok {
		return existing, nil
	}
	m.entries[job.ID] = job
	return job, nil
}

func (m *mockStore) GetJob(_ context.Context, jobID string) (jobstore.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if job, ok := m.entries[jobID]; ok {
		return job, nil
	}
	return jobstore.Job{}, jobstore.ErrNotFound
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{}))
}
