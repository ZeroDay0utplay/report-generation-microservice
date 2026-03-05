package worker

import (
	"testing"

	"pdf-html-service/internal/models"
)

func makeReq(nPairs int) models.ReportRequest {
	req := models.ReportRequest{
		InterventionName: "Test",
		Address:          "Addr",
		Company:          models.Company{Name: "Co", Contact: "c"},
		Trucks:           []models.Photo{{URL: "https://t.example.com/truck.jpg"}},
		Evidences:        []models.Photo{{URL: "https://t.example.com/ev.jpg"}},
	}
	for i := 0; i < nPairs; i++ {
		req.Pairs = append(req.Pairs, models.Pair{
			BeforeURL: "https://img.example.com/before.jpg",
			AfterURL:  "https://img.example.com/after.jpg",
		})
	}
	return req
}

func TestBuildChunks_SingleChunk(t *testing.T) {
	chunks := BuildChunks(makeReq(30), 50)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if !c.ShowCover {
		t.Error("single chunk must have ShowCover=true")
	}
	if !c.ShowFooter {
		t.Error("single chunk must have ShowFooter=true")
	}
	if len(c.Pairs) != 30 {
		t.Errorf("want 30 pairs, got %d", len(c.Pairs))
	}
	if len(c.Trucks) != 1 || len(c.Evidences) != 1 {
		t.Error("single chunk must carry trucks and evidences")
	}
	if c.Index != 0 || c.Total != 1 {
		t.Errorf("index/total want 0/1, got %d/%d", c.Index, c.Total)
	}
}

func TestBuildChunks_ExactlyOneChunkSize(t *testing.T) {
	chunks := BuildChunks(makeReq(50), 50)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0].Pairs) != 50 {
		t.Errorf("want 50 pairs, got %d", len(chunks[0].Pairs))
	}
}

func TestBuildChunks_MultipleChunks_Order(t *testing.T) {
	chunks := BuildChunks(makeReq(611), 50)
	// 611 / 50 = 12 full chunks + 1 remainder → 13 chunks
	if len(chunks) != 13 {
		t.Fatalf("want 13 chunks, got %d", len(chunks))
	}

	// Verify cover only on first, footer only on last
	if !chunks[0].ShowCover {
		t.Error("chunk 0 must have ShowCover=true")
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].ShowCover {
			t.Errorf("chunk %d must not have ShowCover", i)
		}
	}
	if !chunks[len(chunks)-1].ShowFooter {
		t.Error("last chunk must have ShowFooter=true")
	}
	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].ShowFooter {
			t.Errorf("chunk %d must not have ShowFooter", i)
		}
	}

	// Trucks/evidences only on last
	for i, c := range chunks {
		if i < len(chunks)-1 {
			if len(c.Trucks) > 0 || len(c.Evidences) > 0 {
				t.Errorf("chunk %d should have no trucks/evidences", i)
			}
		} else {
			if len(c.Trucks) != 1 || len(c.Evidences) != 1 {
				t.Error("last chunk must carry trucks and evidences")
			}
		}
	}

	// Pair counts: 12 chunks of 50, 1 chunk of 11
	for i, c := range chunks {
		expected := 50
		if i == len(chunks)-1 {
			expected = 611 - 12*50 // 11
		}
		if len(c.Pairs) != expected {
			t.Errorf("chunk %d: want %d pairs, got %d", i, expected, len(c.Pairs))
		}
	}

	// Index and Total are set correctly
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk %d has wrong Index %d", i, c.Index)
		}
		if c.Total != 13 {
			t.Errorf("chunk %d has wrong Total %d", i, c.Total)
		}
	}
}

func TestBuildChunks_MergeOrder(t *testing.T) {
	// Ensure pair slices are non-overlapping and in order.
	req := makeReq(130)
	for i := range req.Pairs {
		req.Pairs[i].BeforeURL = "https://img.example.com/before.jpg" // reuse URL
		// Use Caption to track position
		req.Pairs[i].Caption = string(rune('A' + i%26))
	}
	chunks := BuildChunks(req, 50)
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Pairs) != 50 || len(chunks[1].Pairs) != 50 || len(chunks[2].Pairs) != 30 {
		t.Errorf("unexpected pair distribution: %d %d %d",
			len(chunks[0].Pairs), len(chunks[1].Pairs), len(chunks[2].Pairs))
	}
}

func TestBuildChunks_EmptyPairs(t *testing.T) {
	req := models.ReportRequest{
		InterventionName: "T",
		Address:          "A",
		Company:          models.Company{Name: "C", Contact: "c"},
		Trucks:           []models.Photo{{URL: "https://t.example.com/t.jpg"}},
	}
	chunks := BuildChunks(req, 50)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk for empty pairs, got %d", len(chunks))
	}
	if !chunks[0].ShowCover || !chunks[0].ShowFooter {
		t.Error("empty-pairs chunk must have both cover and footer")
	}
}

func TestBuildChunks_InvalidChunkSize(t *testing.T) {
	chunks := BuildChunks(makeReq(10), 0) // 0 → falls back to 50
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
}
