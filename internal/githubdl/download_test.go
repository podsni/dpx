package githubdl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer stands up an HTTP test server that mimics the GitHub
// REST and raw endpoints. handlers map URL paths to responses.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, h := range handlers {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadSingleFile(t *testing.T) {
	t.Parallel()

	const contents = "export const hello = 'world';\n"
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/podsni/openfolder/main/src/App.tsx": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(contents)))
			_, _ = w.Write([]byte(contents))
		},
	})

	c := NewClient("")
	c.Raw = srv.URL

	req, err := Parse("https://github.com/podsni/openfolder/blob/main/src/App.tsx")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	root := t.TempDir()
	if err := c.Download(context.Background(), req, Options{Root: root}); err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "src", "App.tsx"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != contents {
		t.Errorf("contents = %q, want %q", got, contents)
	}
}

func TestDownloadFolderRecursive(t *testing.T) {
	t.Parallel()

	// Mock two levels: a folder with two files, one of which has a
	// nested folder with one file.
	const fileContents = "import { Header } from './Header';\nexport default function App() { return <Header />; }\n"

	// The apiURL helper appends a leading "/" and "?ref=" so we
	// register exact paths that include the query string.
	apiRoot := "/repos/podsni/openfolder/contents"
	apiList := func(rel string, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, apiRoot) {
				http.NotFound(w, r)
				return
			}
			// path relative to /repos/<owner>/<repo>/contents/
			wantPath := apiRoot + "/" + rel
			wantPath = strings.TrimSuffix(wantPath, "/")
			gotPath := strings.TrimSuffix(r.URL.Path, "/")
			if gotPath != wantPath {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("ref") != "main" {
				http.Error(w, "missing ref", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}

	rootContents, _ := json.Marshal([]entry{
		{Name: "Header.tsx", Path: "src/Header.tsx", Type: "file", Size: int64(len(fileContents)), URL: "x"},
		{Name: "layout", Path: "src/layout", Type: "dir"},
		{Name: "skipme", Path: "src/skipme", Type: "submodule"},
	})
	layoutContents, _ := json.Marshal([]entry{
		{Name: "Header.tsx", Path: "src/layout/Header.tsx", Type: "file", Size: int64(len(fileContents)), URL: "x"},
	})

	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/podsni/openfolder/contents/src":         apiList("src", string(rootContents)),
		"/repos/podsni/openfolder/contents/src/layout":  apiList("src/layout", string(layoutContents)),
		"/podsni/openfolder/main/src/Header.tsx":        rawHandler(fileContents),
		"/podsni/openfolder/main/src/layout/Header.tsx": rawHandler(fileContents),
	})

	c := NewClient("")
	c.API = srv.URL
	c.Raw = srv.URL

	req, err := Parse("https://github.com/podsni/openfolder/tree/main/src")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	dst := t.TempDir()
	var progressCalls []string
	if err := c.Download(context.Background(), req, Options{
		Root: dst,
		Progress: func(n int, rel string, _ int64) {
			progressCalls = append(progressCalls, fmt.Sprintf("%d:%s", n, rel))
		},
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	for _, want := range []string{"src/Header.tsx", "src/layout/Header.tsx"} {
		path := filepath.Join(dst, filepath.FromSlash(want))
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("missing %s: %v", want, err)
			continue
		}
		if string(got) != fileContents {
			t.Errorf("contents of %s = %q, want %q", want, got, fileContents)
		}
	}
	if len(progressCalls) != 2 {
		t.Errorf("expected 2 progress calls, got %v", progressCalls)
	}
}

func rawHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write([]byte(body))
	}
}

func TestDownloadRateLimited(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/o/r/contents/f": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
		},
	})

	c := NewClient("")
	c.API = srv.URL

	req, _ := Parse("https://github.com/o/r/tree/main/f")
	root := t.TempDir()
	err := c.Download(context.Background(), req, Options{Root: root})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if !apiErr.IsRateLimited() {
		t.Errorf("expected rate-limited, status %d", apiErr.Status)
	}
}

func TestDownloadHonoursToken(t *testing.T) {
	t.Parallel()

	var gotAuth atomic.Value
	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/o/r/contents/": func(w http.ResponseWriter, r *http.Request) {
			gotAuth.Store(r.Header.Get("Authorization"))
			_, _ = w.Write([]byte("[]"))
		},
	})

	c := NewClient("ghp_testtoken")
	c.API = srv.URL

	req, _ := Parse("https://github.com/o/r/tree/main")
	if err := c.Download(context.Background(), req, Options{Root: t.TempDir()}); err != nil {
		t.Fatalf("download: %v", err)
	}

	if got := gotAuth.Load(); got != "Bearer ghp_testtoken" {
		t.Errorf("Authorization = %v, want Bearer ghp_testtoken", got)
	}
}

func TestDownloadWithRepoPrefix(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/o/r/main/README.md": rawHandler("# hello"),
	})
	c := NewClient("")
	c.Raw = srv.URL

	req, _ := Parse("https://github.com/o/r/blob/main/README.md")
	root := t.TempDir()
	if err := c.Download(context.Background(), req, Options{Root: root, RepoPrefix: true}); err != nil {
		t.Fatalf("download: %v", err)
	}
	path := filepath.Join(root, "r", "README.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s: %v", path, err)
	}
}

func TestDownloadRejectsTraversal(t *testing.T) {
	t.Parallel()

	c := NewClient("")
	req, err := Parse("https://github.com/o/r/blob/main/../../../etc/passwd")
	if err != nil {
		// Good: parser should reject the malformed path.
		return
	}

	root := t.TempDir()
	err = c.Download(context.Background(), req, Options{Root: root})
	if err == nil {
		t.Fatal("expected error for traversal path, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "invalid") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	if err := writeFileAtomic(target, strings.NewReader("v1")); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if err := writeFileAtomic(target, strings.NewReader("v2 longer content")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "v2 longer content" {
		t.Errorf("contents = %q, want %q", got, "v2 longer content")
	}
	// Confirm no leftover temp files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dpx-dlx-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteFileAtomicSizeLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "big.bin")

	big := strings.Repeat("x", int(MaxFileSize)+1)
	err := writeFileAtomic(target, strings.NewReader(big))
	if err == nil {
		t.Fatal("expected size limit error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no partial file at %s, got err=%v", target, err)
	}
}

func TestDownloadContextCancel(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/o/r/contents/": func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte("[]"))
		},
	})
	c := NewClient("")
	c.API = srv.URL

	req, _ := Parse("https://github.com/o/r/tree/main")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := c.Download(ctx, req, Options{Root: t.TempDir()})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
