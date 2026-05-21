package store_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"speedtest-go/internal/store"
)

// open returns a fresh on-disk SQLite Store rooted under t.TempDir.
// The "on-disk" path (rather than :memory:) is intentional: it exercises the
// WAL pragma plumbing, which is the headline durability guarantee of the
// implementation.
func open(t *testing.T) *store.SQLite {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sampleResult returns a fully-populated Result for use in test fixtures.
// CreatedAt is intentionally set to make ordering deterministic across saves.
func sampleResult(createdAt int64) store.Result {
	return store.Result{
		CreatedAt:        createdAt,
		DownloadMbps:     100.5,
		UploadMbps:       20.25,
		LatencyIdleMs:    5.5,
		LatencyLoadedMs:  12.0,
		DownloadJitterMs: 1.1,
		UploadJitterMs:   1.4,
		PacketLoss:       0.0,
		BufferbloatGrade: "A",
		ClientIP:         "127.0.0.1",
		UserAgent:        "test-agent/1.0",
		SettingsJSON:     `{"mode":"time"}`,
	}
}

func TestOpenAndClose(t *testing.T) {
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close again should be safe (no panic, no error wrap).
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	// A path under a non-existent directory should fail at PingContext.
	if _, err := store.Open("/this/path/does/not/exist/x.db"); err == nil {
		t.Error("expected error for unwritable path, got nil")
	}
}

func TestSaveAssignsIDAndTimestamp(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	r := sampleResult(0) // 0 → server should assign
	saved, err := s.Save(ctx, r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID <= 0 {
		t.Errorf("saved.ID = %d, want > 0", saved.ID)
	}
	if saved.CreatedAt <= 0 {
		t.Errorf("saved.CreatedAt = %d, want > 0", saved.CreatedAt)
	}
}

func TestSaveAndListNewestFirst(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	// Save in chronological order; List must return in reverse.
	for i := int64(1); i <= 5; i++ {
		if _, err := s.Save(ctx, sampleResult(i*1000)); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	got, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("List len = %d, want 5", len(got))
	}
	for i := 0; i < len(got)-1; i++ {
		if got[i].CreatedAt < got[i+1].CreatedAt {
			t.Errorf("List not in newest-first order: got[%d]=%d before got[%d]=%d",
				i, got[i].CreatedAt, i+1, got[i+1].CreatedAt)
		}
	}
}

func TestListLimitAndOffset(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		if _, err := s.Save(ctx, sampleResult(i*1000)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	cases := []struct {
		name           string
		limit, offset  int
		wantLen        int
		wantFirstCAt   int64
	}{
		{"first page", 3, 0, 3, 10_000},
		{"second page", 3, 3, 3, 7_000},
		{"limit larger than rows", 100, 0, 10, 10_000},
		{"offset past end", 5, 20, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(ctx, tc.limit, tc.offset)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantLen > 0 && got[0].CreatedAt != tc.wantFirstCAt {
				t.Errorf("first.CreatedAt = %d, want %d", got[0].CreatedAt, tc.wantFirstCAt)
			}
		})
	}
}

func TestListRejectsZeroLimit(t *testing.T) {
	s := open(t)
	if _, err := s.List(context.Background(), 0, 0); err == nil {
		t.Error("expected error for limit=0, got nil")
	}
}

func TestCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	if n, err := s.Count(ctx); err != nil || n != 0 {
		t.Fatalf("Count(empty) = (%d, %v), want (0, nil)", n, err)
	}

	for i := int64(1); i <= 4; i++ {
		if _, err := s.Save(ctx, sampleResult(i)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	n, err := s.Count(ctx)
	if err != nil || n != 4 {
		t.Errorf("Count = (%d, %v), want (4, nil)", n, err)
	}
}

func TestDeleteExistingRow(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	saved, err := s.Save(ctx, sampleResult(1000))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	ok, err := s.Delete(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !ok {
		t.Error("Delete returned ok=false, want true for existing row")
	}

	n, _ := s.Count(ctx)
	if n != 0 {
		t.Errorf("Count after delete = %d, want 0", n)
	}
}

func TestDeleteMissingRow(t *testing.T) {
	s := open(t)
	ok, err := s.Delete(context.Background(), 9999)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok {
		t.Error("Delete missing row returned ok=true")
	}
}

func TestDeleteAll(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	for i := int64(1); i <= 3; i++ {
		if _, err := s.Save(ctx, sampleResult(i)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	n, err := s.DeleteAll(ctx)
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 3 {
		t.Errorf("DeleteAll returned %d, want 3", n)
	}
	count, _ := s.Count(ctx)
	if count != 0 {
		t.Errorf("Count after DeleteAll = %d, want 0", count)
	}
}

func TestPruneOlderThan(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	// Save five rows at ts = 1000, 2000, 3000, 4000, 5000.
	for i := int64(1); i <= 5; i++ {
		if _, err := s.Save(ctx, sampleResult(i*1000)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	t.Run("noop when cutoff is zero or negative", func(t *testing.T) {
		if n, err := s.PruneOlderThan(ctx, 0); err != nil || n != 0 {
			t.Errorf("Prune(0) = (%d, %v), want (0, nil)", n, err)
		}
		if n, err := s.PruneOlderThan(ctx, -1); err != nil || n != 0 {
			t.Errorf("Prune(-1) = (%d, %v), want (0, nil)", n, err)
		}
	})

	t.Run("removes only rows strictly older than cutoff", func(t *testing.T) {
		// cutoff=3000 → rows at 1000 and 2000 removed, 3000/4000/5000 kept.
		n, err := s.PruneOlderThan(ctx, 3000)
		if err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if n != 2 {
			t.Errorf("Prune deleted %d rows, want 2", n)
		}
		count, _ := s.Count(ctx)
		if count != 3 {
			t.Errorf("remaining count = %d, want 3", count)
		}
	})
}

func TestContextCancellation(t *testing.T) {
	s := open(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	if _, err := s.Save(ctx, sampleResult(1000)); err == nil {
		t.Error("Save with cancelled ctx returned nil error, want context.Canceled")
	}
	if _, err := s.List(ctx, 10, 0); err == nil {
		t.Error("List with cancelled ctx returned nil error")
	}
	if _, err := s.Count(ctx); err == nil {
		t.Error("Count with cancelled ctx returned nil error")
	}
}

// TestConcurrentSaveWithWAL exercises the WAL pragma: 10 concurrent writers
// each save a row, and the final count must equal the goroutine count. Without
// WAL + busy_timeout, lock contention frequently surfaces as SQLITE_BUSY.
func TestConcurrentSaveWithWAL(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	const writers = 10
	var wg sync.WaitGroup
	wg.Add(writers)
	errCh := make(chan error, writers)

	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			r := sampleResult(time.Now().UnixMilli() + int64(i))
			if _, err := s.Save(ctx, r); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent Save error: %v", err)
	}

	n, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != writers {
		t.Errorf("Count after concurrent saves = %d, want %d", n, writers)
	}
}

// TestRoundTripPreservesFields verifies every field survives save → list.
func TestRoundTripPreservesFields(t *testing.T) {
	s := open(t)
	ctx := context.Background()

	r := sampleResult(123_456_789)
	r.PacketLoss = 0.5
	saved, err := s.Save(ctx, r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.List(ctx, 1, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("List: got=%v err=%v", got, err)
	}
	g := got[0]
	if g.ID != saved.ID ||
		g.CreatedAt != r.CreatedAt ||
		g.DownloadMbps != r.DownloadMbps ||
		g.UploadMbps != r.UploadMbps ||
		g.LatencyIdleMs != r.LatencyIdleMs ||
		g.LatencyLoadedMs != r.LatencyLoadedMs ||
		g.DownloadJitterMs != r.DownloadJitterMs ||
		g.UploadJitterMs != r.UploadJitterMs ||
		g.PacketLoss != r.PacketLoss ||
		g.BufferbloatGrade != r.BufferbloatGrade ||
		g.ClientIP != r.ClientIP ||
		g.UserAgent != r.UserAgent ||
		g.SettingsJSON != r.SettingsJSON {
		t.Errorf("round-trip mismatch:\n  got=%+v\n want=%+v", g, r)
	}
}
