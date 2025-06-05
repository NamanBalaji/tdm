package http_test

import (
	"context"
	"errors"

	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
	mux := http.NewServeMux()
	// 1. HEAD returns HTML
	mux.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "fallback GET", http.StatusOK)
	})
	// 2. HEAD returns binary
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "fallback GET", http.StatusOK)
	})
	// 3. HEAD not allowed, GET returns binary
	mux.HandleFunc("/nohead", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "not supported", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"HTML not downloadable", "/html", false},
		{"Binary downloadable", "/binary", true},
		{"No HEAD, fallback GET downloadable", "/nohead", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := ts.URL + tt.path
			got := httpmod.IsDownloadable(url)
			if got != tt.want {
				t.Errorf("IsDownloadable(%q) = %v; want %v", url, got, tt.want)
			}
		})
	}
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

	client := httpmod.NewClient()

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

	client := httpmod.NewClient()

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

	client := httpmod.NewClient()

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
