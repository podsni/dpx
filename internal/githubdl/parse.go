// Package githubdl downloads files and folders from public GitHub
// repositories using the web API. It is intentionally minimal and
// operates entirely over plain HTTPS — no git binary, no codeload
// tarball, no third-party SDK.
//
// Two URL shapes are supported:
//
//   - https://github.com/<owner>/<repo>/blob/<ref>/<path>
//   - https://github.com/<owner>/<repo>/tree/<ref>/<path>
//   - https://raw.githubusercontent.com/<owner>/<repo>/<ref>/<path>
//
// Files are fetched via raw.githubusercontent.com. Folders are
// resolved recursively through the GitHub Contents API at
// api.github.com/repos/<owner>/<repo>/contents/<path>?ref=<ref>.
//
// All filesystem operations go through safeio so they remain safe on
// Windows and Unix (path traversal, symlink checks, mode handling).
package githubdl

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// Mode distinguishes between downloading a single file vs. a folder.
type Mode int

const (
	// ModeUnknown is the zero value and is treated as an error.
	ModeUnknown Mode = iota
	// ModeFile downloads a single file.
	ModeFile
	// ModeFolder downloads a folder recursively.
	ModeFolder
)

func (m Mode) String() string {
	switch m {
	case ModeFile:
		return "file"
	case ModeFolder:
		return "folder"
	default:
		return "unknown"
	}
}

// Request describes what to download. Either Path (for ModeFile) or
// Dir (for ModeFolder) is meaningful — the other is empty.
type Request struct {
	Mode Mode
	// Owner and Repo identify the GitHub repository.
	Owner string
	Repo  string
	// Ref is the branch, tag, or commit SHA. Defaults to "HEAD" (the
	// repository default branch) when empty.
	Ref string
	// Path is the file path inside the repository (forward-slash,
	// not platform-specific). Empty for raw.githubusercontent URLs
	// that point at the repo root.
	Path string
}

// DefaultRef is used when the URL does not specify one (only happens
// for raw.githubusercontent URLs without a ref — which GitHub does
// not actually produce; we still handle the case for robustness).
const DefaultRef = "HEAD"

// MaxFileSize caps individual file downloads. The GitHub Contents API
// hard-limits file content at 100 MB for blobs, and the raw endpoint
// has no documented cap but can be abused. 100 MiB is a safe ceiling
// that catches accidents while still allowing large artifacts.
const MaxFileSize int64 = 100 * 1024 * 1024

// Parse accepts any of the supported URL shapes and returns a Request.
// It validates required components and rejects malformed input with a
// descriptive error.
func Parse(rawURL string) (Request, error) {
	if strings.TrimSpace(rawURL) == "" {
		return Request{}, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return Request{}, fmt.Errorf("invalid url: %w", err)
	}

	host := strings.ToLower(u.Host)
	switch {
	case host == "github.com":
		return parseGitHubUI(u)
	case host == "raw.githubusercontent.com":
		return parseRawHost(u)
	default:
		return Request{}, fmt.Errorf("unsupported host %q (only github.com and raw.githubusercontent.com)", u.Host)
	}
}

// parseGitHubUI handles the github.com web UI URLs. The path layout is
//
//	/<owner>/<repo>/blob/<ref>/<path...>     → file
//	/<owner>/<repo>/tree/<ref>/<path...>     → folder
//
// The ref segment is required — GitHub always emits it in shared links.
// A trailing slash after the path is tolerated.
func parseGitHubUI(u *url.URL) (Request, error) {
	parts := splitPath(u.Path)
	if len(parts) < 4 {
		return Request{}, fmt.Errorf("github url too short: expected /<owner>/<repo>/<kind>/<ref>/..., got %q", u.Path)
	}

	owner := parts[0]
	repo := parts[1]
	kind := parts[2]
	ref := parts[3]
	rest := parts[4:]

	if !validGitName(owner) {
		return Request{}, fmt.Errorf("invalid owner in url: %q", owner)
	}
	if !validGitName(repo) {
		return Request{}, fmt.Errorf("invalid repo in url: %q", repo)
	}
	if ref == "" {
		return Request{}, fmt.Errorf("missing ref (branch/tag) in url")
	}

	req := Request{Owner: owner, Repo: repo, Ref: ref}

	switch kind {
	case "blob":
		if len(rest) == 0 {
			return Request{}, fmt.Errorf("file url missing path: %s", u.Path)
		}
		req.Mode = ModeFile
		req.Path = strings.Join(rest, "/")
	case "tree":
		// /tree/<ref> with no further path means "the root at this ref".
		req.Mode = ModeFolder
		if len(rest) > 0 {
			req.Path = strings.Join(rest, "/")
		}
	default:
		return Request{}, fmt.Errorf("unsupported github url kind %q (expected 'blob' or 'tree')", kind)
	}

	return req, nil
}

// parseRawHost handles https://raw.githubusercontent.com/<owner>/<repo>/<ref>/<path>
// URLs. The ref segment is required because GitHub's raw CDN requires
// it for routing.
func parseRawHost(u *url.URL) (Request, error) {
	parts := splitPath(u.Path)
	// /<owner>/<repo>/<ref>/<path...>  →  4+ parts.
	if len(parts) < 4 {
		return Request{}, fmt.Errorf("raw.githubusercontent url too short: expected /<owner>/<repo>/<ref>/<path>, got %q", u.Path)
	}

	owner := parts[0]
	repo := parts[1]
	ref := parts[2]
	rest := parts[3:]

	if !validGitName(owner) {
		return Request{}, fmt.Errorf("invalid owner in url: %q", owner)
	}
	if !validGitName(repo) {
		return Request{}, fmt.Errorf("invalid repo in url: %q", repo)
	}
	if ref == "" {
		return Request{}, fmt.Errorf("missing ref in raw url")
	}

	return Request{
		Mode:  ModeFile,
		Owner: owner,
		Repo:  repo,
		Ref:   ref,
		Path:  strings.Join(rest, "/"),
	}, nil
}

// splitPath breaks a URL path into non-empty segments. Leading and
// trailing slashes are ignored.
func splitPath(p string) []string {
	cleaned := strings.Trim(p, "/")
	if cleaned == "" {
		return nil
	}
	return strings.Split(cleaned, "/")
}

// validGitName enforces the conservative subset of GitHub's naming
// rules that we rely on for filesystem safety. GitHub allows more
// characters, but rejecting exotic names here keeps the path-safe
// guarantees simple.
func validGitName(s string) bool {
	if s == "" || len(s) > 100 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// SafeLocalPath returns the destination path on disk for a file
// relative to the given root. It refuses any segment that would
// escape the root via "..", absolute paths, or null bytes. It also
// rejects Windows-reserved device names so that a file called
// "CON.tsx" or "nul" does not get rejected by the OS later.
//
// GitHub paths are forward-slash, so this function uses
// filepath.FromSlash and filepath.Clean to map them to the host OS.
func SafeLocalPath(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root is required")
	}
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("path contains null byte")
	}
	// Reject Windows device names at any segment. GitHub allows some
	// of these (NUL, CON, ...) but they cause confusing OS errors
	// later. This is a conservative guard.
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if isWindowsReservedName(seg) {
			return "", fmt.Errorf("path contains reserved name %q", seg)
		}
	}

	// Build the joined path on the host OS. Reject early if the user
	// gave an absolute or traversal path.
	full := filepath.Join(root, filepath.FromSlash(rel))
	rootClean := filepath.Clean(root)
	relFinal, err := filepath.Rel(rootClean, full)
	if err != nil {
		return "", fmt.Errorf("path %q invalid under root: %w", rel, err)
	}
	if relFinal == ".." || strings.HasPrefix(relFinal, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return full, nil
}

// isWindowsReservedName matches the reserved basenames listed in
// https://learn.microsoft.com/windows/win32/fileio/naming-a-file —
// the OS rejects these as filenames regardless of extension.
func isWindowsReservedName(name string) bool {
	base := name
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}
