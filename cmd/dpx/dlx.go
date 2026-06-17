package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dwirx/dpx/internal/githubdl"
)

type dlxArgs struct {
	url      string
	output   string
	token    string
	ref      string
	noPrefix bool
	quiet    bool
}

func parseDlxArgs(args []string, opts runOptions) (dlxArgs, error) {
	fs := flag.NewFlagSet("dlx", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	output := fs.String("output", "", "destination directory (default: current working directory)")
	outputShort := fs.String("o", "", "alias for --output")
	token := fs.String("token", "", "GitHub personal access token for higher rate limit (also reads DPX_GITHUB_TOKEN)")
	ref := fs.String("ref", "", "override branch/tag/SHA from the URL")
	noPrefix := fs.Bool("no-prefix", false, "do not nest downloads under <repo>/ (write paths verbatim)")
	quiet := fs.Bool("quiet", false, "suppress per-file progress output")
	if err := fs.Parse(args); err != nil {
		return dlxArgs{}, err
	}
	if fs.NArg() != 1 {
		return dlxArgs{}, fmt.Errorf("dlx requires exactly one url argument, got %d", fs.NArg())
	}

	out := *output
	if out == "" {
		out = *outputShort
	}

	return dlxArgs{
		url:      fs.Arg(0),
		output:   out,
		token:    firstNonEmpty(*token, os.Getenv("DPX_GITHUB_TOKEN")),
		ref:      *ref,
		noPrefix: *noPrefix,
		quiet:    *quiet,
	}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func runDlx(args []string, opts runOptions) error {
	parsed, err := parseDlxArgs(args, opts)
	if err != nil {
		return err
	}

	req, err := githubdl.Parse(parsed.url)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if parsed.ref != "" {
		req.Ref = parsed.ref
	}

	root := parsed.output
	if root == "" {
		root = opts.cwd
	}
	// MkdirAll up front so the user gets a clear error if the path is
	// invalid before we make any network calls.
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	client := githubdl.NewClient(parsed.token)

	dlOpts := githubdl.Options{
		Root:       root,
		RepoPrefix: !parsed.noPrefix,
	}
	if !parsed.quiet && opts.stdout != nil {
		dlOpts.Progress = func(n int, rel string, size int64) {
			fmt.Fprintf(opts.stdout, "  [%d] %s (%s)\n", n, rel, humanBytes(size))
		}
	}

	if !parsed.quiet && opts.stdout != nil {
		fmt.Fprintf(opts.stdout, "Mode:     %s\n", req.Mode)
		fmt.Fprintf(opts.stdout, "Repo:     %s/%s\n", req.Owner, req.Repo)
		fmt.Fprintf(opts.stdout, "Ref:      %s\n", req.Ref)
		if req.Path != "" {
			fmt.Fprintf(opts.stdout, "Path:     %s\n", req.Path)
		}
		fmt.Fprintf(opts.stdout, "Output:   %s\n", root)
		fmt.Fprintln(opts.stdout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := client.Download(ctx, req, dlOpts); err != nil {
		return err
	}

	if !parsed.quiet && opts.stdout != nil {
		fmt.Fprintln(opts.stdout, "Done.")
	}
	return nil
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
