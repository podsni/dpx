package httpdl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSrv(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestParseHTTPSOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want string // substring expected in error, or "" for success
	}{
		{"https url", "https://example.com/file.zip", ""},
		{"https with path", "https://example.com/a/b/c.txt", ""},
		{"empty", "", "url is required"},
		{"whitespace", "   ", "url is required"},
		{"http scheme", "http://example.com/file.zip", "only https"},
		{"ftp scheme", "ftp://example.com/file.zip", "only https"},
		{"file scheme", "file:///etc/passwd", "only https"},
		{"missing host", "https:///file.zip", "missing host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.url)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestInferFilenameFromURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want string
	}{
		{"https://example.com/file.zip", "file.zip"},
		{"https://example.com/a/b/c.tar.gz", "c.tar.gz"},
		{"https://example.com/", ""},
		{"https://example.com", ""},
		{"https://example.com/?q=1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := inferFilename(tc.url, "")
			if got != tc.want {
				t.Errorf("inferFilename(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestInferFilenameFromContentDisposition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		header string
		want   string
	}{
		{"", ""},
		{"attachment; filename=secret.env", "secret.env"},
		{"attachment; filename=\"spaces in name.txt\"", "spaces in name.txt"},
		{"attachment; filename*=UTF-8''hello%20world.txt", "hello world.txt"},
		{"inline", ""},
		{"garbage", ""},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			got := inferFilename("https://example.com/", tc.header)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDownloadWritesFile(t *testing.T) {
	t.Parallel()

	const body = "hello, world\n"
	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write([]byte(body))
	})

	c := NewClient()
	root := t.TempDir()
	if err := c.Download(context.Background(),
		Request{URL: srv.URL + "/data/hello.txt"},
		Options{Root: root}); err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestDownloadUsesContentDisposition(t *testing.T) {
	t.Parallel()

	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="backup-2026.tar.gz"`)
		_, _ = w.Write([]byte("pretend tarball"))
	})

	c := NewClient()
	root := t.TempDir()
	if err := c.Download(context.Background(),
		Request{URL: srv.URL + "/x"},
		Options{Root: root}); err != nil {
		t.Fatalf("download: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "backup-2026.tar.gz")); err != nil {
		t.Errorf("expected Content-Disposition filename on disk: %v", err)
	}
}

func TestDownloadOverridesFilename(t *testing.T) {
	t.Parallel()

	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="server-name.txt"`)
		_, _ = w.Write([]byte("x"))
	})

	c := NewClient()
	root := t.TempDir()
	if err := c.Download(context.Background(),
		Request{URL: srv.URL + "/x"},
		Options{Root: root, Filename: "client-name.txt"}); err != nil {
		t.Fatalf("download: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "client-name.txt")); err != nil {
		t.Errorf("expected override filename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "server-name.txt")); err == nil {
		t.Errorf("server name should not be used when override set")
	}
}

func TestDownloadRejectsFilenameWithPath(t *testing.T) {
	t.Parallel()

	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	})

	c := NewClient()
	root := t.TempDir()
	err := c.Download(context.Background(),
		Request{URL: srv.URL + "/x"},
		Options{Root: root, Filename: "../escape.txt"})
	if err == nil {
		t.Fatal("expected error for filename with path separator")
	}
	if !strings.Contains(err.Error(), "invalid filename") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadHonorsSizeLimit(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", 1024)
	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	})

	c := NewClient()
	c.MaxSize = 512
	root := t.TempDir()
	err := c.Download(context.Background(),
		Request{URL: srv.URL + "/big.bin"},
		Options{Root: root})
	if err == nil {
		t.Fatal("expected size limit error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadPropagatesStatusError(t *testing.T) {
	t.Parallel()

	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})

	c := NewClient()
	err := c.Download(context.Background(),
		Request{URL: srv.URL + "/missing"},
		Options{Root: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v", err)
	}
}

func TestDownloadFollowsRedirects(t *testing.T) {
	t.Parallel()

	const body = "redirected payload"
	final := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="real.txt"`)
		_, _ = w.Write([]byte(body))
	})

	redir := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/real-path", http.StatusTemporaryRedirect)
	})

	c := NewClient()
	root := t.TempDir()
	if err := c.Download(context.Background(),
		Request{URL: redir.URL + "/start"},
		Options{Root: root}); err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "real.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestDownloadReportsProgress(t *testing.T) {
	t.Parallel()

	srv := newSrv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1234567890"))
	})

	c := NewClient()
	var seen int64
	root := t.TempDir()
	if err := c.Download(context.Background(),
		Request{URL: srv.URL + "/x"},
		Options{Root: root, Progress: func(n int64) { seen = n }}); err != nil {
		t.Fatalf("download: %v", err)
	}
	if seen != 10 {
		t.Errorf("progress = %d, want 10", seen)
	}
}

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},
		{"with space.txt", "with space.txt"},
		{"a/b.txt", "a_b.txt"},
		{`a\b.txt`, "a_b.txt"},
		{"a:b.txt", "a_b.txt"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
