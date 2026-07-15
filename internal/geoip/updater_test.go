package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// dummyMMDB is arbitrary bytes standing in for a real MaxMind DB. The
// Updater tests inject a stubOpener so nothing actually parses this —
// we only need the bytes to round-trip through tar.gz → extract →
// rename. Real mmdb parsing is exercised by TestFormatLocation and the
// live smoke test with an operator-supplied file.
var dummyMMDB = []byte("NOT-A-REAL-MMDB-JUST-A-FIXTURE-STUB")

// stubLookup satisfies the Lookup interface for tests that need to
// verify OnSwap fires with a usable-looking handle.
type stubLookup struct{ closed bool }

func (s *stubLookup) Locate(net.IP) string { return "Stubland" }
func (s *stubLookup) Close() error         { s.closed = true; return nil }

// stubOpener bypasses the real Open() so tests don't need a valid mmdb
// fixture. Returns a fresh stubLookup for each call so the test can
// assert on close-ordering (old handle closed after new handle installed).
func stubOpener(_ string) (Lookup, error) { return &stubLookup{}, nil }

// makeTarGz packs mmdbBytes into a tar.gz with the same shape MaxMind's
// bundles use: `GeoLite2-City_YYYYMMDD/GeoLite2-City.mmdb` alongside a
// non-mmdb file so extract() has to skip past it.
func makeTarGz(t *testing.T, mmdbBytes []byte, mmdbName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	license := []byte("MaxMind license text")
	licenseHdr := &tar.Header{
		Name:     "GeoLite2-City_20260714/COPYRIGHT.txt",
		Mode:     0o644,
		Size:     int64(len(license)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(licenseHdr); err != nil {
		t.Fatalf("write license header: %v", err)
	}
	if _, err := tw.Write(license); err != nil {
		t.Fatalf("write license: %v", err)
	}

	mmdbHdr := &tar.Header{
		Name:     "GeoLite2-City_20260714/" + mmdbName,
		Mode:     0o644,
		Size:     int64(len(mmdbBytes)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(mmdbHdr); err != nil {
		t.Fatalf("write mmdb header: %v", err)
	}
	if _, err := tw.Write(mmdbBytes); err != nil {
		t.Fatalf("write mmdb: %v", err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// fakeMaxMind returns an httptest.Server serving the supplied tarball on
// GET, honouring If-Modified-Since when notModifiedSince is non-nil, so
// the 304 branch can be exercised. Returns the server and a hit counter.
func fakeMaxMind(t *testing.T, tarGz []byte, notModifiedSince *time.Time) (*httptest.Server, *int) {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if notModifiedSince != nil {
			if ims := r.Header.Get("If-Modified-Since"); ims != "" {
				parsed, err := http.ParseTime(ims)
				if err == nil && !parsed.Before(*notModifiedSince) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarGz)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestNewUpdaterRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  UpdaterConfig
	}{
		{"no license key", UpdaterConfig{Edition: "X", TargetPath: "/tmp/x"}},
		{"no edition", UpdaterConfig{LicenseKey: "k", TargetPath: "/tmp/x"}},
		{"no target path", UpdaterConfig{LicenseKey: "k", Edition: "X"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewUpdater(tc.cfg); err == nil {
				t.Errorf("NewUpdater(%+v) returned nil error", tc.cfg)
			}
		})
	}
}

func TestUpdaterTickDownloadsExtractsSwaps(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "GeoLite2-City.mmdb")
	tarGz := makeTarGz(t, dummyMMDB, "GeoLite2-City.mmdb")
	srv, hits := fakeMaxMind(t, tarGz, nil)

	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "k",
		Edition:    "GeoLite2-City",
		TargetPath: target,
		BaseURL:    srv.URL,
		Opener:     stubOpener,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	var (
		mu     sync.Mutex
		swaps  int
		latest Lookup
	)
	u.OnSwap(func(l Lookup) {
		mu.Lock()
		defer mu.Unlock()
		swaps++
		latest = l
	})

	u.tick(context.Background())

	if *hits != 1 {
		t.Errorf("hits = %d, want 1", *hits)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target not written: %v", err)
	}
	if !bytes.Equal(body, dummyMMDB) {
		t.Errorf("target contents != dummyMMDB (extract wrote the wrong file)")
	}
	mu.Lock()
	defer mu.Unlock()
	if swaps != 1 {
		t.Errorf("swaps = %d, want 1", swaps)
	}
	if latest == nil {
		t.Fatal("latest Lookup is nil")
	}
}

func TestUpdaterTickReturnsEarlyOn304(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "GeoLite2-City.mmdb")

	// Seed the target so the updater sends If-Modified-Since.
	if err := os.WriteFile(target, dummyMMDB, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Set mtime in the past so the request header carries a definite
	// value; server replies 304 whenever IMS is set.
	past := time.Now().Add(-24 * time.Hour)
	_ = os.Chtimes(target, past, past)
	notModifiedSince := past.Add(-time.Second)
	tarGz := makeTarGz(t, dummyMMDB, "GeoLite2-City.mmdb")
	srv, hits := fakeMaxMind(t, tarGz, &notModifiedSince)

	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "k",
		Edition:    "GeoLite2-City",
		TargetPath: target,
		BaseURL:    srv.URL,
		Opener:     stubOpener,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	swapped := false
	u.OnSwap(func(l Lookup) { swapped = true; _ = l.Close() })

	u.tick(context.Background())

	if *hits != 1 {
		t.Errorf("hits = %d, want 1", *hits)
	}
	if swapped {
		t.Error("OnSwap should not fire on 304")
	}
}

func TestUpdaterDownloadPropagatesHTTPError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geo.mmdb")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Invalid license key")
	}))
	t.Cleanup(srv.Close)

	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "wrong",
		Edition:    "GeoLite2-City",
		TargetPath: target,
		BaseURL:    srv.URL,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	_, err = u.download(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "401") || !contains(err.Error(), "Invalid license key") {
		t.Errorf("error should mention status + body snippet, got %q", err.Error())
	}
}

func TestUpdaterExtractSkipsNonMMDBEntries(t *testing.T) {
	// The archive layout puts the license FIRST, mmdb SECOND — verifies
	// that extract() doesn't accidentally pick the first regular file.
	dir := t.TempDir()
	target := filepath.Join(dir, "geo.mmdb")
	tarGz := makeTarGz(t, dummyMMDB, "GeoLite2-City.mmdb")
	tarPath := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(tarPath, tarGz, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "k", Edition: "GeoLite2-City", TargetPath: target,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	got, err := u.extract(tarPath)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	defer os.Remove(got)
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(data, dummyMMDB) {
		t.Errorf("extracted != mmdb; extract picked up the wrong entry")
	}
}

func TestUpdaterExtractRejectsBundleWithoutMMDB(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geo.mmdb")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	only := []byte("just a license")
	_ = tw.WriteHeader(&tar.Header{
		Name: "LICENSE.txt", Mode: 0o644, Size: int64(len(only)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(only)
	_ = tw.Close()
	_ = gz.Close()

	tarPath := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(tarPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "k", Edition: "GeoLite2-City", TargetPath: target,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	if _, err := u.extract(tarPath); err == nil {
		t.Fatal("expected error for archive without mmdb")
	}
}

// A corrupt bundle (Opener rejects it) must NOT rename the temp file
// into place — the live on-disk copy stays untouched. This is the
// invariant that makes fail-open safe.
func TestUpdaterTickDoesNotOverwriteOnVerifyFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "GeoLite2-City.mmdb")
	original := []byte("good on-disk copy")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tarGz := makeTarGz(t, []byte("garbage that fails verify"), "GeoLite2-City.mmdb")
	srv, _ := fakeMaxMind(t, tarGz, nil)

	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "k",
		Edition:    "GeoLite2-City",
		TargetPath: target,
		BaseURL:    srv.URL,
		Opener:     func(_ string) (Lookup, error) { return nil, io.ErrUnexpectedEOF },
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	swapped := false
	u.OnSwap(func(l Lookup) { swapped = true; _ = l.Close() })

	u.tick(context.Background())

	if swapped {
		t.Error("OnSwap fired despite verify failure")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target vanished: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("target overwritten: got %q, want %q", got, original)
	}
}

// TestUpdaterRunStopsOnCancel verifies the Run loop obeys ctx.Done()
// and does not leak the goroutine. Uses a bad BaseURL so tick() fails
// fast and the loop cycles through the ticker branch quickly.
func TestUpdaterRunStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geo.mmdb")
	u, err := NewUpdater(UpdaterConfig{
		LicenseKey:      "k",
		Edition:         "GeoLite2-City",
		TargetPath:      target,
		BaseURL:         "http://127.0.0.1:1", // TCP RST fast
		RefreshInterval: 20 * time.Millisecond,
		HTTPClient:      &http.Client{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.Run(ctx)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return within 500ms after cancel")
	}
}

// URL builder must carry license_key + edition_id + suffix.
func TestUpdaterDownloadURLShape(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geo.mmdb")
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusServiceUnavailable) // fail so tick doesn't proceed
	}))
	t.Cleanup(srv.Close)
	u, err := NewUpdater(UpdaterConfig{
		LicenseKey: "MYKEY", Edition: "GeoLite2-Country",
		TargetPath: target, BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	_, _ = u.download(context.Background())
	if !contains(gotURL, "edition_id=GeoLite2-Country") {
		t.Errorf("URL missing edition: %q", gotURL)
	}
	if !contains(gotURL, "license_key=MYKEY") {
		t.Errorf("URL missing license_key: %q", gotURL)
	}
	if !contains(gotURL, "suffix=tar.gz") {
		t.Errorf("URL missing suffix: %q", gotURL)
	}
}
