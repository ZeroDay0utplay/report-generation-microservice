package pipeline

import (
	"encoding/json"
	"fmt"

	"pdf-html-service/internal/models"
)

func parsePayload(data []byte) (models.ReportRequest, error) {
	var req models.ReportRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return models.ReportRequest{}, fmt.Errorf("unmarshal report request: %w", err)
	}
	return req, nil
}
