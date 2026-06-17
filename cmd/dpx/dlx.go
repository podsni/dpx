package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dwirx/dpx/internal/githubdl"
	"github.com/dwirx/dpx/internal/httpdl"
)

type dlxArgs struct {
	urls     []string
	output   string
	token    string
	ref      string
	noPrefix bool
	glob     string
	maxSize  int64
	quiet    bool
}

func parseDlxArgs(args []string, opts runOptions) (dlxArgs, error) {
	fs := flag.NewFlagSet("dlx", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	output := fs.String("output", "", "destination directory (default: current working directory)")
	outputShort := fs.String("o", "", "alias for --output")
	token := fs.String("token", "", "GitHub personal access token (also reads DPX_GITHUB_TOKEN); only used for github.com URLs")
	ref := fs.String("ref", "", "override branch/tag/SHA (only used for github.com URLs)")
	noPrefix := fs.Bool("no-prefix", false, "do not nest GitHub downloads under <repo>/ (write paths verbatim)")
	glob := fs.String("glob", "", "restrict GitHub folder downloads to files matching this pattern (e.g. '*.go', 'src/*.tsx')")
	maxSize := fs.Int64("max-size", 0, "per-file size cap in bytes (0 = default: 100 MiB)")
	quiet := fs.Bool("quiet", false, "suppress per-file progress output")
	if err := fs.Parse(args); err != nil {
		return dlxArgs{}, err
	}

	out := *output
	if out == "" {
		out = *outputShort
	}

	urls := fs.Args()
	if len(urls) == 0 {
		return dlxArgs{}, fmt.Errorf("dlx requires at least one url argument")
	}

	return dlxArgs{
		urls:     urls,
		output:   out,
		token:    firstNonEmpty(*token, os.Getenv("DPX_GITHUB_TOKEN")),
		ref:      *ref,
		noPrefix: *noPrefix,
		glob:     *glob,
		maxSize:  *maxSize,
		quiet:    *quiet,
	}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// isGitHubURL reports whether the URL targets a GitHub host that the
// dlx command can expand (file or folder). Both the web UI host and
// the raw CDN are accepted; other hosts fall through to httpdl.
func isGitHubURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "github.com" || host == "raw.githubusercontent.com"
}

type dlSummary struct {
	URL    string
	Mode   string
	Files  int
	Bytes  int64
	Output string
}

func runDlx(args []string, opts runOptions) error {
	parsed, err := parseDlxArgs(args, opts)
	if err != nil {
		return err
	}

	root := parsed.output
	if root == "" {
		root = opts.cwd
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	summaries := make([]dlSummary, 0, len(parsed.urls))
	for _, rawURL := range parsed.urls {
		sum, err := runOne(ctx, rawURL, parsed, root, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", rawURL, err)
		}
		summaries = append(summaries, sum)
	}

	if !parsed.quiet && opts.stdout != nil {
		printDlxSummary(opts.stdout, summaries)
	}
	return nil
}

// runOne dispatches a single URL to the appropriate downloader.
// GitHub URLs are tried through githubdl first; URLs that don't
// match a known GitHub layout (e.g. archive tarballs at
// /archive/refs/heads/<ref>.tar.gz) fall back to httpdl so the
// user can still download them with a single command.
func runOne(ctx context.Context, rawURL string, parsed dlxArgs, root string, opts runOptions) (dlSummary, error) {
	if isGitHubURL(rawURL) {
		sum, err := runGitHubOne(ctx, rawURL, parsed, root, opts)
		if err == nil {
			return sum, nil
		}
		if !isUnsupportedGitHubLayout(err) {
			return dlSummary{}, err
		}
		if !parsed.quiet && opts.stdout != nil {
			fmt.Fprintf(opts.stdout, "   (not a GitHub file/folder layout, falling back to generic HTTPS)\n")
		}
	}
	return runHTTPOne(ctx, rawURL, parsed, root, opts)
}

// isUnsupportedGitHubLayout reports whether err comes from the
// githubdl parser rejecting a URL that is on github.com but does
// not match the /blob/ or /tree/ shapes — typically archive
// tarballs or other server-rendered endpoints.
func isUnsupportedGitHubLayout(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unsupported github url kind") ||
		strings.Contains(msg, "github url too short")
}

func runGitHubOne(ctx context.Context, rawURL string, parsed dlxArgs, root string, opts runOptions) (dlSummary, error) {
	req, err := githubdl.Parse(rawURL)
	if err != nil {
		return dlSummary{}, fmt.Errorf("parse url: %w", err)
	}
	if parsed.ref != "" {
		req.Ref = parsed.ref
	}

	client := githubdl.NewClient(parsed.token)

	dlOpts := githubdl.Options{
		Root:       root,
		RepoPrefix: !parsed.noPrefix,
		Glob:       parsed.glob,
		MaxSize:    parsed.maxSize,
	}
	if !parsed.quiet && opts.stdout != nil {
		fmt.Fprintf(opts.stdout, "\n-> %s\n", rawURL)
		fmt.Fprintf(opts.stdout, "   Mode:   %s | Repo: %s/%s | Ref: %s\n",
			req.Mode, req.Owner, req.Repo, req.Ref)
		if req.Path != "" {
			fmt.Fprintf(opts.stdout, "   Path:   %s\n", req.Path)
		}
		if parsed.glob != "" {
			fmt.Fprintf(opts.stdout, "   Glob:   %s\n", parsed.glob)
		}
		dlOpts.Progress = func(n int, rel string, size int64) {
			fmt.Fprintf(opts.stdout, "   [%d] %s (%s)\n", n, rel, humanBytes(size))
		}
	}

	if err := client.Download(ctx, req, dlOpts); err != nil {
		return dlSummary{}, err
	}
	return dlSummary{URL: rawURL, Mode: req.Mode.String(), Output: root}, nil
}

func runHTTPOne(ctx context.Context, rawURL string, parsed dlxArgs, root string, opts runOptions) (dlSummary, error) {
	req, err := httpdl.Parse(rawURL)
	if err != nil {
		return dlSummary{}, fmt.Errorf("parse url: %w", err)
	}
	client := httpdl.NewClient()
	if parsed.maxSize > 0 {
		client.MaxSize = parsed.maxSize
	}

	if !parsed.quiet && opts.stdout != nil {
		fmt.Fprintf(opts.stdout, "\n-> %s\n", rawURL)
		fmt.Fprintf(opts.stdout, "   Mode:   file (generic https)\n")
	}
	dlOpts := httpdl.Options{Root: root}
	if !parsed.quiet && opts.stdout != nil {
		dlOpts.Progress = func(n int64) {
			fmt.Fprintf(opts.stdout, "   -> %s (%s)\n", savedFilename(root), humanBytes(n))
		}
	}

	if err := client.Download(ctx, req, dlOpts); err != nil {
		return dlSummary{}, err
	}
	return dlSummary{URL: rawURL, Mode: "file", Output: root}, nil
}

// savedFilename returns the most recently created file in root, for
// progress reporting of single-file downloads. Empty string if root
// is empty or contains nothing new.
func savedFilename(root string) string {
	if root == "" {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		return ""
	}
	type candidate struct {
		name    string
		modTime time.Time
	}
	var newest candidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest.modTime) {
			newest = candidate{name: info.Name(), modTime: info.ModTime()}
		}
	}
	return newest.name
}

func printDlxSummary(w ioWriter, sums []dlSummary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Summary:")
	for _, s := range sums {
		fmt.Fprintf(w, "  - %s (%s)\n", s.URL, s.Mode)
	}
	fmt.Fprintf(w, "  Total: %d URL(s) -> %s\n", len(sums), sums[0].Output)
}

// humanBytes formats a byte count using binary multiples. Returns "?"
// for negative or unknown sizes.
func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const (
		kib = 1024
		mib = 1024 * 1024
		gib = 1024 * 1024 * 1024
	)
	switch {
	case n < kib:
		return fmt.Sprintf("%d B", n)
	case n < mib:
		return fmt.Sprintf("%.1f KB", float64(n)/kib)
	case n < gib:
		return fmt.Sprintf("%.1f MB", float64(n)/mib)
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/gib)
	}
}

// IoWriter is an alias kept short to avoid an import collision in
// the helper above. It is the same as io.Writer.
type ioWriter = interface {
	Write(p []byte) (int, error)
}
