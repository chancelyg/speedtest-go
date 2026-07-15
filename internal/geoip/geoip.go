// Package geoip resolves an IP address to a human-readable "City, Country"
// location string using a locally-provided MaxMind- or DB-IP-format .mmdb
// database. The feature is opt-in: main.go only calls Open when the
// operator supplies SPEEDTEST_GEOIP_DB (or --geoip-db); an unset flag
// leaves everything nil and the surrounding code paths short-circuit.
//
// Language handling: the mmdb record's `names` map is keyed by IETF
// language tag ("en", "zh-CN", "ja", ...). We prefer zh-CN and fall back
// to en so both Chinese and English audiences read something natural. The
// preference chain is intentionally hard-coded rather than exposed as a
// config knob — one more surface to document isn't worth the flexibility
// for a self-hosted single-machine tool.
//
// The raw maxminddb-golang reader is used instead of the geoip2-golang
// typed wrapper so the same decoder struct works against both MaxMind
// GeoLite2 City and DB-IP City Lite (their schemas differ in typed edges
// but share the country/city name fields we read here).
package geoip

import (
	"fmt"
	"net"

	"github.com/oschwald/maxminddb-golang"
)

// Lookup is the interface handlers depend on. Kept small and stub-friendly
// so tests can inject a canned resolver without touching a real mmdb file.
type Lookup interface {
	// Locate returns a "City, Country" (or just "Country") string for ip.
	// Returns "" when ip is nil, private/loopback (privacy — do not attempt
	// to geolocate LAN addresses), or absent from the database. Never
	// returns an error: a failed lookup is indistinguishable from a
	// legitimate miss and neither is worth surfacing to the caller.
	Locate(ip net.IP) string

	// Close releases the underlying database handle. Safe to call multiple
	// times; safe to call on a stub implementation as a no-op.
	Close() error
}

// preferredLanguages is the language-tag chain the decoder consults in
// order. The first match wins. Exposed as a var so future tests can
// override it without a re-export.
var preferredLanguages = []string{"zh-CN", "en"}

// mmdbRecord is the minimal shape we decode from each mmdb entry. Both
// MaxMind GeoLite2 City and DB-IP City Lite use the `country.names` /
// `city.names` layout, so a single struct works against both formats.
// GeoLite2 Country omits the City subtree — pickName("") on a missing
// map returns "" and the caller falls back to country-only output.
type mmdbRecord struct {
	Country struct {
		IsoCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
}

// reader is the concrete Lookup backed by an on-disk mmdb file.
type reader struct {
	db *maxminddb.Reader
}

// Open loads path as a MaxMind- or DB-IP-format mmdb database. Returns an
// error if the file is missing, unreadable, or not a valid mmdb.
func Open(path string) (Lookup, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mmdb %s: %w", path, err)
	}
	return &reader{db: db}, nil
}

// Locate implements Lookup. See interface doc for return semantics.
func (r *reader) Locate(ip net.IP) string {
	if r == nil || r.db == nil || ip == nil {
		return ""
	}
	if isPrivateOrLoopback(ip) {
		return ""
	}
	var rec mmdbRecord
	if err := r.db.Lookup(ip, &rec); err != nil {
		// Corrupt entry or lookup failure — treat as a miss rather than
		// bubbling. Enrichment is a nice-to-have, not a correctness path.
		return ""
	}
	return formatLocation(rec)
}

// Close closes the underlying mmdb reader.
func (r *reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// formatLocation assembles "City, Country" using the preferred language
// chain. Falls back to country-only when the city name is empty (which is
// the case for the GeoLite2 Country DB) or when the city lives in a
// different-language locale that our chain doesn't cover.
func formatLocation(rec mmdbRecord) string {
	country := pickName(rec.Country.Names)
	if country == "" {
		// No country name means no meaningful data — even the ISO code
		// isn't user-friendly enough to expose in the History table.
		return ""
	}
	city := pickName(rec.City.Names)
	if city == "" {
		return country
	}
	return city + ", " + country
}

// pickName walks preferredLanguages and returns the first non-empty match
// from names. Returns "" if the map is nil or none of the preferred tags
// resolve.
func pickName(names map[string]string) string {
	if len(names) == 0 {
		return ""
	}
	for _, lang := range preferredLanguages {
		if v, ok := names[lang]; ok && v != "" {
			return v
		}
	}
	return ""
}

// isPrivateOrLoopback reports whether ip is one we should decline to
// resolve. Includes loopback (127.0.0.0/8, ::1), IPv4 link-local
// (169.254/16), RFC1918 private ranges (10/8, 172.16/12, 192.168/16), and
// IPv6 unique-local (fc00::/7). Matching the deliberately narrow set that
// handler.ClientIP already trusts for proxy-header parsing keeps the two
// notions of "internal address" consistent.
func isPrivateOrLoopback(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// net.IP.IsPrivate (Go 1.17+) covers RFC1918 IPv4 and RFC4193 IPv6.
	return ip.IsPrivate()
}
