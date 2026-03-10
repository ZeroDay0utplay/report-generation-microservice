package pdfjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"pdf-html-service/internal/jobstore"
	"pdf-html-service/internal/models"
	"pdf-html-service/internal/render"
	"pdf-html-service/internal/util"
)

var ErrQueueFull = errors.New("pdf job queue is full")

type Storage interface {
	UploadHTML(ctx context.Context, key string, html string) error
	UploadPDF(ctx context.Context, key string, reader io.Reader) error
	PublicURL(key string) string
}

type Renderer interface {
	ConvertHTMLToPDF(ctx context.Context, html string) (io.ReadCloser, error)
}

type MissionInfo struct {
	InterventionName string
	Address          string
}

type Notifier interface {
	SendReportReady(ctx context.Context, recipients []string, jobID, pdfURL string, mission MissionInfo) error
}

type Config struct {
	WorkerCount      int
	QueueSize        int
	EmailWorkerCount int
	EmailQueueSize   int
	SyncWaitTimeout  time.Duration
	RecoveryLimit    int
	OutputPrefix     string
	UploadHTMLOnPDF  bool
	LogoURL          string
}

type SubmitResult struct {
	JobID        string
	Status       string
	PDFURL       string
	ErrorCode    string
	ErrorMessage string
}

type RegisterRecipientsResult struct {
	JobID            string
	Accepted         []string
	TotalRecipients  int
	EmailStatus      string
	ProcessingStatus string
}

type Service struct {
	logger   *slog.Logger
	store    jobstore.PDFStore
	storage  Storage
	renderer Renderer
	notifier Notifier
	cfg      Config
	now      func() time.Time

	workerCtx context.Context
	cancel    context.CancelFunc

	jobsWG sync.WaitGroup

	jobQueue   chan string
	emailQueue chan string

	startOnce sync.Once
	startErr  error
	stopOnce  sync.Once

	jobMu       sync.Mutex
	jobInflight map[string]struct{}

	emailMu       sync.Mutex
	emailInflight map[string]struct{}

	waitersMu sync.Mutex
	waiters   map[string][]chan jobstore.Job
}

func NewService(
	logger *slog.Logger,
	store jobstore.PDFStore,
	storage Storage,
	renderer Renderer,
	notifier Notifier,
	cfg Config,
) *Service {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 128
	}
	if cfg.EmailWorkerCount <= 0 {
		cfg.EmailWorkerCount = 2
	}
	if cfg.EmailQueueSize <= 0 {
		cfg.EmailQueueSize = 128
	}
	if cfg.SyncWaitTimeout <= 0 {
		cfg.SyncWaitTimeout = 10 * time.Second
	}
	if cfg.RecoveryLimit <= 0 {
		cfg.RecoveryLimit = 1000
	}

	return &Service{
		logger:        logger,
		store:         store,
		storage:       storage,
		renderer:      renderer,
		notifier:      notifier,
		cfg:           cfg,
		now:           time.Now,
		jobQueue:      make(chan string, cfg.QueueSize),
		emailQueue:    make(chan string, cfg.EmailQueueSize),
		jobInflight:   make(map[string]struct{}),
		emailInflight: make(map[string]struct{}),
		waiters:       make(map[string][]chan jobstore.Job),
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		s.workerCtx, s.cancel = context.WithCancel(context.Background())

		for i := 0; i < s.cfg.WorkerCount; i++ {
			s.jobsWG.Add(1)
			go s.runPDFWorker(i + 1)
		}
		for i := 0; i < s.cfg.EmailWorkerCount; i++ {
			s.jobsWG.Add(1)
			go s.runEmailWorker(i + 1)
		}

		s.startErr = s.recoverPending(ctx)
	})
	return s.startErr
}

func (s *Service) Stop(ctx context.Context) error {
	var stopErr error

	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.jobsWG.Wait()
		}()

		select {
		case <-done:
		case <-ctx.Done():
			stopErr = ctx.Err()
		}
	})

	return stopErr
}

func (s *Service) Submit(ctx context.Context, rawPayload []byte) (SubmitResult, error) {
	now := s.now().UTC()
	jobID := util.JobIDFromPayload(rawPayload)

	seed := jobstore.Job{
		ID:          jobID,
		Type:        jobstore.JobTypePDF,
		Status:      jobstore.JobStatusQueued,
		EmailStatus: jobstore.EmailStatusNone,
		CreatedAt:   now,
		UpdatedAt:   now,
		Payload:     append([]byte(nil), rawPayload...),
	}

	if _, err := s.store.Save(ctx, seed); err != nil {
		return SubmitResult{}, err
	}

	shouldQueue := false
	job, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		if j.Type == "" {
			j.Type = jobstore.JobTypePDF
		}
		if len(j.Payload) == 0 {
			j.Payload = append([]byte(nil), rawPayload...)
		}
		if j.CreatedAt.IsZero() {
			j.CreatedAt = now
		}
		if j.EmailStatus == "" {
			if len(j.Recipients) > 0 {
				j.EmailStatus = jobstore.EmailStatusRegistered
			} else {
				j.EmailStatus = jobstore.EmailStatusNone
			}
		}

		switch j.Status {
		case jobstore.JobStatusCompleted:
		case jobstore.JobStatusQueued:
			shouldQueue = true
		case jobstore.JobStatusProcessing:
		default:
			j.Status = jobstore.JobStatusQueued
			j.ErrorCode = ""
			j.ErrorMessage = ""
			j.FailedAt = nil
			shouldQueue = true
		}
		j.UpdatedAt = now
		return nil
	})
	if err != nil {
		return SubmitResult{}, err
	}

	if job.Status == jobstore.JobStatusCompleted {
		if len(job.Recipients) > 0 {
			_ = s.enqueueEmail(job.ID)
		}
		return SubmitResult{JobID: job.ID, Status: jobstore.JobStatusCompleted, PDFURL: job.PDFURL}, nil
	}

	if shouldQueue {
		if err := s.enqueueJob(jobID); err != nil {
			if errors.Is(err, ErrQueueFull) {
				_, updateErr := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
					n := s.now().UTC()
					j.Status = jobstore.JobStatusFailed
					j.ErrorCode = "QUEUE_FULL"
					j.ErrorMessage = "pdf job queue is full"
					j.UpdatedAt = n
					j.FailedAt = &n
					return nil
				})
				if updateErr != nil {
					s.logger.Error("failed to persist queue-full state",
						slog.String("jobId", jobID),
						slog.String("error", updateErr.Error()),
					)
				}
			}
			return SubmitResult{}, err
		}
	}

	waitedJob, terminal, err := s.waitForTerminal(ctx, jobID, s.cfg.SyncWaitTimeout)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SubmitResult{JobID: jobID, Status: jobstore.JobStatusProcessing}, nil
		}
		return SubmitResult{}, err
	}

	if terminal {
		switch waitedJob.Status {
		case jobstore.JobStatusCompleted:
			return SubmitResult{JobID: waitedJob.ID, Status: jobstore.JobStatusCompleted, PDFURL: waitedJob.PDFURL}, nil
		case jobstore.JobStatusFailed:
			return SubmitResult{
				JobID:        waitedJob.ID,
				Status:       jobstore.JobStatusFailed,
				ErrorCode:    waitedJob.ErrorCode,
				ErrorMessage: waitedJob.ErrorMessage,
			}, nil
		}
	}

	return SubmitResult{JobID: jobID, Status: jobstore.JobStatusProcessing}, nil
}

func (s *Service) RegisterRecipients(ctx context.Context, jobID string, emails []string) (RegisterRecipientsResult, error) {
	accepted := make([]string, 0, len(emails))
	now := s.now().UTC()

	job, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		existing := make(map[string]struct{}, len(j.Recipients))
		for _, recipient := range j.Recipients {
			existing[strings.ToLower(strings.TrimSpace(recipient))] = struct{}{}
		}

		for _, email := range emails {
			normalized := strings.ToLower(strings.TrimSpace(email))
			if normalized == "" {
				continue
			}
			if _, ok := existing[normalized]; ok {
				continue
			}
			existing[normalized] = struct{}{}
			j.Recipients = append(j.Recipients, normalized)
			accepted = append(accepted, normalized)
		}

		if len(j.Recipients) > 0 {
			if j.RecipientsRegisteredAt == nil {
				t := now
				j.RecipientsRegisteredAt = &t
			}

			switch j.Status {
			case jobstore.JobStatusCompleted:
				if j.EmailStatus != jobstore.EmailStatusSent {
					j.EmailStatus = jobstore.EmailStatusPending
				}
			default:
				if j.EmailStatus == "" || j.EmailStatus == jobstore.EmailStatusNone {
					j.EmailStatus = jobstore.EmailStatusRegistered
				}
			}
		}

		j.UpdatedAt = now
		return nil
	})
	if err != nil {
		return RegisterRecipientsResult{}, err
	}

	if job.Status == jobstore.JobStatusCompleted && len(job.Recipients) > 0 {
		_ = s.enqueueEmail(jobID)
	}

	return RegisterRecipientsResult{
		JobID:            job.ID,
		Accepted:         accepted,
		TotalRecipients:  len(job.Recipients),
		EmailStatus:      job.EmailStatus,
		ProcessingStatus: job.Status,
	}, nil
}

func (s *Service) recoverPending(ctx context.Context) error {
	jobs, err := s.store.List(ctx, s.cfg.RecoveryLimit)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Type != "" && job.Type != jobstore.JobTypePDF {
			continue
		}

		switch job.Status {
		case jobstore.JobStatusQueued:
			if err := s.enqueueJob(job.ID); err != nil && !errors.Is(err, ErrQueueFull) {
				s.logger.Error("failed to recover queued job", slog.String("jobId", job.ID), slog.String("error", err.Error()))
			}
		case jobstore.JobStatusProcessing:
			_, updateErr := s.store.Update(ctx, job.ID, func(j *jobstore.Job) error {
				n := s.now().UTC()
				j.Status = jobstore.JobStatusQueued
				j.UpdatedAt = n
				return nil
			})
			if updateErr != nil {
				s.logger.Error("failed to reset processing job during recovery",
					slog.String("jobId", job.ID),
					slog.String("error", updateErr.Error()),
				)
				continue
			}
			if err := s.enqueueJob(job.ID); err != nil && !errors.Is(err, ErrQueueFull) {
				s.logger.Error("failed to recover reset job", slog.String("jobId", job.ID), slog.String("error", err.Error()))
			}
		}

		if len(job.Recipients) > 0 && job.Status == jobstore.JobStatusCompleted {
			switch job.EmailStatus {
			case "", jobstore.EmailStatusRegistered, jobstore.EmailStatusPending, jobstore.EmailStatusSending, jobstore.EmailStatusFailed:
				if err := s.enqueueEmail(job.ID); err != nil && !errors.Is(err, ErrQueueFull) {
					s.logger.Error("failed to recover email delivery",
						slog.String("jobId", job.ID),
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}
	return nil
}

func (s *Service) runPDFWorker(workerID int) {
	defer s.jobsWG.Done()
	s.logger.Info("pdf worker started", slog.Int("workerId", workerID))
	defer s.logger.Info("pdf worker stopped", slog.Int("workerId", workerID))

	for {
		select {
		case <-s.workerCtx.Done():
			return
		case jobID := <-s.jobQueue:
			s.processPDFJob(s.workerCtx, workerID, jobID)
			s.clearQueuedJob(jobID)
		}
	}
}

func (s *Service) processPDFJob(ctx context.Context, workerID int, jobID string) {
	startedAt := s.now().UTC()
	claim := false

	job, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		if j.Status != jobstore.JobStatusQueued {
			return nil
		}
		claim = true
		j.Status = jobstore.JobStatusProcessing
		j.ErrorCode = ""
		j.ErrorMessage = ""
		j.UpdatedAt = startedAt
		j.ProcessingStartedAt = &startedAt
		return nil
	})
	if err != nil {
		s.logger.Error("failed to claim pdf job",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		return
	}
	if !claim {
		if job.Status == jobstore.JobStatusCompleted && len(job.Recipients) > 0 {
			_ = s.enqueueEmail(jobID)
		}
		return
	}

	if len(job.Payload) == 0 {
		s.failJob(ctx, jobID, "PAYLOAD_NOT_FOUND", "report payload not available")
		return
	}

	var payload models.ReportRequest
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		s.failJob(ctx, jobID, "INVALID_PAYLOAD", "stored report payload is invalid")
		return
	}

	htmlStart := time.Now()
	html, err := render.RenderPDFHTMLWithLogo(payload, s.cfg.LogoURL)
	htmlGenMS := time.Since(htmlStart).Milliseconds()
	if err != nil {
		s.failJob(ctx, jobID, "RENDER_ERROR", "failed to render HTML")
		return
	}

	if s.cfg.UploadHTMLOnPDF {
		htmlKey := htmlObjectKey(s.cfg.OutputPrefix, jobID)
		if err := s.storage.UploadHTML(ctx, htmlKey, html); err != nil {
			s.failJob(ctx, jobID, "UPLOAD_ERROR", "failed to upload debug HTML")
			return
		}
	}

	convertStart := time.Now()
	pdfReader, err := s.renderer.ConvertHTMLToPDF(ctx, html)
	convertMS := time.Since(convertStart).Milliseconds()
	if err != nil {
		s.failJob(ctx, jobID, "GOTENBERG_ERROR", "PDF conversion failed")
		return
	}
	defer pdfReader.Close()

	pdfBytes, err := io.ReadAll(pdfReader)
	if err != nil {
		s.failJob(ctx, jobID, "PDF_ERROR", "failed to read PDF")
		return
	}

	pdfKey := pdfObjectKey(s.cfg.OutputPrefix, jobID)
	uploadStart := time.Now()
	if err := s.storage.UploadPDF(ctx, pdfKey, bytes.NewReader(pdfBytes)); err != nil {
		s.failJob(ctx, jobID, "UPLOAD_ERROR", "failed to upload PDF")
		return
	}
	uploadMS := time.Since(uploadStart).Milliseconds()

	completedAt := s.now().UTC()
	pdfURL := s.storage.PublicURL(pdfKey)
	completedJob, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		j.Status = jobstore.JobStatusCompleted
		j.PDFURL = pdfURL
		j.ErrorCode = ""
		j.ErrorMessage = ""
		j.UpdatedAt = completedAt
		j.CompletedAt = &completedAt
		j.FailedAt = nil

		if len(j.Recipients) > 0 && j.EmailStatus != jobstore.EmailStatusSent {
			j.EmailStatus = jobstore.EmailStatusPending
		}
		if j.EmailStatus == "" {
			j.EmailStatus = jobstore.EmailStatusNone
		}
		return nil
	})
	if err != nil {
		s.logger.Error("failed to persist completed pdf job",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		return
	}

	s.notifyWaiters(completedJob)
	if len(completedJob.Recipients) > 0 {
		_ = s.enqueueEmail(jobID)
	}

	s.logger.Info("pdf job completed",
		slog.String("jobId", jobID),
		slog.Int("workerId", workerID),
		slog.Int64("html_gen_ms", htmlGenMS),
		slog.Int64("convert_ms", convertMS),
		slog.Int64("upload_ms", uploadMS),
		slog.Int64("total_ms", time.Since(startedAt).Milliseconds()),
	)
}

func (s *Service) runEmailWorker(workerID int) {
	defer s.jobsWG.Done()
	s.logger.Info("email worker started", slog.Int("workerId", workerID))
	defer s.logger.Info("email worker stopped", slog.Int("workerId", workerID))

	for {
		select {
		case <-s.workerCtx.Done():
			return
		case jobID := <-s.emailQueue:
			s.processEmailJob(s.workerCtx, workerID, jobID)
			s.clearQueuedEmail(jobID)
		}
	}
}

func (s *Service) processEmailJob(ctx context.Context, workerID int, jobID string) {
	claim := false
	var recipients []string
	var pdfURL string
	var mission MissionInfo

	_, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		if j.Status != jobstore.JobStatusCompleted || j.PDFURL == "" || len(j.Recipients) == 0 {
			return nil
		}
		switch j.EmailStatus {
		case jobstore.EmailStatusSent, jobstore.EmailStatusSending:
			return nil
		}

		claim = true
		recipients = append([]string(nil), j.Recipients...)
		pdfURL = j.PDFURL
		mission = missionInfoFromPayload(j.Payload)
		n := s.now().UTC()
		j.EmailStatus = jobstore.EmailStatusSending
		j.EmailError = ""
		j.UpdatedAt = n
		return nil
	})
	if err != nil {
		s.logger.Error("failed to claim email delivery",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		return
	}
	if !claim {
		return
	}

	if err := s.notifier.SendReportReady(ctx, recipients, jobID, pdfURL, mission); err != nil {
		_, updateErr := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
			n := s.now().UTC()
			j.EmailStatus = jobstore.EmailStatusFailed
			j.EmailError = err.Error()
			j.UpdatedAt = n
			return nil
		})
		if updateErr != nil {
			s.logger.Error("failed to persist email failure",
				slog.String("jobId", jobID),
				slog.String("error", updateErr.Error()),
			)
		}
		s.logger.Error("email delivery failed",
			slog.String("jobId", jobID),
			slog.Int("workerId", workerID),
			slog.String("error", err.Error()),
		)
		return
	}

	_, err = s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		n := s.now().UTC()
		j.EmailStatus = jobstore.EmailStatusSent
		j.EmailError = ""
		j.UpdatedAt = n
		j.EmailSentAt = &n
		return nil
	})
	if err != nil {
		s.logger.Error("failed to persist email sent status",
			slog.String("jobId", jobID),
			slog.String("error", err.Error()),
		)
		return
	}

	s.logger.Info("email delivery completed",
		slog.String("jobId", jobID),
		slog.Int("workerId", workerID),
		slog.Int("recipients", len(recipients)),
	)
}

func missionInfoFromPayload(raw json.RawMessage) MissionInfo {
	if len(raw) == 0 {
		return MissionInfo{}
	}

	var payload models.ReportRequest
	if err := json.Unmarshal(raw, &payload); err != nil {
		return MissionInfo{}
	}

	return MissionInfo{
		InterventionName: strings.TrimSpace(payload.InterventionName),
		Address:          strings.TrimSpace(payload.Address),
	}
}

func (s *Service) failJob(ctx context.Context, jobID, code, message string) {
	now := s.now().UTC()
	failedJob, err := s.store.Update(ctx, jobID, func(j *jobstore.Job) error {
		j.Status = jobstore.JobStatusFailed
		j.ErrorCode = code
		j.ErrorMessage = message
		j.UpdatedAt = now
		j.FailedAt = &now
		return nil
	})
	if err != nil {
		s.logger.Error("failed to persist failed pdf job",
			slog.String("jobId", jobID),
			slog.String("code", code),
			slog.String("error", err.Error()),
		)
		return
	}
	s.notifyWaiters(failedJob)
	s.logger.Error("pdf job failed",
		slog.String("jobId", jobID),
		slog.String("code", code),
		slog.String("message", message),
	)
}

func (s *Service) waitForTerminal(ctx context.Context, jobID string, timeout time.Duration) (jobstore.Job, bool, error) {
	current, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return jobstore.Job{}, false, err
	}
	if isTerminal(current.Status) {
		return current, true, nil
	}

	ch := make(chan jobstore.Job, 1)
	s.addWaiter(jobID, ch)
	defer s.removeWaiter(jobID, ch)

	current, err = s.store.GetJob(ctx, jobID)
	if err != nil {
		return jobstore.Job{}, false, err
	}
	if isTerminal(current.Status) {
		return current, true, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case job := <-ch:
		return job, isTerminal(job.Status), nil
	case <-timer.C:
		current, getErr := s.store.GetJob(ctx, jobID)
		if getErr != nil {
			return jobstore.Job{}, false, getErr
		}
		return current, isTerminal(current.Status), nil
	case <-ctx.Done():
		return jobstore.Job{}, false, ctx.Err()
	}
}

func (s *Service) addWaiter(jobID string, ch chan jobstore.Job) {
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	s.waiters[jobID] = append(s.waiters[jobID], ch)
}

func (s *Service) removeWaiter(jobID string, target chan jobstore.Job) {
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()

	list := s.waiters[jobID]
	if len(list) == 0 {
		return
	}

	filtered := list[:0]
	for _, ch := range list {
		if ch == target {
			continue
		}
		filtered = append(filtered, ch)
	}

	if len(filtered) == 0 {
		delete(s.waiters, jobID)
		return
	}
	s.waiters[jobID] = filtered
}

func (s *Service) notifyWaiters(job jobstore.Job) {
	if !isTerminal(job.Status) {
		return
	}

	s.waitersMu.Lock()
	waiters := s.waiters[job.ID]
	delete(s.waiters, job.ID)
	s.waitersMu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- job:
		default:
		}
	}
}

func (s *Service) enqueueJob(jobID string) error {
	s.jobMu.Lock()
	if _, exists := s.jobInflight[jobID]; exists {
		s.jobMu.Unlock()
		return nil
	}
	s.jobInflight[jobID] = struct{}{}
	s.jobMu.Unlock()

	select {
	case <-s.workerCtx.Done():
		s.clearQueuedJob(jobID)
		return context.Canceled
	case s.jobQueue <- jobID:
		return nil
	default:
		s.clearQueuedJob(jobID)
		return ErrQueueFull
	}
}

func (s *Service) clearQueuedJob(jobID string) {
	s.jobMu.Lock()
	delete(s.jobInflight, jobID)
	s.jobMu.Unlock()
}

func (s *Service) enqueueEmail(jobID string) error {
	s.emailMu.Lock()
	if _, exists := s.emailInflight[jobID]; exists {
		s.emailMu.Unlock()
		return nil
	}
	s.emailInflight[jobID] = struct{}{}
	s.emailMu.Unlock()

	select {
	case <-s.workerCtx.Done():
		s.clearQueuedEmail(jobID)
		return context.Canceled
	case s.emailQueue <- jobID:
		return nil
	default:
		s.clearQueuedEmail(jobID)
		return ErrQueueFull
	}
}

func (s *Service) clearQueuedEmail(jobID string) {
	s.emailMu.Lock()
	delete(s.emailInflight, jobID)
	s.emailMu.Unlock()
}

func isTerminal(status string) bool {
	switch status {
	case jobstore.JobStatusCompleted, jobstore.JobStatusFailed:
		return true
	default:
		return false
	}
}

func htmlObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/index.html", strings.Trim(prefix, "/"), jobID)
}

func pdfObjectKey(prefix, jobID string) string {
	return fmt.Sprintf("%s/%s/report.pdf", strings.Trim(prefix, "/"), jobID)
}
