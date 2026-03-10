package pdfjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
)

type fakeRenderer struct {
	mu            sync.Mutex
	delay         time.Duration
	block         <-chan struct{}
	err           error
	pdfBytes      []byte
	calls         int
	concurrent    int
	maxConcurrent int
}

func (f *fakeRenderer) ConvertHTMLToPDF(ctx context.Context, _ string) (io.ReadCloser, error) {
	f.mu.Lock()
	f.calls++
	f.concurrent++
	if f.concurrent > f.maxConcurrent {
		f.maxConcurrent = f.concurrent
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.concurrent--
		f.mu.Unlock()
	}()

	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if f.err != nil {
		return nil, f.err
	}

	payload := f.pdfBytes
	if len(payload) == 0 {
		payload = []byte("%PDF-1.7")
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

type fakeStorage struct {
	mu            sync.Mutex
	uploadHTMLErr error
	uploadPDFErr  error
	htmlObjects   map[string]string
	pdfObjects    map[string][]byte
	publicBaseURL string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		htmlObjects:   make(map[string]string),
		pdfObjects:    make(map[string][]byte),
		publicBaseURL: "https://public.example.com",
	}
}

func (f *fakeStorage) UploadHTML(_ context.Context, key string, html string) error {
	if f.uploadHTMLErr != nil {
		return f.uploadHTMLErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.htmlObjects[key] = html
	return nil
}

func (f *fakeStorage) UploadPDF(_ context.Context, key string, reader io.Reader) error {
	if f.uploadPDFErr != nil {
		return f.uploadPDFErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pdfObjects[key] = data
	return nil
}

func (f *fakeStorage) PublicURL(key string) string {
	return f.publicBaseURL + "/" + key
}

type fakeNotifier struct {
	mu         sync.Mutex
	err        error
	block      <-chan struct{}
	calls      int
	jobCalls   map[string]int
	recipients map[string][]string
	missions   map[string]MissionInfo
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{
		jobCalls:   make(map[string]int),
		recipients: make(map[string][]string),
		missions:   make(map[string]MissionInfo),
	}
}

func (f *fakeNotifier) SendReportReady(ctx context.Context, recipients []string, jobID, _ string, mission MissionInfo) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.err != nil {
		return f.err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.jobCalls[jobID]++
	f.recipients[jobID] = append([]string(nil), recipients...)
	f.missions[jobID] = mission
	return nil
}

func (f *fakeNotifier) callCount(jobID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobCalls[jobID]
}

func (f *fakeNotifier) mission(jobID string) MissionInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.missions[jobID]
}

func newServiceForTest(t *testing.T, renderer *fakeRenderer, storage *fakeStorage, notifier *fakeNotifier, cfg Config) (*Service, *jobstore.MemoryStore) {
	t.Helper()
	store := jobstore.NewMemoryStore()
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{}))
	svc := NewService(logger, store, storage, renderer, notifier, cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	return svc, store
}

func sampleRawPayload(t *testing.T, suffix string) []byte {
	t.Helper()
	payload := models.ReportRequest{
		InvoiceNumber:    models.StringPtr("INV-" + suffix),
		InterventionName: "Kitchen renovation",
		Address:          "123 Main St",
		Company: models.Company{
			Name:    "ACME Services",
			Contact: "+216 00 000 000",
			Email:   "hello@acme.tn",
		},
		IncludeDates: true,
		PhotoLayout:  "one_by_row",
		Pairs: []models.Pair{{
			BeforeURL: "https://img.example.com/before.jpg",
			AfterURL:  "https://img.example.com/after.jpg",
			Date:      "2026-03-07",
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return body
}

func waitForJobStatus(t *testing.T, store *jobstore.MemoryStore, jobID, status string, timeout time.Duration) jobstore.Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), jobID)
		if err == nil && job.Status == status {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := store.GetJob(context.Background(), jobID)
	t.Fatalf("job %s did not reach status %s, current=%s", jobID, status, job.Status)
	return jobstore.Job{}
}

func waitForNotifierCalls(t *testing.T, notifier *fakeNotifier, jobID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if notifier.callCount(jobID) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected notifier calls=%d for job %s, got %d", want, jobID, notifier.callCount(jobID))
}

func TestSubmitCompletesWithinWaitWindow(t *testing.T) {
	renderer := &fakeRenderer{delay: 25 * time.Millisecond}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, _ := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      2,
		QueueSize:        16,
		EmailWorkerCount: 1,
		EmailQueueSize:   8,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	res, err := svc.Submit(context.Background(), sampleRawPayload(t, "fast"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Status != jobstore.JobStatusCompleted {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	if res.PDFURL == "" {
		t.Fatal("expected non-empty pdf url")
	}
}

func TestSubmitReturnsProcessingWhenTimeoutExceeded(t *testing.T) {
	block := make(chan struct{})
	renderer := &fakeRenderer{block: block}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  30 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	res, err := svc.Submit(context.Background(), sampleRawPayload(t, "slow"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Status != jobstore.JobStatusProcessing {
		t.Fatalf("expected processing, got %s", res.Status)
	}

	close(block)
	waitForJobStatus(t, store, res.JobID, jobstore.JobStatusCompleted, 2*time.Second)
}

func TestRegisterRecipientsReturnsImmediatelyWhileProcessing(t *testing.T) {
	block := make(chan struct{})
	renderer := &fakeRenderer{block: block}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  25 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "processing-email"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitRes.Status != jobstore.JobStatusProcessing {
		t.Fatalf("expected processing, got %s", submitRes.Status)
	}

	start := time.Now()
	registerRes, err := svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com", "a@example.com", "b@example.com"})
	if err != nil {
		t.Fatalf("register recipients: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("register recipients should return immediately")
	}
	if len(registerRes.Accepted) != 2 {
		t.Fatalf("expected 2 accepted emails, got %v", registerRes.Accepted)
	}

	close(block)
	waitForJobStatus(t, store, submitRes.JobID, jobstore.JobStatusCompleted, 2*time.Second)
	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)
}

func TestCompletedJobRegistrationSendsImmediately(t *testing.T) {
	renderer := &fakeRenderer{delay: 10 * time.Millisecond}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, _ := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "complete-then-email"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitRes.Status != jobstore.JobStatusCompleted {
		t.Fatalf("expected completed, got %s", submitRes.Status)
	}

	_, err = svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com"})
	if err != nil {
		t.Fatalf("register recipients: %v", err)
	}
	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)

	mission := notifier.mission(submitRes.JobID)
	if mission.InterventionName != "Kitchen renovation" {
		t.Fatalf("expected intervention name to be propagated, got %q", mission.InterventionName)
	}
	if mission.Address != "123 Main St" {
		t.Fatalf("expected address to be propagated, got %q", mission.Address)
	}
}

func TestInProgressRegistrationSendsAfterCompletion(t *testing.T) {
	block := make(chan struct{})
	renderer := &fakeRenderer{block: block}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, _ := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  25 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "race-email"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	_, err = svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com"})
	if err != nil {
		t.Fatalf("register recipients: %v", err)
	}
	if notifier.callCount(submitRes.JobID) != 0 {
		t.Fatal("email should not be sent before completion")
	}

	close(block)
	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)
}

func TestDuplicateRecipientsAndNoDuplicateSend(t *testing.T) {
	renderer := &fakeRenderer{}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "dedupe"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForJobStatus(t, store, submitRes.JobID, jobstore.JobStatusCompleted, 2*time.Second)

	res1, err := svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"A@example.com", "a@example.com"})
	if err != nil {
		t.Fatalf("register 1: %v", err)
	}
	if len(res1.Accepted) != 1 {
		t.Fatalf("expected 1 accepted email, got %v", res1.Accepted)
	}
	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)

	res2, err := svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com"})
	if err != nil {
		t.Fatalf("register 2: %v", err)
	}
	if len(res2.Accepted) != 0 {
		t.Fatalf("expected 0 accepted in second registration, got %v", res2.Accepted)
	}
	time.Sleep(100 * time.Millisecond)
	if notifier.callCount(submitRes.JobID) != 1 {
		t.Fatalf("expected single email send, got %d", notifier.callCount(submitRes.JobID))
	}
}

func TestConcurrentGenerationAcrossWorkers(t *testing.T) {
	renderer := &fakeRenderer{delay: 120 * time.Millisecond}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      4,
		QueueSize:        8,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  15 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	results := make([]SubmitResult, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Submit(context.Background(), sampleRawPayload(t, "concurrent-"+string(rune('a'+i))))
			if err != nil {
				t.Errorf("submit %d: %v", i, err)
				return
			}
			results[i] = res
		}()
	}
	wg.Wait()

	for _, res := range results {
		waitForJobStatus(t, store, res.JobID, jobstore.JobStatusCompleted, 3*time.Second)
	}

	renderer.mu.Lock()
	maxConcurrent := renderer.maxConcurrent
	renderer.mu.Unlock()
	if maxConcurrent < 2 {
		t.Fatalf("expected concurrent rendering across workers, max=%d", maxConcurrent)
	}
}

func TestNoDuplicateEmailSendOnConcurrentRegistration(t *testing.T) {
	renderer := &fakeRenderer{}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 2,
		EmailQueueSize:   8,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "dup-send"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForJobStatus(t, store, submitRes.JobID, jobstore.JobStatusCompleted, 2*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com", "b@example.com"})
		}()
	}
	wg.Wait()

	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)
}

func TestRaceBetweenCompletionAndRecipientSubmission(t *testing.T) {
	block := make(chan struct{})
	renderer := &fakeRenderer{block: block}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  20 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	submitRes, err := svc.Submit(context.Background(), sampleRawPayload(t, "race"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = svc.RegisterRecipients(context.Background(), submitRes.JobID, []string{"a@example.com"})
	}()
	go func() {
		defer wg.Done()
		close(block)
	}()
	wg.Wait()

	waitForJobStatus(t, store, submitRes.JobID, jobstore.JobStatusCompleted, 2*time.Second)
	waitForNotifierCalls(t, notifier, submitRes.JobID, 1, 2*time.Second)
}

func TestGenerationFailure(t *testing.T) {
	renderer := &fakeRenderer{err: errors.New("convert fail")}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	res, err := svc.Submit(context.Background(), sampleRawPayload(t, "fail-render"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Status != jobstore.JobStatusFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	job := waitForJobStatus(t, store, res.JobID, jobstore.JobStatusFailed, 2*time.Second)
	if job.ErrorCode != "GOTENBERG_ERROR" {
		t.Fatalf("expected GOTENBERG_ERROR, got %s", job.ErrorCode)
	}
}

func TestUploadFailure(t *testing.T) {
	renderer := &fakeRenderer{}
	storage := newFakeStorage()
	storage.uploadPDFErr = errors.New("upload failed")
	notifier := newFakeNotifier()
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	res, err := svc.Submit(context.Background(), sampleRawPayload(t, "fail-upload"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Status != jobstore.JobStatusFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	job := waitForJobStatus(t, store, res.JobID, jobstore.JobStatusFailed, 2*time.Second)
	if job.ErrorCode != "UPLOAD_ERROR" {
		t.Fatalf("expected UPLOAD_ERROR, got %s", job.ErrorCode)
	}
}

func TestEmailFailureState(t *testing.T) {
	renderer := &fakeRenderer{}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	notifier.err = errors.New("smtp down")
	svc, store := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        4,
		EmailWorkerCount: 1,
		EmailQueueSize:   4,
		SyncWaitTimeout:  500 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	res, err := svc.Submit(context.Background(), sampleRawPayload(t, "email-fail"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForJobStatus(t, store, res.JobID, jobstore.JobStatusCompleted, 2*time.Second)

	_, err = svc.RegisterRecipients(context.Background(), res.JobID, []string{"a@example.com"})
	if err != nil {
		t.Fatalf("register recipients: %v", err)
	}

	job := waitForJobStatus(t, store, res.JobID, jobstore.JobStatusCompleted, 2*time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, _ = store.GetJob(context.Background(), res.JobID)
		if job.EmailStatus == jobstore.EmailStatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.EmailStatus != jobstore.EmailStatusFailed {
		t.Fatalf("expected email status failed, got %s", job.EmailStatus)
	}
}

func TestQueueSaturation(t *testing.T) {
	block := make(chan struct{})
	renderer := &fakeRenderer{block: block}
	storage := newFakeStorage()
	notifier := newFakeNotifier()
	svc, _ := newServiceForTest(t, renderer, storage, notifier, Config{
		WorkerCount:      1,
		QueueSize:        1,
		EmailWorkerCount: 1,
		EmailQueueSize:   2,
		SyncWaitTimeout:  5 * time.Millisecond,
		OutputPrefix:     "docs",
	})

	_, err1 := svc.Submit(context.Background(), sampleRawPayload(t, "q1"))
	if err1 != nil {
		t.Fatalf("submit q1: %v", err1)
	}
	_, err2 := svc.Submit(context.Background(), sampleRawPayload(t, "q2"))
	if err2 != nil {
		t.Fatalf("submit q2: %v", err2)
	}
	_, err3 := svc.Submit(context.Background(), sampleRawPayload(t, "q3"))
	if !errors.Is(err3, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err3)
	}

	close(block)
}
