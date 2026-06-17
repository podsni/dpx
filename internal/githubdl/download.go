package githubdl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client downloads GitHub files and folders over HTTPS. It is safe
// for concurrent use; transport state is read-only after construction.
type Client struct {
	HTTP  *http.Client
	Token string // optional GitHub PAT for higher rate limit
	API   string // overridable for tests / GitHub Enterprise
	Raw   string // overridable for tests / GitHub Enterprise
}

// NewClient returns a Client using a sensible default HTTP transport
// and timeouts. token may be empty for anonymous access (60 req/hour
// per IP).
func NewClient(token string) *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: 30 * time.Second},
		Token: token,
		API:   "https://api.github.com",
		Raw:   "https://raw.githubusercontent.com",
	}
}

// Options controls filesystem layout and progress reporting.
type Options struct {
	// Root is the destination directory. Created if missing.
	Root string
	// RepoPrefix, when true, places all files under <Root>/<repo>/...
	// so multiple downloads from different repos do not collide.
	RepoPrefix bool
	// Progress, if non-nil, is called after each file is written. The
	// counter starts at 1; the path is the relative path inside Root.
	Progress func(downloaded int, relPath string, size int64)
}

// Entry is a single item in a GitHub Contents API response.
type entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
	Size int64  `json:"size"`
	URL  string `json:"download_url"`
}

// APIError surfaces a non-2xx response from the GitHub API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github api: status %d: %s", e.Status, truncate(e.Body, 200))
}

// IsRateLimited reports whether an APIError corresponds to GitHub's
// secondary rate limit or primary rate limit.
func (e *APIError) IsRateLimited() bool {
	return e.Status == http.StatusForbidden || e.Status == http.StatusTooManyRequests
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Download executes a request and writes the resulting file(s) to
// disk under opts.Root. It is the single entry point for both files
// and folders.
func (c *Client) Download(ctx context.Context, req Request, opts Options) error {
	if opts.Root == "" {
		return fmt.Errorf("output root is required")
	}
	switch req.Mode {
	case ModeFile:
		return c.downloadFile(ctx, req, opts)
	case ModeFolder:
		return c.downloadFolder(ctx, req, opts)
	default:
		return fmt.Errorf("unknown request mode")
	}
}

func (c *Client) localRoot(opts Options, req Request) string {
	if opts.RepoPrefix {
		return filepath.Join(opts.Root, req.Repo)
	}
	return opts.Root
}

func (c *Client) downloadFile(ctx context.Context, req Request, opts Options) error {
	root := c.localRoot(opts, req)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root: %w", err)
	}

	rel := req.Path
	if rel == "" {
		// /<owner>/<repo>/blob/<ref>/  with no file — pick a default name.
		rel = req.Repo + ".txt"
	}
	dst, err := SafeLocalPath(root, rel)
	if err != nil {
		return err
	}

	src, _, err := c.fetchRaw(ctx, req)
	if err != nil {
		return err
	}
	defer src.Close()

	cr, ok := src.(*countingReader)
	if !ok {
		return fmt.Errorf("internal: fetchRaw did not return countingReader")
	}
	if err := writeFileAtomic(dst, src); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	size := cr.n
	if opts.Progress != nil {
		opts.Progress(1, rel, size)
	}
	return nil
}

// fetchRaw GETs the raw file bytes and returns the body plus its size
// in bytes (the actual decoded size when the response is compressed,
// since Go's transport transparently decompresses). It enforces
// MaxFileSize while streaming; the limit applies to decoded bytes.
func (c *Client) fetchRaw(ctx context.Context, req Request) (io.ReadCloser, int64, error) {
	target, err := c.rawURL(req)
	if err != nil {
		return nil, 0, err
	}
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	req2.Header.Set("User-Agent", "dpx-githubdl/1.0")
	resp, err := c.HTTP.Do(req2)
	if err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, &APIError{Status: resp.StatusCode, Body: readAll(resp.Body)}
	}
	// GitHub sends Transfer-Encoding: chunked for raw files, and Go's
	// transport transparently decompresses gzip, so resp.ContentLength
	// is unreliable. Wrap the body in a counter so the caller learns
	// the decoded size after streaming.
	cr := &countingReader{r: resp.Body}
	return cr, -1, nil
}

// countingReader counts the bytes that flow through it. It is safe
// for one-shot reads only — do not interleave calls.
type countingReader struct {
	r        io.ReadCloser
	n        int64
	overflow bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > MaxFileSize {
		c.overflow = true
	}
	return n, err
}

func (c *countingReader) Close() error {
	return c.r.Close()
}

func (c *Client) rawURL(req Request) (string, error) {
	u, err := url.Parse(c.Raw)
	if err != nil {
		return "", err
	}
	ref := req.Ref
	if ref == "" {
		ref = DefaultRef
	}
	parts := []string{u.Path, req.Owner, req.Repo, ref, req.Path}
	u.Path = pathJoin(parts)
	return u.String(), nil
}

func (c *Client) apiURL(req Request, subpath string) (string, error) {
	u, err := url.Parse(c.API)
	if err != nil {
		return "", err
	}
	ref := req.Ref
	if ref == "" {
		ref = DefaultRef
	}
	q := u.Query()
	q.Set("ref", ref)
	u.RawQuery = q.Encode()
	parts := []string{u.Path, "repos", req.Owner, req.Repo, "contents"}
	if subpath != "" {
		parts = append(parts, subpath)
	}
	u.Path = pathJoin(parts)
	return u.String(), nil
}

// pathJoin joins path segments with exactly one "/" between each,
// handling empty segments cleanly.
func pathJoin(parts []string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, strings.Trim(p, "/"))
		}
	}
	return "/" + strings.Join(cleaned, "/")
}

func (c *Client) downloadFolder(ctx context.Context, req Request, opts Options) error {
	root := c.localRoot(opts, req)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root: %w", err)
	}

	count := 0
	return c.walkFolder(ctx, req, root, func(rel string, size int64) error {
		count++
		if opts.Progress != nil {
			opts.Progress(count, rel, size)
		}
		return nil
	})
}

// walkFolder recursively visits every file under req. For each file
// it calls onFile with the path relative to root. The walk is
// depth-first and pre-order (directories are entered before their
// files are visited).
func (c *Client) walkFolder(ctx context.Context, req Request, root string, onFile func(rel string, size int64) error) error {
	var recurse func(rel string) error
	recurse = func(rel string) error {
		entries, err := c.listContents(ctx, req, rel)
		if err != nil {
			return err
		}
		for _, e := range entries {
			switch e.Type {
			case "dir":
				if err := recurse(e.Path); err != nil {
					return err
				}
			case "file":
				// Fetch the file. We re-derive the request path from
				// the entry to avoid relying on the API's URL field,
				// which can be a permalink with a different ref.
				sub := req
				sub.Path = e.Path
				rc, _, err := c.fetchRaw(ctx, sub)
				if err != nil {
					return fmt.Errorf("download %s: %w", e.Path, err)
				}
				cr, ok := rc.(*countingReader)
				if !ok {
					_ = rc.Close()
					return fmt.Errorf("internal: fetchRaw did not return countingReader")
				}
				dst, err := SafeLocalPath(root, e.Path)
				if err != nil {
					_ = rc.Close()
					return err
				}
				if err := writeFileAtomic(dst, rc); err != nil {
					_ = rc.Close()
					return fmt.Errorf("write %s: %w", dst, err)
				}
				_ = rc.Close()
				if err := onFile(e.Path, cr.n); err != nil {
					return err
				}
			case "submodule", "symlink":
				// Skip — downloading submodules requires cloning, and
				// symlinks could escape the destination tree.
				continue
			default:
				return fmt.Errorf("unexpected entry type %q for %s", e.Type, e.Path)
			}
		}
		return nil
	}
	return recurse(req.Path)
}

func (c *Client) listContents(ctx context.Context, req Request, subpath string) ([]entry, error) {
	target, err := c.apiURL(req, subpath)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", "dpx-githubdl/1.0")
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()
	body := readAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{Status: resp.StatusCode, Body: body}
	}
	var entries []entry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}
	return entries, nil
}

// readAll reads the body fully, capped at 1 MiB. Used for error
// reporting where the body should be small.
func readAll(r io.Reader) string {
	const limit = 1 << 20
	buf, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return ""
	}
	return string(buf)
}

// writeFileAtomic writes data to a temporary file in the same
// directory and then renames it over the destination. This avoids
// leaving a half-written file behind if the process is killed.
//
// On Windows, os.Rename fails if the destination exists, so we fall
// back to remove-then-rename. The window is small but acceptable
// because we are downloading into a directory we just created.
func writeFileAtomic(path string, r io.Reader) (retErr error) {
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

	// Cap at MaxFileSize even when the server does not advertise it.
	limited := io.LimitReader(r, MaxFileSize+1)
	written, err := io.Copy(tmp, limited)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if written > MaxFileSize {
		_ = tmp.Close()
		return fmt.Errorf("file too large (exceeds %d bytes)", MaxFileSize)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("rename: %w (also: %v)", err, removeErr)
		}
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
	}
	return nil
}
