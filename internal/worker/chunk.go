package worker

import "pdf-html-service/internal/models"

// Chunk represents one unit of PDF rendering work.
type Chunk struct {
	Index      int
	Total      int
	Pairs      []models.Pair
	Trucks     []models.Photo // only populated on the last chunk
	Evidences  []models.Photo // only populated on the last chunk
	ShowCover  bool           // true for the first chunk
	ShowFooter bool           // true for the last chunk
}

// BuildChunks splits req.Pairs into groups of chunkSize.
// Trucks, Evidences, ShowCover, and ShowFooter are distributed as follows:
//   - Chunk 0 gets ShowCover = true.
//   - The last chunk gets ShowFooter = true, Trucks, and Evidences.
//   - A single-chunk job gets both ShowCover and ShowFooter = true.
//
// Returns at least one chunk even when Pairs is empty (cover + footer only).
func BuildChunks(req models.ReportRequest, chunkSize int) []Chunk {
	if chunkSize <= 0 {
		chunkSize = 50
	}

	pairs := req.Pairs
	if len(pairs) == 0 {
		// Nothing to split — one cover+footer page.
		return []Chunk{{
			Index:      0,
			Total:      1,
			ShowCover:  true,
			ShowFooter: true,
			Trucks:     req.Trucks,
			Evidences:  req.Evidences,
		}}
	}

	// Partition pairs.
	var groups [][]models.Pair
	for i := 0; i < len(pairs); i += chunkSize {
		end := i + chunkSize
		if end > len(pairs) {
			end = len(pairs)
		}
		groups = append(groups, pairs[i:end])
	}

	total := len(groups)
	chunks := make([]Chunk, total)
	for i, g := range groups {
		chunks[i] = Chunk{
			Index:     i,
			Total:     total,
			Pairs:     g,
			ShowCover: i == 0,
			ShowFooter: i == total-1,
		}
		if i == total-1 {
			chunks[i].Trucks = req.Trucks
			chunks[i].Evidences = req.Evidences
		}
	}
	return chunks
}
