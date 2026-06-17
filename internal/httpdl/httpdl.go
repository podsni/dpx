// Package httpdl downloads arbitrary files over HTTPS. It is the
// generic counterpart to internal/githubdl and powers `dpx dlx`
// when the URL does not point at GitHub.
//
// Features:
//   - Streaming writes with a configurable size cap (decoded bytes).
//   - Filename inference from URL basename or Content-Disposition.
//   - Re-uses the same atomic write primitive as githubdl so
//     partially-written files never appear on disk.
//   - Plain HTTPS only. file://, ftp://, gopher://, etc. are rejected.
//
// Folder / recursive downloads are not supported here; only GitHub
// has an API for listing directory contents. For a generic HTTPS
// folder, point at a tarball URL instead.
package httpdl

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultMaxSize is the per-file ceiling when MaxSize is zero. 100
// MiB matches the githubdl cap so users get consistent behavior
// regardless of which URL shape they use.
const DefaultMaxSize int64 = 100 * 1024 * 1024

// Request describes what to download.
type Request struct {
	// URL is the absolute https URL to fetch.
	URL string
	// Filename, if non-empty, overrides the inferred name. It must
	// be a basename, not a path — nested paths are rejected.
	Filename string
}

// Client downloads a single file per call. Transport is safe to share
// across goroutines.
type Client struct {
	HTTP    *http.Client
	MaxSize int64 // 0 means DefaultMaxSize
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
		MaxSize: DefaultMaxSize,
	}
}

// Parse accepts any https URL. It rejects non-https schemes and
// empty input. The returned Filename is empty unless the caller
// provides one — the inferred name is computed at download time
// from headers and the URL.
func Parse(rawURL string) (Request, error) {
	if strings.TrimSpace(rawURL) == "" {
		return Request{}, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return Request{}, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "https" {
		return Request{}, fmt.Errorf("only https is supported, got %q", u.Scheme)
	}
	if u.Host == "" {
		return Request{}, fmt.Errorf("url is missing host")
	}
	return Request{URL: u.String()}, nil
}

// Options controls filesystem layout and progress reporting.
type Options struct {
	// Root is the destination directory. Created if missing.
	Root string
	// Filename, when set, overrides the name inferred from the URL
	// or headers. Must be a basename only — paths are rejected.
	Filename string
	// Progress, if non-nil, is called once the file is on disk.
	// size is the decoded byte count.
	Progress func(size int64)
}

// Download fetches req.URL and writes it under opts.Root using the
// inferred or overridden filename. It enforces opts.MaxSize (or
// NewClient's default) while streaming.
func (c *Client) Download(ctx context.Context, req Request, opts Options) error {
	if opts.Root == "" {
		return fmt.Errorf("output root is required")
	}
	if err := os.MkdirAll(opts.Root, 0o755); err != nil {
		return fmt.Errorf("create root: %w", err)
	}

	maxSize := c.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("User-Agent", "dpx-httpdl/1.0")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http: status %d", resp.StatusCode)
	}

	name := opts.Filename
	if name == "" {
		name = req.Filename
	}
	if name == "" {
		name = inferFilename(req.URL, resp.Header.Get("Content-Disposition"))
	}
	if name == "" {
		name = "download.bin"
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid filename %q", name)
	}

	dst := filepath.Join(opts.Root, filepath.FromSlash(name))
	if err := writeCapped(dst, resp.Body, maxSize); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}

	if opts.Progress != nil {
		opts.Progress(fileSize(dst))
	}
	return nil
}

// inferFilename picks a sensible basename from the URL, falling back
// to the Content-Disposition header. Returns "" if nothing usable
// is found (the caller then uses a generic default).
func inferFilename(rawURL, contentDisposition string) string {
	if name := filenameFromContentDisposition(contentDisposition); name != "" {
		return name
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return ""
	}
	// URL paths are always POSIX-style ("/"), so use path.Base rather
	// than filepath.Base — on Windows the latter converts "/" to "\"
	// and yields a backslash instead of an empty basename.
	base := path.Base(u.Path)
	// Strip query-ish suffix that url.Parse may have left in.
	if idx := strings.IndexByte(base, '?'); idx >= 0 {
		base = base[:idx]
	}
	if base == "" || base == "." || base == "/" {
		return ""
	}
	// Reject names with control chars or that are too long.
	if len(base) > 200 {
		base = base[len(base)-200:]
	}
	for _, r := range base {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return base
}

// filenameFromContentDisposition extracts filename="..." or
// filename*=UTF-8”... per RFC 6266. Returns "" when no usable
// value is present.
func filenameFromContentDisposition(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	if name, ok := params["filename"]; ok && name != "" {
		return sanitizeFilename(name)
	}
	if name, ok := params["filename*"]; ok && name != "" {
		// Strip the RFC 5987 encoding (e.g. "utf-8''hello.txt" -> "hello.txt").
		if idx := strings.Index(name, "''"); idx >= 0 {
			name = name[idx+2:]
		}
		return sanitizeFilename(name)
	}
	return ""
}

// filenameReserved matches characters that are unsafe in filenames
// across platforms (control + Windows-reserved set).
var filenameReserved = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filenameReserved.ReplaceAllString(name, "_")
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// writeCapped streams r to a temp file in the same directory, then
// atomically renames. Rejects anything larger than maxSize.
func writeCapped(path string, r io.Reader, maxSize int64) (retErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dpx-dlx-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpName)
		}
	}()

	limited := &capReader{r: r, max: maxSize}
	written, err := io.Copy(tmp, limited)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if limited.truncated {
		_ = tmp.Close()
		return fmt.Errorf("file too large (exceeds %d bytes)", maxSize)
	}
	if written == 0 {
		_ = tmp.Close()
		return fmt.Errorf("empty response")
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("rename: %w (also: %v)", err, removeErr)
		}
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
	}
	return nil
}

// capReader stops reading once max bytes have been observed. The
// reader is single-use.
type capReader struct {
	r         io.Reader
	max       int64
	n         int64
	truncated bool
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.truncated {
		return 0, io.EOF
	}
	remaining := c.max + 1 - c.n
	if remaining <= 0 {
		c.truncated = true
		return 0, io.EOF
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
