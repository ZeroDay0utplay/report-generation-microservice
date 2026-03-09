package models

// ReportRequest is the shared request schema for both /v1/html and /v1/pdf.
type ReportRequest struct {
	InvoiceNumber    *string `json:"invoiceNumber" validate:"omitempty,max=200"`
	InterventionName string  `json:"interventionName" validate:"required,max=200"`
	Address          string  `json:"address" validate:"required,max=200"`
	Company          Company `json:"company" validate:"required"`
	Message          string  `json:"message,omitempty" validate:"omitempty,max=50000"`
	IncludeDates     bool    `json:"includeDates,omitempty"`
	IncludeDate      *bool   `json:"includeDate,omitempty"`
	PhotoLayout      string  `json:"photoLayout,omitempty" validate:"omitempty,max=50"`
	Pairs            []Pair  `json:"pairs" validate:"required,min=1,dive"`
	Trucks           []Photo `json:"trucks,omitempty" validate:"omitempty,dive"`
	Evidences        []Photo `json:"evidences,omitempty" validate:"omitempty,dive"`
}

type Company struct {
	Name    string `json:"name" validate:"required,max=200"`
	Contact string `json:"contact" validate:"required,max=200"`
	Email   string `json:"email,omitempty" validate:"omitempty,email,max=200"`
	Phone   string `json:"phone,omitempty" validate:"omitempty,max=200"`
	Website string `json:"website,omitempty" validate:"omitempty,url,max=200"`
	LogoURL string `json:"logoUrl,omitempty" validate:"omitempty,url,max=200"`
}

type Pair struct {
	BeforeURL string `json:"beforeUrl" validate:"required,url,max=2000"`
	AfterURL  string `json:"afterUrl" validate:"required,url,max=2000"`
	Date      string `json:"date,omitempty" validate:"omitempty,datetime=2006-01-02"`
	Caption   string `json:"caption,omitempty" validate:"omitempty,max=300"`
}

type Photo struct {
	URL  string `json:"url" validate:"required,url,max=2000"`
	Date string `json:"date,omitempty" validate:"omitempty,datetime=2006-01-02"`
}

type HTMLResponse struct {
	RequestID string `json:"requestId"`
	JobID     string `json:"jobId"`
	HTMLKey   string `json:"htmlKey"`
	HTMLURL   string `json:"htmlUrl"`
}

type PDFDebug struct {
	HTMLKey string `json:"htmlKey"`
	HTMLURL string `json:"htmlUrl"`
}

type PDFResponse struct {
	RequestID string    `json:"requestId"`
	JobID     string    `json:"jobId"`
	PDFKey    string    `json:"pdfKey"`
	PDFURL    string    `json:"pdfUrl"`
	Debug     *PDFDebug `json:"debug,omitempty"`
}

type PDFJobResponse struct {
	RequestID string `json:"requestId"`
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	PDFURL    string `json:"pdfUrl,omitempty"`
}

type RegisterRecipientsRequest struct {
	JobID  string   `json:"jobId" validate:"required,max=200"`
	Emails []string `json:"emails" validate:"required,min=1,max=50,dive,required,email,max=200"`
}

type RegisterRecipientsResponse struct {
	RequestID        string   `json:"requestId"`
	JobID            string   `json:"jobId"`
	Accepted         []string `json:"accepted"`
	TotalRecipients  int      `json:"totalRecipients"`
	EmailStatus      string   `json:"emailStatus"`
	ProcessingStatus string   `json:"status"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type ErrorResponse struct {
	RequestID string   `json:"requestId"`
	Error     APIError `json:"error"`
}

type HealthResponse struct {
	RequestID string `json:"requestId,omitempty"`
	OK        bool   `json:"ok"`
}

// ReportSubmitResponse is returned synchronously by POST /v1/reports.
type ReportSubmitResponse struct {
	RequestID string `json:"requestId"`
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	HTMLURL   string `json:"htmlUrl"`
}
