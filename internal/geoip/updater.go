package geoip

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// downloadHostDefault is the MaxMind endpoint fronted by their CDN; it
// serves signed .tar.gz bundles authenticated by license_key. Overridable
// via UpdaterConfig.BaseURL so tests can point at an httptest server.
const downloadHostDefault = "https://download.maxmind.com"

// UpdaterConfig captures everything the auto-download loop needs. Every
// field except LicenseKey and TargetPath is optional and picks up a
// sensible default.
type UpdaterConfig struct {
	// LicenseKey is the MaxMind account license key. Required.
	LicenseKey string
	// Edition is the MaxMind edition ID, e.g. "GeoLite2-City". Required.
	Edition string
	// TargetPath is where the extracted .mmdb ultimately lives. Required
	// — Updater renames its temp file to this path atomically after each
	// successful download.
	TargetPath string
	// RefreshInterval is how often the background loop re-checks MaxMind
	// for a newer bundle. Zero = default of 7 days (MaxMind publishes
	// updates twice weekly, so weekly is generous but not wasteful).
	RefreshInterval time.Duration
	// HTTPClient is the http.Client used for the download. Zero-value
	// uses a client with a 60s timeout — big enough for the 60 MB City
	// bundle on a home connection, short enough to fail fast on network
	// hangs. Injectable for tests + operators behind egress proxies.
	HTTPClient *http.Client
	// BaseURL overrides downloadHostDefault. Only tests set this.
	BaseURL string
	// Log is called for each significant transition (download start /
	// success / failure, swap, backoff). Passing nil silences the
	// updater — main.go plugs in a slog wrapper. Signature deliberately
	// mimics slog.LogAttrs so the wrapper can pass args through.
	Log func(level, msg string, kv ...any)
	// Opener verifies a freshly-extracted mmdb by opening it. Zero =
	// the package's real Open(). Injectable so tests can bypass the
	// need for a valid mmdb fixture (constructing one by hand is
	// error-prone; downloading a real GeoLite2 needs a MaxMind account
	// which isn't a reasonable test prereq).
	Opener func(path string) (Lookup, error)
}

// Updater downloads a MaxMind mmdb bundle, atomically replaces
// UpdaterConfig.TargetPath, opens the new file as a Lookup, and hands it
// to a caller-supplied callback (typically Handler.SetGeoIP). It also
// loops in the background refreshing on a schedule. Everything is
// fail-open: any download or extract failure logs a warning and the
// existing on-disk file (if any) keeps serving.
type Updater struct {
	cfg UpdaterConfig
	// onSwap fires after each successful download+open with the fresh
	// Lookup. Registered via OnSwap.
	onSwap func(Lookup)
}

// NewUpdater constructs an Updater. Returns an error if LicenseKey,
// Edition, or TargetPath is missing — those are hard preconditions and a
// silent nil-return would mask misconfiguration.
func NewUpdater(cfg UpdaterConfig) (*Updater, error) {
	if cfg.LicenseKey == "" {
		return nil, errors.New("updater: LicenseKey is required")
	}
	if cfg.Edition == "" {
		return nil, errors.New("updater: Edition is required")
	}
	if cfg.TargetPath == "" {
		return nil, errors.New("updater: TargetPath is required")
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 7 * 24 * time.Hour
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = downloadHostDefault
	}
	if cfg.Log == nil {
		cfg.Log = func(_, _ string, _ ...any) {}
	}
	if cfg.Opener == nil {
		cfg.Opener = Open
	}
	return &Updater{cfg: cfg}, nil
}

// OnSwap registers the callback fired after each successful update with
// the newly-opened Lookup. Callers are expected to call Handler.SetGeoIP
// (or equivalent) from within this callback — the swap side is their
// responsibility, since Updater doesn't own the Handler.
func (u *Updater) OnSwap(fn func(Lookup)) { u.onSwap = fn }

// Run drives the update loop. The first iteration runs immediately
// (synchronous to the goroutine that Run is called from), subsequent
// iterations wake up on cfg.RefreshInterval ticks. Blocks until ctx is
// cancelled; ctx cancellation is the only exit path.
//
// Typical usage: `go updater.Run(ctx)` from main.go.
func (u *Updater) Run(ctx context.Context) {
	// Immediate first fetch. On failure the loop keeps going — the next
	// tick may succeed once the network / MaxMind is back.
	u.tickSafe(ctx)
	ticker := time.NewTicker(u.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.tickSafe(ctx)
		}
	}
}

// tickSafe wraps tick with a recover so a panic (malformed tar header,
// nil deref inside extract, unexpected Opener behaviour) doesn't
// silently kill the updater goroutine and freeze auto-refresh forever.
// A single panic degrades this cycle only; the next scheduled tick
// still runs.
func (u *Updater) tickSafe(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			u.cfg.Log("warn", "geoip: tick panicked",
				"panic", fmt.Sprintf("%v", r))
		}
	}()
	u.tick(ctx)
}

// tick performs one download + extract + swap cycle. Every failure is
// logged and swallowed so the caller loop can retry on the next tick.
func (u *Updater) tick(ctx context.Context) {
	u.cfg.Log("info", "geoip: checking for update", "edition", u.cfg.Edition)
	// Ensure the target directory exists BEFORE download tries to
	// create its temp file there. Skipping this made the "fresh host
	// with --geoip-db /var/lib/speedtest/geo.mmdb" case loop forever:
	// download's os.CreateTemp failed with ENOENT and the old MkdirAll
	// call below (post-download) was never reached.
	if err := os.MkdirAll(filepath.Dir(u.cfg.TargetPath), 0o755); err != nil {
		u.cfg.Log("warn", "geoip: target dir create failed", "err", err.Error())
		return
	}
	tmp, err := u.download(ctx)
	if err != nil {
		u.cfg.Log("warn", "geoip: download failed", "err", err.Error())
		return
	}
	// If download returned "already up to date", tmp is empty — skip
	// extract + swap, nothing to do.
	if tmp == "" {
		u.cfg.Log("info", "geoip: already up to date")
		return
	}
	defer os.Remove(tmp) //nolint:errcheck

	extracted, err := u.extract(tmp)
	if err != nil {
		u.cfg.Log("warn", "geoip: extract failed", "err", err.Error())
		return
	}
	defer os.Remove(extracted) //nolint:errcheck

	// Verify the extracted file actually opens as an mmdb before it
	// replaces the live file — a corrupt archive should never nuke a
	// good on-disk copy.
	lu, err := u.cfg.Opener(extracted)
	if err != nil {
		u.cfg.Log("warn", "geoip: verify failed", "err", err.Error())
		return
	}

	// Atomic swap. rename() on Linux is atomic within a filesystem;
	// dstDir on same fs is guaranteed because the temp file was created
	// via os.CreateTemp(filepath.Dir(TargetPath), ...) above.
	if err := os.Rename(extracted, u.cfg.TargetPath); err != nil {
		_ = lu.Close()
		u.cfg.Log("warn", "geoip: rename failed", "err", err.Error())
		return
	}

	u.cfg.Log("info", "geoip: mmdb updated", "path", u.cfg.TargetPath)
	if u.onSwap != nil {
		u.onSwap(lu)
	} else {
		// No swap registered: still close the reader we just opened —
		// otherwise it would leak. Defensive belt-and-suspenders for
		// misuse (Run without OnSwap first).
		_ = lu.Close()
	}
}

// download fetches the .tar.gz bundle to a temp file next to the target
// path (same filesystem, so the subsequent rename is atomic). Uses
// If-Modified-Since keyed on the current TargetPath's mtime so an
// unchanged bundle returns 304 and skips the transfer.
//
// Returns ("", nil) on 304 Not Modified so the caller can skip extract.
func (u *Updater) download(ctx context.Context) (string, error) {
	u.cfg.BaseURL = strings.TrimRight(u.cfg.BaseURL, "/")
	q := url.Values{}
	q.Set("edition_id", u.cfg.Edition)
	q.Set("license_key", u.cfg.LicenseKey)
	q.Set("suffix", "tar.gz")
	url := u.cfg.BaseURL + "/app/geoip_download?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	// If we already have a local file, propose its mtime as
	// If-Modified-Since so MaxMind returns 304 for an unchanged bundle.
	if info, statErr := os.Stat(u.cfg.TargetPath); statErr == nil {
		req.Header.Set("If-Modified-Since", info.ModTime().UTC().Format(http.TimeFormat))
	}

	resp, err := u.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		// Read a short body slice for diagnostics — MaxMind returns
		// text/plain error bodies with helpful messages (bad license
		// key, unknown edition, over quota, etc.).
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	// Stream to a temp file in the same dir as TargetPath so the later
	// rename() stays on the same filesystem (atomic).
	tmp, err := os.CreateTemp(filepath.Dir(u.cfg.TargetPath), "geoip-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	// Cap body copy at 200 MB — GeoLite2-City is ~60 MB, so 200 MB is
	// 3x headroom against future editions but bounds a malicious /
	// bugged endpoint that streams forever.
	const maxBytes = 200 << 20
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("copy body: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}
	if written >= maxBytes {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("bundle exceeds %d bytes", maxBytes)
	}
	return tmp.Name(), nil
}

// extract opens tarGzPath, walks the archive, and copies the first
// .mmdb entry to a sibling temp file. Returns the extracted path so the
// caller can rename it into place.
//
// MaxMind's bundle layout is `GeoLite2-City_YYYYMMDD/GeoLite2-City.mmdb`
// plus a COPYRIGHT / LICENSE text file; we don't care about the license
// files, only the .mmdb.
func (u *Updater) extract(tarGzPath string) (string, error) {
	f, err := os.Open(tarGzPath) //nolint:gosec // path is our own temp
	if err != nil {
		return "", fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("no .mmdb entry in archive")
		}
		if err != nil {
			return "", fmt.Errorf("tar next: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Only care about the .mmdb file. Base-name check side-steps any
		// weird archive layouts and pathological "../" traversal in
		// header names — we write to os.CreateTemp anyway, but rejecting
		// non-.mmdb entries defensively rules out a subtle bug where a
		// LICENSE file with a bogus header ends up being what we open.
		if !strings.HasSuffix(strings.ToLower(path.Base(hdr.Name)), ".mmdb") {
			continue
		}
		out, err := os.CreateTemp(filepath.Dir(u.cfg.TargetPath), "geoip-*.mmdb")
		if err != nil {
			return "", fmt.Errorf("create temp mmdb: %w", err)
		}
		// Cap the extracted size at the same 200 MB bound to guard
		// against a decompression bomb.
		const maxBytes = 200 << 20
		if _, err := io.Copy(out, io.LimitReader(tr, maxBytes)); err != nil {
			_ = out.Close()
			_ = os.Remove(out.Name())
			return "", fmt.Errorf("copy mmdb: %w", err)
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(out.Name())
			return "", fmt.Errorf("close mmdb: %w", err)
		}
		return out.Name(), nil
	}
}
