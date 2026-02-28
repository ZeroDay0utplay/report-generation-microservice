package handlers

import (
	"net/http"

	"pdf-html-service/internal/models"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, models.HealthResponse{
		RequestID: requestID(r),
		OK:        true,
	})
}
