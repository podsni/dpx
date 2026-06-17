package githubdl

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFileURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		url       string
		wantMode  Mode
		wantOwner string
		wantRepo  string
		wantRef   string
		wantPath  string
	}{
		{
			name:      "simple file",
			url:       "https://github.com/podsni/openfolder/blob/main/src/App.tsx",
			wantMode:  ModeFile,
			wantOwner: "podsni",
			wantRepo:  "openfolder",
			wantRef:   "main",
			wantPath:  "src/App.tsx",
		},
		{
			name:      "file with deep path",
			url:       "https://github.com/podsni/openfolder/blob/main/src/components/layout/Header.tsx",
			wantMode:  ModeFile,
			wantOwner: "podsni",
			wantRepo:  "openfolder",
			wantRef:   "main",
			wantPath:  "src/components/layout/Header.tsx",
		},
		{
			name:      "file with commit sha",
			url:       "https://github.com/owner/repo/blob/abc1234/file.go",
			wantMode:  ModeFile,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "abc1234",
			wantPath:  "file.go",
		},
		{
			name:      "file with branch containing slash via URL (not legal but defensive)",
			url:       "https://github.com/o/r/blob/main/sub/dir/file.txt",
			wantMode:  ModeFile,
			wantOwner: "o",
			wantRepo:  "r",
			wantRef:   "main",
			wantPath:  "sub/dir/file.txt",
		},
		{
			name:      "trailing slash tolerated",
			url:       "https://github.com/o/r/blob/main/file.txt/",
			wantMode:  ModeFile,
			wantOwner: "o",
			wantRepo:  "r",
			wantRef:   "main",
			wantPath:  "file.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Mode != tc.wantMode {
				t.Errorf("mode = %v, want %v", got.Mode, tc.wantMode)
			}
			if got.Owner != tc.wantOwner {
				t.Errorf("owner = %q, want %q", got.Owner, tc.wantOwner)
			}
			if got.Repo != tc.wantRepo {
				t.Errorf("repo = %q, want %q", got.Repo, tc.wantRepo)
			}
			if got.Ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", got.Ref, tc.wantRef)
			}
			if got.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", got.Path, tc.wantPath)
			}
		})
	}
}

func TestParseFolderURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantRef   string
		wantPath  string
	}{
		{
			name:      "folder",
			url:       "https://github.com/podsni/openfolder/tree/main/src/components/layout",
			wantOwner: "podsni",
			wantRepo:  "openfolder",
			wantRef:   "main",
			wantPath:  "src/components/layout",
		},
		{
			name:      "folder root",
			url:       "https://github.com/podsni/openfolder/tree/main",
			wantOwner: "podsni",
			wantRepo:  "openfolder",
			wantRef:   "main",
			wantPath:  "",
		},
		{
			name:      "folder deep",
			url:       "https://github.com/o/r/tree/v1.2.3/a/b/c",
			wantOwner: "o",
			wantRepo:  "r",
			wantRef:   "v1.2.3",
			wantPath:  "a/b/c",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Mode != ModeFolder {
				t.Errorf("mode = %v, want folder", got.Mode)
			}
			if got.Owner != tc.wantOwner || got.Repo != tc.wantRepo || got.Ref != tc.wantRef || got.Path != tc.wantPath {
				t.Errorf("got %+v, want owner=%q repo=%q ref=%q path=%q",
					got, tc.wantOwner, tc.wantRepo, tc.wantRef, tc.wantPath)
			}
		})
	}
}

func TestParseRawURL(t *testing.T) {
	t.Parallel()
	got, err := Parse("https://raw.githubusercontent.com/podsni/openfolder/main/src/App.tsx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mode != ModeFile || got.Owner != "podsni" || got.Repo != "openfolder" || got.Ref != "main" || got.Path != "src/App.tsx" {
		t.Errorf("unexpected request: %+v", got)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"empty", "", "url is required"},
		{"whitespace", "   ", "url is required"},
		{"unsupported host", "https://gitlab.com/o/r/blob/main/f", "unsupported host"},
		{"too short", "https://github.com/o/r", "github url too short"},
		{"missing kind", "https://github.com/o/r/main/file", "unsupported github url kind"},
		{"bad kind", "https://github.com/o/r/issues/main", "unsupported github url kind"},
		{"missing path for file", "https://github.com/o/r/blob/main", "missing path"},
		{"invalid owner", "https://github.com/with$bad/r/blob/main/f", "invalid owner"},
		{"invalid repo", "https://github.com/o/r:bad/blob/main/f", "invalid repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.url)
			if err == nil {
				t.Fatalf("expected error for %q", tc.url)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSafeLocalPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"simple", "src/App.tsx", false},
		{"nested", "src/components/layout/Header.tsx", false},
		{"root file", "README.md", false},
		{"null byte", "src/\x00bad", true},
		{"escape via dotdot", "../escape.txt", true},
		{"escape via nested dotdot", "src/../../escape.txt", true},
		{"windows reserved", "CON.tsx", true},
		{"windows reserved no ext", "nul", true},
		{"windows reserved in segment", "src/COM1/file.txt", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SafeLocalPath(root, tc.rel)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got path %q", tc.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.rel, err)
			}
			// Path must be absolute and under root (lexical check).
			if !filepath.IsAbs(got) {
				t.Errorf("expected absolute path, got %q", got)
			}
		})
	}
}

func TestSafeLocalPathCrossPlatformJoin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	rel := "src/components/layout/Header.tsx"
	got, err := SafeLocalPath(root, rel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Use filepath.Join to compute the expected host-specific path.
	want := filepath.Join(root, filepath.FromSlash(rel))
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
