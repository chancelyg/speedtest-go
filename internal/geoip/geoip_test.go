package geoip

import (
	"net"
	"testing"
)

// formatLocation is the core string-assembly rule the frontend depends on.
// Table-driven because the fallback chain (city+country → country-only →
// empty) has several branches worth pinning down explicitly.
func TestFormatLocation(t *testing.T) {
	cases := []struct {
		name string
		rec  mmdbRecord
		want string
	}{
		{
			name: "city and country in zh-CN",
			rec: mmdbRecord{
				Country: struct {
					IsoCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				}{IsoCode: "CN", Names: map[string]string{"zh-CN": "中国", "en": "China"}},
				City: struct {
					Names map[string]string `maxminddb:"names"`
				}{Names: map[string]string{"zh-CN": "上海市", "en": "Shanghai"}},
			},
			want: "上海市, 中国",
		},
		{
			name: "en fallback when zh-CN missing",
			rec: mmdbRecord{
				Country: struct {
					IsoCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				}{IsoCode: "US", Names: map[string]string{"en": "United States"}},
				City: struct {
					Names map[string]string `maxminddb:"names"`
				}{Names: map[string]string{"en": "Palo Alto"}},
			},
			want: "Palo Alto, United States",
		},
		{
			name: "country only when city missing (GeoLite2 Country DB)",
			rec: mmdbRecord{
				Country: struct {
					IsoCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				}{IsoCode: "JP", Names: map[string]string{"zh-CN": "日本", "en": "Japan"}},
			},
			want: "日本",
		},
		{
			name: "empty record → empty string",
			rec:  mmdbRecord{},
			want: "",
		},
		{
			name: "country with unknown-language-only names is treated as empty",
			rec: mmdbRecord{
				Country: struct {
					IsoCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				}{IsoCode: "FR", Names: map[string]string{"fr": "France"}},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatLocation(tc.rec); got != tc.want {
				t.Errorf("formatLocation = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPickName(t *testing.T) {
	cases := []struct {
		name  string
		names map[string]string
		want  string
	}{
		{"prefers zh-CN over en", map[string]string{"zh-CN": "中国", "en": "China"}, "中国"},
		{"falls back to en", map[string]string{"en": "China"}, "China"},
		{"nil map", nil, ""},
		{"empty map", map[string]string{}, ""},
		{"empty string in preferred slot skips to next", map[string]string{"zh-CN": "", "en": "X"}, "X"},
		{"unsupported languages ignored", map[string]string{"ja": "中国", "fr": "Chine"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickName(tc.names); got != tc.want {
				t.Errorf("pickName = %q, want %q", got, tc.want)
			}
		})
	}
}

// isPrivateOrLoopback guards Locate against attempting to geolocate LAN
// addresses — mmdb files don't have entries for these anyway, but also
// this is a privacy-affecting decision worth explicit coverage.
func TestIsPrivateOrLoopback(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},        // IPv4 loopback
		{"127.5.6.7", true},        // whole 127/8
		{"::1", true},              // IPv6 loopback
		{"10.0.0.1", true},         // RFC1918
		{"172.16.5.4", true},       // RFC1918
		{"192.168.1.1", true},      // RFC1918
		{"169.254.1.2", true},      // IPv4 link-local
		{"fe80::1", true},          // IPv6 link-local
		{"fd00::1", true},          // IPv6 unique-local (RFC4193 via IsPrivate)
		{"8.8.8.8", false},         // public
		{"1.1.1.1", false},         // public
		{"2001:4860:4860::8888", false}, // public IPv6
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("could not parse test IP %q", tc.ip)
			}
			if got := isPrivateOrLoopback(ip); got != tc.want {
				t.Errorf("isPrivateOrLoopback(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// A nil-receiver reader must not panic on any method — this is the state
// callers see when Open failed but they didn't guard the returned Lookup.
// Handler code already nil-checks, but a defensive method impl is cheap.
func TestReaderNilSafe(t *testing.T) {
	var r *reader
	if got := r.Locate(net.ParseIP("8.8.8.8")); got != "" {
		t.Errorf("nil reader Locate = %q, want empty", got)
	}
	if err := r.Close(); err != nil {
		t.Errorf("nil reader Close = %v, want nil", err)
	}
}

// Open on a missing path returns a wrapped error whose message names the
// path, so operators can diagnose their misconfigured --geoip-db from a
// single log line without cross-referencing.
func TestOpenMissingFile(t *testing.T) {
	_, err := Open("/nonexistent/path/should/never/exist.mmdb")
	if err == nil {
		t.Fatal("Open returned nil error for missing file")
	}
	if got := err.Error(); !contains(got, "/nonexistent/path/should/never/exist.mmdb") {
		t.Errorf("error %q does not mention the input path", got)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
