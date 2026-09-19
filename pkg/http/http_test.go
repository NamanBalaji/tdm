package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	httpmod "github.com/NamanBalaji/tdm/pkg/http"
)

func TestGetFilename(t *testing.T) {
	tests := []struct {
		name string
		resp *http.Response
		want string
	}{
		{
			name: "Content-Disposition filename",
			resp: &http.Response{
				Header: http.Header{
					"Content-Disposition": []string{`attachment; filename="example.txt"`},
				},
				Request: &http.Request{URL: mustParseURL("http://example.com/ignored")},
			},
			want: "example.txt",
		},
		{
			name: "URL path fallback",
			resp: &http.Response{
				Header:  http.Header{},
				Request: &http.Request{URL: mustParseURL("http://example.com/path/to/file.bin")},
			},
			want: "file.bin",
		},
		{
			name: "URL query filename param",
			resp: &http.Response{
				Header:  http.Header{},
				Request: &http.Request{URL: mustParseURL("http://example.com/download?filename=data.zip")},
			},
			want: "data.zip",
		},
		{
			name: "Default when no path or param",
			resp: &http.Response{
				Header:  http.Header{},
				Request: &http.Request{URL: mustParseURL("http://example.com/")},
			},
			want: "download",
		},
		{
			name: "Content-Disposition path traversal is sanitized",
			resp: &http.Response{
				Header: http.Header{
					"Content-Disposition": []string{`attachment; filename="../../home/user/.ssh/authorized_keys"`},
				},
				Request: &http.Request{URL: mustParseURL("http://example.com/x")},
			},
			want: "authorized_keys",
		},
		{
			name: "query filename traversal is sanitized",
			resp: &http.Response{
				Header:  http.Header{},
				Request: &http.Request{URL: mustParseURL("http://example.com/dl?filename=..%2F..%2Fetc%2Fcrontab")},
			},
			want: "crontab",
		},
		{
			name: "hostile Content-Disposition falls through to URL path",
			resp: &http.Response{
				Header: http.Header{
					"Content-Disposition": []string{`attachment; filename=".."`},
				},
				Request: &http.Request{URL: mustParseURL("http://example.com/real-name.zip")},
			},
			want: "real-name.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.GetFilename(tt.resp)
			if got != tt.want {
				t.Errorf("GetFilename() = %q; want %q", got, tt.want)
			}
		})
	}
}

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func TestParseLastModified(t *testing.T) {
	valid := "Mon, 02 Jan 2006 15:04:05 MST"
	parsed := httpmod.ParseLastModified(valid)
	if parsed.IsZero() {
		t.Errorf("ParseLastModified(%q) returned zero time; want non-zero", valid)
	}

	invalid := "Not a date"
	parsed2 := httpmod.ParseLastModified(invalid)
	if !parsed2.IsZero() {
		t.Errorf("ParseLastModified(%q) = %v; want zero time", invalid, parsed2)
	}
}

func TestIsDownloadable(t *testing.T) {
	// IsDownloadable is purely syntactic — no network I/O.
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"http URL", "http://example.com/file.bin", true},
		{"https URL", "https://example.com/file.bin", true},
		{"URL without path", "https://example.com", true},
		{"magnet link", "magnet:?xt=urn:btih:abc", false},
		{"ftp URL", "ftp://example.com/file", false},
		{"missing host", "http://", false},
		{"relative path", "some/file.bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.IsDownloadable(tt.url)
			if got != tt.want {
				t.Errorf("IsDownloadable(%q) = %v; want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name unchanged", "file.txt", "file.txt"},
		{"unix traversal stripped", "../../etc/cron.d/evil", "evil"},
		{"windows traversal stripped", `..\..\Windows\system32\evil.exe`, "evil.exe"},
		{"absolute path stripped", "/etc/passwd", "passwd"},
		{"dot-dot alone rejected", "..", ""},
		{"dot alone rejected", ".", ""},
		{"slash alone rejected", "/", ""},
		{"empty rejected", "", ""},
		{"control chars removed", "fi\x00le\x1f.txt", "file.txt"},
		{"invalid windows chars replaced", `a<b>c:d"e|f?g*h.txt`, "a_b_c_d_e_f_g_h.txt"},
		{"trailing dots and spaces trimmed", "name.. ", "name"},
		{"dotfile preserved", ".bashrc", ".bashrc"},
		{"windows reserved name prefixed", "CON", "_CON"},
		{"windows reserved with extension prefixed", "com1.txt", "_com1.txt"},
		{"non-reserved similar name kept", "CONFIG.txt", "CONFIG.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.SanitizeFilename(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeFilename(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("over-long names are truncated below the filesystem limit", func(t *testing.T) {
		long := strings.Repeat("a", 300) + ".bin"

		got := httpmod.SanitizeFilename(long)
		if len(got) > 200 {
			t.Errorf("len = %d; want <= 200 (room for .part/.N suffixes under NAME_MAX)", len(got))
		}

		if !strings.HasSuffix(got, ".bin") {
			t.Errorf("extension not preserved: %q", got)
		}
	})

	t.Run("truncation does not split multi-byte runes", func(t *testing.T) {
		// 3-byte runes with a 4-byte extension: the 196-byte cut lands
		// mid-rune (196 % 3 == 1), forcing the backoff loop to trim.
		long := strings.Repeat("€", 100) + ".txt"

		got := httpmod.SanitizeFilename(long)
		if !utf8.ValidString(got) {
			t.Errorf("truncated name is not valid UTF-8: %q", got)
		}

		if len(got) > 200 {
			t.Errorf("len = %d; want <= 200", len(got))
		}

		if !strings.HasSuffix(got, ".txt") {
			t.Errorf("extension not preserved: %q", got)
		}
	})
}

func TestClient_Head(t *testing.T) {
	// Handler that returns 200 on HEAD
	okHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	// Handler that returns 404 on HEAD
	notFoundHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}

	tsOK := httptest.NewServer(http.HandlerFunc(okHandler))
	defer tsOK.Close()
	ts404 := httptest.NewServer(http.HandlerFunc(notFoundHandler))
	defer ts404.Close()

	client := httpmod.NewClient(0)

	t.Run("Head success", func(t *testing.T) {
		resp, err := client.Head(context.Background(), tsOK.URL, map[string]string{"X-Test": "value"})
		if err != nil {
			t.Fatalf("Head() error = %v; want nil", err)
		}
		resp.Body.Close()
	})

	t.Run("Head 404", func(t *testing.T) {
		_, err := client.Head(context.Background(), ts404.URL, nil)
		if !errors.Is(err, httpmod.ErrResourceNotFound) {
			t.Errorf("Head() error = %v; want ErrResourceNotFound", err)
		}
	})
}

func TestClient_Range(t *testing.T) {
	// Handler that returns 206 if Range header is present, else 200
	rangeHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		http.Error(w, "no range", http.StatusOK)
	}
	ts := httptest.NewServer(http.HandlerFunc(rangeHandler))
	defer ts.Close()

	client := httpmod.NewClient(0)

	t.Run("Range supported", func(t *testing.T) {
		resp, err := client.Range(context.Background(), ts.URL, 0, 0, nil)
		if err != nil {
			t.Fatalf("Range() error = %v; want nil", err)
		}
		resp.Body.Close()
	})

	t.Run("Range not supported", func(t *testing.T) {
		// Server returns 200 for GET with Range => should trigger ErrRangesNotSupported
		_, err := client.Range(context.Background(), ts.URL, 0, 0, nil)
		if err != nil && !errors.Is(err, httpmod.ErrRangesNotSupported) {
			t.Errorf("Range() error = %v; want ErrRangesNotSupported", err)
		}
	})
}

func TestClient_Get(t *testing.T) {
	okHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	notFoundHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}

	tsOK := httptest.NewServer(http.HandlerFunc(okHandler))
	defer tsOK.Close()
	ts404 := httptest.NewServer(http.HandlerFunc(notFoundHandler))
	defer ts404.Close()

	client := httpmod.NewClient(0)

	t.Run("Get success", func(t *testing.T) {
		resp, err := client.Get(context.Background(), tsOK.URL)
		if err != nil {
			t.Fatalf("Get() error = %v; want nil", err)
		}
		resp.Body.Close()
	})

	t.Run("Get 404", func(t *testing.T) {
		_, err := client.Get(context.Background(), ts404.URL)
		if !errors.Is(err, httpmod.ErrResourceNotFound) {
			t.Errorf("Get() error = %v; want ErrResourceNotFound", err)
		}
	})
}

func TestIsDownloadable_InvalidURL(t *testing.T) {
	got := httpmod.IsDownloadable("http://[::1]:named")
	if got {
		t.Errorf("IsDownloadable(invalid) = true; want false")
	}
}
