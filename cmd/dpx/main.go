package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/dwirx/dpx/internal/app"
	"github.com/dwirx/dpx/internal/config"
	"github.com/dwirx/dpx/internal/crypto/agex"
	"github.com/dwirx/dpx/internal/discovery"
	"github.com/dwirx/dpx/internal/envelope"
	"github.com/dwirx/dpx/internal/policy"
	"github.com/dwirx/dpx/internal/safeio"
	"github.com/dwirx/dpx/internal/selfupdate"
	"github.com/dwirx/dpx/internal/tui"
)

var version = "dev"

var (
	runSelfUpdate   = selfupdate.Update
	runSelfRollback = selfupdate.Rollback
	runtimeGOOS     = runtime.GOOS
)

const (
	appName          = "dpx"
	primaryConfig    = ".dpx.yaml"
	legacyConfig     = ".dopx.yaml"
	doctorTitle      = "DPX Doctor"
	legacyDoctorNote = "legacy"
)

type runOptions struct {
	cwd    string
	stdin  io.Reader
	reader *bufio.Reader
	stdout io.Writer
	stderr io.Writer

	forcePassword bool
}

type configSource struct {
	Path   string
	Exists bool
	Legacy bool
}

type doctorReport struct {
	Config         configSource
	ConfigError    error
	KeyPath        string
	KeyExists      bool
	KeyUsesLegacy  bool
	RecipientCount int
	SuggestedFiles int
	EncryptedFiles int
}

func main() {
	if err := run(os.Args[1:], runOptions{
		cwd:    mustGetwd(),
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", humanizeError(err))
		os.Exit(1)
	}
}

// humanizeError translates low-level errors into actionable messages with
// suggestions. Returns err unchanged when no friendly mapping exists.
func humanizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// Authentication failures from AEAD ciphers surface as low-level crypto
	// messages. Detect the common patterns and present a single, clear cause.
	if strings.Contains(msg, "chacha20poly1305: message authentication failed") ||
		strings.Contains(msg, "hmac mismatch") ||
		strings.Contains(msg, "authentication failed") {
		return fmt.Errorf("wrong password or corrupted file (password backend): %w", err)
	}

	// Strip os.* syscall framing that leaks "openat", "read", "stat", etc.,
	// because end users do not care which syscall failed — only the path.
	// Go's *PathError formats as "<op> <path>: <errno>".
	for _, prefix := range []string{"openat ", "open ", "read ", "stat ", "lstat ", "remove ", "rename "} {
		if !strings.HasPrefix(msg, prefix) {
			continue
		}
		parts := strings.SplitN(msg, ": ", 2)
		if len(parts) != 2 {
			break
		}
		// "<op> <path>" — recover the path.
		pathAndOp := strings.TrimPrefix(parts[0], prefix)
		return fmt.Errorf("file not found or unreadable: %s: %s", pathAndOp, parts[1])
	}

	// Unknown command — keep current message but suggest running help.
	if strings.HasPrefix(msg, "unknown command ") || strings.HasPrefix(msg, "unknown env subcommand ") {
		return fmt.Errorf("%w\n  hint: run 'dpx help' to see available commands", err)
	}

	return err
}

func run(args []string, opts runOptions) error {
	if opts.cwd == "" {
		opts.cwd = mustGetwd()
	}
	if opts.stdin == nil {
		opts.stdin = strings.NewReader("")
	}
	if opts.reader == nil {
		opts.reader = bufio.NewReader(opts.stdin)
	}
	if opts.stdout == nil {
		opts.stdout = io.Discard
	}
	if opts.stderr == nil {
		opts.stderr = io.Discard
	}
	if len(args) == 0 {
		printUsage(opts.stdout)
		return nil
	}

	command, err := resolveCommand(args[0])
	if err != nil {
		if isDirectFileInvocation(args[0], opts.cwd) {
			cfg, _, cfgErr := loadConfig(opts.cwd)
			if cfgErr != nil {
				return cfgErr
			}
			svc := app.New(cfg)
			return runDirectPathInvocation(svc, cfg, args, opts)
		}
		return err
	}
	args[0] = command

	switch command {
	case "version", "--version", "-v":
		printVersion(opts.stdout)
		return nil
	case "help", "--help", "-h":
		printUsage(opts.stdout)
		return nil
	case "init":
		return runInit(opts)
	case "doctor":
		return runDoctor(opts)
	case "uninstall":
		return runUninstall(args[1:], opts)
	case "update":
		return runUpdate(args[1:], opts)
	case "rollback":
		return runRollback(args[1:], opts)
	case "genpass":
		return runGeneratePassword(args[1:], opts)
	case "hook":
		return runHook(args[1:], opts)
	case "policy":
		return runPolicy(args[1:], opts)
	}

	cfg, _, err := loadConfig(opts.cwd)
	if err != nil {
		return err
	}
	svc := app.New(cfg)

	switch command {
	case "keygen":
		return runKeygen(svc, cfg, args[1:], opts)
	case "run":
		return runRun(svc, cfg, args[1:], opts)
	case "encrypt":
		return runEncrypt(svc, cfg, args[1:], opts)
	case "decrypt":
		return runDecrypt(svc, args[1:], opts)
	case "inspect":
		return runInspect(svc, args[1:], opts)
	case "env":
		return runEnv(svc, cfg, args[1:], opts)
	case "rotate":
		return runRotate(svc, cfg, args[1:], opts)
	case "repassword":
		return runRepassword(svc, args[1:], opts)
	case "tui":
		return runTUI(svc, cfg, opts)
	case "dlx":
		return runDlx(args[1:], opts)
	default:
		if isDirectFileInvocation(args[0], opts.cwd) {
			return runDirectPathInvocation(svc, cfg, args, opts)
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(opts runOptions) error {
	source, err := resolveConfigSource(opts.cwd)
	if err != nil {
		return err
	}
	if source.Exists {
		return fmt.Errorf("config already exists: %s", source.Path)
	}
	cfgPath := filepath.Join(opts.cwd, primaryConfig)
	svc := app.New(config.Default())
	if err := svc.Init(cfgPath); err != nil {
		return err
	}
	fmt.Fprintln(opts.stdout, "✅ Created .dpx.yaml")
	fmt.Fprintln(opts.stdout)
	fmt.Fprintln(opts.stdout, "Next steps:")
	fmt.Fprintln(opts.stdout, "  1. Run 'dpx keygen' to generate a key pair")
	fmt.Fprintln(opts.stdout, "  2. Add your public key to .dpx.yaml")
	fmt.Fprintln(opts.stdout, "  3. Run 'dpx encrypt <file>' to encrypt your secrets")
	return nil
}

func runDoctor(opts runOptions) error {
	report, err := collectDoctorReport(opts.cwd)
	if err != nil {
		return err
	}
	printDoctorReport(opts.stdout, report)
	return nil
}

type uninstallArgs struct {
	yes             bool
	removeKey       bool
	removeEncrypted bool
}

type updateArgs struct {
	version string
}

func parseUninstallArgs(args []string, opts runOptions) (uninstallArgs, error) {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	removeKey := fs.Bool("remove-key", false, "remove key file from config")
	removeEncrypted := fs.Bool("remove-encrypted", false, "remove .dpx files in current directory")
	if err := fs.Parse(args); err != nil {
		return uninstallArgs{}, err
	}
	if fs.NArg() > 0 {
		return uninstallArgs{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return uninstallArgs{
		yes:             *yes,
		removeKey:       *removeKey,
		removeEncrypted: *removeEncrypted,
	}, nil
}

func runUninstall(args []string, opts runOptions) error {
	parsed, err := parseUninstallArgs(args, opts)
	if err != nil {
		return err
	}

	source, err := resolveConfigSource(opts.cwd)
	if err != nil {
		return err
	}
	cfg := config.Default()
	if source.Exists {
		cfg, err = config.Load(source.Path)
		if err != nil {
			return err
		}
	}

	pathsToRemove := make([]string, 0, 8)
	if source.Exists {
		pathsToRemove = append(pathsToRemove, source.Path)
	}

	keyPath := expandHome(cfg.KeyFile)
	if parsed.removeKey {
		if !canRemoveKeyPath(opts.cwd, keyPath) {
			return fmt.Errorf("refusing to remove key file outside safe scope: %s", keyPath)
		}
		pathsToRemove = append(pathsToRemove, keyPath)
	}

	if parsed.removeEncrypted {
		files, err := findEncryptedFiles(opts.cwd)
		if err != nil {
			return err
		}
		pathsToRemove = append(pathsToRemove, files...)
	}

	if len(pathsToRemove) == 0 {
		fmt.Fprintln(opts.stdout, "Nothing to uninstall in current directory.")
		return nil
	}
	if err := validateRemovalTargets(pathsToRemove); err != nil {
		return err
	}

	if !parsed.yes {
		fmt.Fprintln(opts.stdout, "Uninstall plan:")
		for _, path := range pathsToRemove {
			fmt.Fprintf(opts.stdout, "  - %s\n", path)
		}
		fmt.Fprintln(opts.stdout, `Type "YES" to confirm uninstall.`)
		answer, err := prompt(opts, "Confirm: ")
		if err != nil {
			return err
		}
		if answer != "YES" {
			return fmt.Errorf("uninstall canceled")
		}
	}

	removed := make([]string, 0, len(pathsToRemove))
	for _, path := range pathsToRemove {
		ok, err := removeFileIfExists(path)
		if err != nil {
			return err
		}
		if ok {
			removed = append(removed, path)
		}
	}

	fmt.Fprintf(opts.stdout, "Uninstall completed. Removed %d file(s).\n", len(removed))
	for _, path := range removed {
		fmt.Fprintf(opts.stdout, "  - %s\n", path)
	}
	return nil
}

func parseUpdateArgs(args []string, opts runOptions) (updateArgs, error) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	version := fs.String("version", "", "target release version (for example v1.2.3)")
	if err := fs.Parse(args); err != nil {
		return updateArgs{}, err
	}
	if fs.NArg() > 0 {
		return updateArgs{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return updateArgs{version: strings.TrimSpace(*version)}, nil
}

func parseRollbackArgs(args []string, opts runOptions) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

func runUpdate(args []string, opts runOptions) error {
	parsed, err := parseUpdateArgs(args, opts)
	if err != nil {
		return err
	}
	baseURL := strings.TrimSpace(os.Getenv("DPX_UPDATE_BASE_URL"))
	progress := newUpdateProgressRenderer(opts.stdout)
	result, err := runSelfUpdate(selfupdate.UpdateOptions{
		Version: parsed.version,
		BaseURL: baseURL,
		Progress: func(event selfupdate.ProgressEvent) {
			progress.Handle(event)
		},
	})
	progress.Finish()
	if err != nil {
		return err
	}
	if result.Scheduled {
		fmt.Fprintf(opts.stdout, "Update scheduled (%s). Close and reopen your terminal, then run dpx --version.\n", result.Version)
		fmt.Fprintf(opts.stdout, "Backup: %s\n", result.BackupPath)
		return nil
	}
	fmt.Fprintf(opts.stdout, "Updated dpx (%s) at %s\n", result.Version, result.CurrentPath)
	fmt.Fprintf(opts.stdout, "Backup: %s\n", result.BackupPath)
	return nil
}

type updateProgressRenderer struct {
	out        io.Writer
	tty        bool
	activeBar  bool
	lastRender time.Time
}

func newUpdateProgressRenderer(out io.Writer) *updateProgressRenderer {
	renderer := &updateProgressRenderer{out: out}
	if file, ok := out.(*os.File); ok {
		if fd, fdOK := fileDescriptorInt(file); fdOK && term.IsTerminal(fd) {
			renderer.tty = true
		}
	}
	return renderer
}

func (r *updateProgressRenderer) Handle(event selfupdate.ProgressEvent) {
	if r == nil || r.out == nil {
		return
	}
	if event.Stage == "download" {
		r.renderDownload(event)
		return
	}
	if !event.Done && strings.TrimSpace(event.Message) != "" {
		if r.activeBar {
			fmt.Fprint(r.out, "\n")
			r.activeBar = false
		}
		if !r.tty {
			fmt.Fprintf(r.out, "%s...\n", event.Message)
		}
	}
}

func (r *updateProgressRenderer) Finish() {
	if r == nil || r.out == nil {
		return
	}
	if r.activeBar {
		fmt.Fprint(r.out, "\n")
		r.activeBar = false
	}
}

func (r *updateProgressRenderer) renderDownload(event selfupdate.ProgressEvent) {
	if r.out == nil {
		return
	}
	if !r.tty {
		if event.Done {
			fmt.Fprintf(r.out, "Downloading update... done (%s)\n", formatBinaryBytes(event.Downloaded))
		}
		return
	}
	now := time.Now()
	if !event.Done && !r.lastRender.IsZero() && now.Sub(r.lastRender) < 70*time.Millisecond {
		return
	}
	r.lastRender = now
	line := formatUpdateProgressBar(event.Downloaded, event.Total)
	fmt.Fprintf(r.out, "\r%-96s", line)
	r.activeBar = true
	if event.Done {
		fmt.Fprint(r.out, "\n")
		r.activeBar = false
	}
}

func formatUpdateProgressBar(downloaded, total int64) string {
	const width = 28
	if total <= 0 {
		return fmt.Sprintf("Downloading update %s", formatBinaryBytes(downloaded))
	}
	if downloaded < 0 {
		downloaded = 0
	}
	if downloaded > total {
		downloaded = total
	}
	percent := float64(downloaded) / float64(total)
	filled := int(percent * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	return fmt.Sprintf("Downloading update [%s] %3d%% (%s/%s)", bar, int(percent*100), formatBinaryBytes(downloaded), formatBinaryBytes(total))
}

func formatBinaryBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(suffix) {
		exp = len(suffix) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix[exp])
}

func runRollback(args []string, opts runOptions) error {
	if err := parseRollbackArgs(args, opts); err != nil {
		return err
	}
	result, err := runSelfRollback(selfupdate.RollbackOptions{})
	if err != nil {
		return err
	}
	if result.Scheduled {
		fmt.Fprintln(opts.stdout, "Rollback scheduled. Close and reopen your terminal, then run dpx --version.")
		fmt.Fprintf(opts.stdout, "Source backup: %s\n", result.BackupPath)
		return nil
	}
	fmt.Fprintf(opts.stdout, "Rollback completed. Current binary restored from %s\n", result.BackupPath)
	return nil
}

func removeFileIfExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("refusing to remove directory: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func validateRemovalTargets(paths []string) error {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("refusing to remove directory: %s", path)
		}
	}
	return nil
}

func canRemoveKeyPath(cwd, keyPath string) bool {
	cleaned := filepath.Clean(keyPath)
	defaultPath := filepath.Clean(expandHome(config.DefaultKeyFile))
	legacyPath := filepath.Clean(expandHome(config.LegacyKeyFile))
	if cleaned == defaultPath || cleaned == legacyPath {
		return true
	}
	return isPathWithin(cwd, cleaned)
}

func isPathWithin(baseDir, targetPath string) bool {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func runKeygen(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	outFlagProvided := hasLongFlag(args, "--out")
	outPath := fs.String("out", cfg.KeyFile, "path to write private key")
	regen := fs.Bool("regen", false, "regenerate key pair if key file already exists")
	importFile := fs.String("import-file", "", "import identity from an existing age key file")
	importStdin := fs.Bool("import-stdin", false, "import identity from stdin (paste key block then EOF)")
	noConfigUpdate := fs.Bool("no-config-update", false, "skip updating .dpx.yaml with generated public key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *importFile != "" && *importStdin {
		return fmt.Errorf("use only one import source: --import-file or --import-stdin")
	}
	if (*importFile != "" || *importStdin) && *regen {
		return fmt.Errorf("--regen cannot be combined with import flags")
	}

	selectedOutPath := *outPath
	if *importFile != "" && !outFlagProvided {
		selectedOutPath = *importFile
	}
	keyPath := expandHome(selectedOutPath)
	var (
		identity agex.Identity
		err      error
		status   string
	)
	if *importFile != "" || *importStdin {
		var raw []byte
		if *importFile != "" {
			raw, err = os.ReadFile(expandHome(*importFile))
			if err != nil {
				return err
			}
		} else {
			raw, err = io.ReadAll(opts.stdin)
			if err != nil {
				return err
			}
		}
		if strings.TrimSpace(string(raw)) == "" {
			return fmt.Errorf("no key data provided for import")
		}
		identity, err = svc.ImportIdentity(keyPath, string(raw))
		if err != nil {
			return err
		}
		status = "imported"
	} else if _, statErr := os.Stat(keyPath); statErr == nil {
		if *regen {
			identity, err = svc.Keygen(keyPath)
			status = "regenerated"
		} else {
			identity, err = svc.ReadIdentity(keyPath)
			status = "using existing"
		}
		if err != nil {
			if *regen {
				return err
			}
			return fmt.Errorf("read existing key file %s: %w (use --regen to replace)", keyPath, err)
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		identity, err = svc.Keygen(keyPath)
		if err != nil {
			return err
		}
		status = "generated"
	} else if statErr != nil {
		return statErr
	}

	var syncResult keygenConfigSyncResult
	if !*noConfigUpdate {
		syncResult, err = syncKeygenConfig(opts.cwd, selectedOutPath, identity.PublicKey)
		if err != nil {
			return err
		}
	}

	box := []string{
		"╔══════════════════════════════════════════════════════════════════╗",
		"║                  🔑 DPX Key Generated Successfully               ║",
		"╠══════════════════════════════════════════════════════════════════╣",
		"║ Backend: age                                                     ║",
		fmt.Sprintf("║ Key file: %-52s║", padRight(keyPath, 52)),
		fmt.Sprintf("║ Status: %-54s║", padRight(status, 54)),
		"╠══════════════════════════════════════════════════════════════════╣",
		"║ Public Key:                                                       ║",
		fmt.Sprintf("║   %-63s║", truncate(identity.PublicKey, 63)),
		"╚══════════════════════════════════════════════════════════════════╝",
	}
	for _, line := range box {
		fmt.Fprintln(opts.stdout, line)
	}
	if !*noConfigUpdate {
		fmt.Fprintf(opts.stdout, "✅ Updated config: %s\n", syncResult.Path)
		if syncResult.RecipientAdded {
			fmt.Fprintln(opts.stdout, "✅ Added public key to age.recipients")
		} else {
			fmt.Fprintln(opts.stdout, "ℹ️ Public key already exists in age.recipients")
		}
		if syncResult.LegacyMigrated {
			fmt.Fprintln(opts.stdout, "ℹ️ Legacy .dopx.yaml detected, settings written to .dpx.yaml")
		}
	}
	return nil
}

type keygenConfigSyncResult struct {
	Path           string
	RecipientAdded bool
	LegacyMigrated bool
}

func syncKeygenConfig(cwd, keyFilePath, publicKey string) (keygenConfigSyncResult, error) {
	source, err := resolveConfigSource(cwd)
	if err != nil {
		return keygenConfigSyncResult{}, err
	}

	targetPath := filepath.Join(cwd, primaryConfig)
	cfg := config.Default()
	result := keygenConfigSyncResult{Path: targetPath}

	if source.Exists {
		loaded, err := config.Load(source.Path)
		if err != nil {
			return keygenConfigSyncResult{}, err
		}
		cfg = loaded
		if !source.Legacy {
			targetPath = source.Path
			result.Path = targetPath
		} else {
			result.LegacyMigrated = true
		}
	}

	cfg.KeyFile = keyFilePath
	if !containsString(cfg.Age.Recipients, publicKey) {
		cfg.Age.Recipients = append(cfg.Age.Recipients, publicKey)
		result.RecipientAdded = true
	}

	if err := config.Save(targetPath, cfg); err != nil {
		return keygenConfigSyncResult{}, err
	}
	return result, nil
}

func runEncrypt(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEncryptArgs(args)
	if err != nil {
		return err
	}

	filePath := parsed.filePath
	if filePath == "" {
		selectedPath, err := chooseEncryptPath(opts, svc, opts.cwd)
		if err != nil {
			return err
		}
		filePath = selectedPath
	}

	recipients := splitCSV(parsed.recipientsText)
	mode := chooseMode(parsed.passwordText, parsed.useAge, recipients, cfg, opts.forcePassword)
	req := app.EncryptRequest{
		InputPath:  filePath,
		OutputPath: parsed.outPath,
		Mode:       mode,
		Recipients: recipients,
		KDFProfile: parsed.kdfProfile,
	}
	switch mode {
	case envelope.ModePassword:
		req.Passphrase = []byte(parsed.passwordText)
		if len(req.Passphrase) == 0 {
			pass, err := promptSecretWithConfirmation(opts, "Password: ", "Confirm password: ")
			if err != nil {
				return err
			}
			req.Passphrase = []byte(pass)
		}
	case envelope.ModeAge:
		if len(req.Recipients) == 0 {
			req.Recipients = cfg.Age.Recipients
		}
	}

	outputPath, err := svc.EncryptFile(req)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Encrypted %s -> %s\n", req.InputPath, outputPath)
	return nil
}

func runDecrypt(svc app.Service, args []string, opts runOptions) error {
	parsed, err := parseDecryptArgs(args)
	if err != nil {
		return err
	}

	filePath := parsed.filePath
	if filePath == "" {
		files, err := findEncryptedFiles(opts.cwd)
		if err != nil {
			return err
		}
		choice, err := chooseString(opts, "Select a file to decrypt", files)
		if err != nil {
			return err
		}
		filePath = choice
	}

	meta, err := svc.Inspect(filePath)
	if err != nil {
		hasAge, hasPassword, detectErr := svc.DetectEnvInlineModes(filePath)
		if detectErr == nil && (hasAge || hasPassword) {
			inlineReq := app.EnvInlineDecryptRequest{
				InputPath:    filePath,
				OutputPath:   parsed.outPath,
				IdentityPath: parsed.identityPath,
			}
			if hasPassword {
				inlineReq.Passphrase = []byte(parsed.passwordText)
				if len(inlineReq.Passphrase) == 0 {
					pass, promptErr := promptSecret(opts, "Password: ")
					if promptErr != nil {
						return promptErr
					}
					inlineReq.Passphrase = []byte(pass)
				}
			}
			result, decErr := svc.DecryptEnvInlineFile(inlineReq)
			if decErr != nil {
				return decErr
			}
			fmt.Fprintf(opts.stdout, "Env inline decrypted %s -> %s\n", inlineReq.InputPath, result.OutputPath)
			fmt.Fprintf(opts.stdout, "Updated keys (%d): %s\n", len(result.Updated), strings.Join(result.Updated, ", "))
			return nil
		}
		if detectErr == nil && !hasAge && !hasPassword {
			return fmt.Errorf("%w (not a DPX envelope and no inline ENC tokens found)", err)
		}
		return err
	}
	req := app.DecryptRequest{InputPath: filePath, OutputPath: parsed.outPath, IdentityPath: parsed.identityPath}
	if meta.Mode == envelope.ModePassword {
		req.Passphrase = []byte(parsed.passwordText)
		if len(req.Passphrase) == 0 {
			pass, err := promptSecret(opts, "Password: ")
			if err != nil {
				return err
			}
			req.Passphrase = []byte(pass)
		}
	}
	outputPath, err := svc.DecryptFile(req)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Decrypted %s -> %s\n", filePath, outputPath)
	return nil
}

func runInspect(svc app.Service, args []string, opts runOptions) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(opts.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("inspect requires a .dpx file")
	}
	meta, err := svc.Inspect(fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Version: %d\n", meta.Version)
	fmt.Fprintf(opts.stdout, "Mode: %s\n", meta.Mode)
	fmt.Fprintf(opts.stdout, "Original Name: %s\n", meta.OriginalName)
	fmt.Fprintf(opts.stdout, "Created At: %s\n", meta.CreatedAt.Format("2006-01-02 15:04:05Z07:00"))
	if meta.EncryptionAlgorithm != "" {
		fmt.Fprintf(opts.stdout, "Encryption: %s\n", meta.EncryptionAlgorithm)
	}
	if meta.KDF != nil {
		fmt.Fprintf(opts.stdout, "KDF: %s\n", meta.KDF.Algorithm)
	}
	return nil
}

type envEncryptArgs struct {
	filePath       string
	mode           string
	outPath        string
	keysText       string
	recipientsText string
	passwordText   string
	kdfProfile     string
}

type envDecryptArgs struct {
	filePath     string
	outPath      string
	passwordText string
	identityPath string
}

type envSetArgs struct {
	filePath       string
	outPath        string
	key            string
	value          string
	hasValue       bool
	mode           string
	encrypt        bool
	passwordText   string
	recipientsText string
	kdfProfile     string
}

type envUpdateKeysArgs struct {
	filePath       string
	outPath        string
	identityPath   string
	recipientsText string
	keysText       string
}

func runEnv(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	if len(args) == 0 {
		return fmt.Errorf("env requires subcommand: encrypt, decrypt, list, get, set, or updatekeys")
	}
	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcommand {
	case "encrypt", "enc":
		return runEnvEncrypt(svc, cfg, args[1:], opts)
	case "decrypt", "dec":
		return runEnvDecrypt(svc, cfg, args[1:], opts)
	case "list", "ls":
		return runEnvList(svc, cfg, args[1:], opts)
	case "get":
		return runEnvGet(svc, cfg, args[1:], opts)
	case "set":
		return runEnvSet(svc, cfg, args[1:], opts)
	case "updatekeys":
		return runEnvUpdateKeys(svc, cfg, args[1:], opts)
	default:
		return fmt.Errorf("unknown env subcommand %q", subcommand)
	}
}

func runEnvEncrypt(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvEncryptArgs(args)
	if err != nil {
		return err
	}

	filePath := strings.TrimSpace(parsed.filePath)
	if filePath == "" {
		filePath, err = chooseEnvInlineSource(opts, svc, opts.cwd)
		if err != nil {
			return err
		}
	}
	rule, hasRule := matchCreationRule(cfg, filePath)

	mode := strings.TrimSpace(strings.ToLower(parsed.mode))
	if mode == "" && hasRule {
		ruleMode := strings.TrimSpace(strings.ToLower(rule.Mode))
		if ruleMode == envelope.ModeAge || ruleMode == envelope.ModePassword {
			mode = ruleMode
		}
	}
	if mode == "" {
		mode, err = chooseString(opts, "Choose env encryption mode", []string{"age", "password"})
		if err != nil {
			return err
		}
	}
	if mode != envelope.ModeAge && mode != envelope.ModePassword {
		return fmt.Errorf("unsupported env mode %q", mode)
	}

	keys := splitCSV(parsed.keysText)
	if len(keys) == 0 && hasRule && len(rule.EncryptKeys) > 0 {
		keys = append([]string{}, rule.EncryptKeys...)
	}
	if len(keys) == 0 {
		availableKeys, err := svc.ListEnvInlineKeys(filePath)
		if err != nil {
			return err
		}
		keys, err = chooseEnvKeys(opts, availableKeys)
		if err != nil {
			return err
		}
	}

	req := app.EnvInlineEncryptRequest{
		InputPath:    filePath,
		OutputPath:   parsed.outPath,
		Mode:         mode,
		SelectedKeys: keys,
		KDFProfile:   parsed.kdfProfile,
	}
	switch mode {
	case envelope.ModeAge:
		req.Recipients = splitCSV(parsed.recipientsText)
		if len(req.Recipients) == 0 {
			req.Recipients = append([]string{}, cfg.Age.Recipients...)
		}
	case envelope.ModePassword:
		req.Passphrase = []byte(parsed.passwordText)
		if len(req.Passphrase) == 0 {
			pass, err := promptSecretWithConfirmation(opts, "Password: ", "Confirm password: ")
			if err != nil {
				return err
			}
			req.Passphrase = []byte(pass)
		}
	}

	result, err := svc.EncryptEnvInlineFile(req)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Env inline encrypted %s -> %s\n", req.InputPath, result.OutputPath)
	fmt.Fprintf(opts.stdout, "Updated keys (%d): %s\n", len(result.Updated), strings.Join(result.Updated, ", "))
	return nil
}

func runEnvDecrypt(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvDecryptArgs(args)
	if err != nil {
		return err
	}
	filePath := strings.TrimSpace(parsed.filePath)
	if filePath == "" {
		files, err := findEncryptedFiles(opts.cwd)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			filePath, err = prompt(opts, "Env .dpx file path: ")
			if err != nil {
				return err
			}
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				return fmt.Errorf("file path is required")
			}
		} else {
			filePath, err = chooseString(opts, "Select a .env.dpx file to decrypt", files)
			if err != nil {
				return err
			}
		}
	}

	hasAge, hasPassword, err := svc.DetectEnvInlineModes(filePath)
	if err != nil {
		return err
	}

	req := app.EnvInlineDecryptRequest{
		InputPath:    filePath,
		OutputPath:   parsed.outPath,
		IdentityPath: parsed.identityPath,
	}
	if hasPassword {
		req.Passphrase = []byte(parsed.passwordText)
		if len(req.Passphrase) == 0 {
			pass, err := promptSecret(opts, "Password: ")
			if err != nil {
				return err
			}
			req.Passphrase = []byte(pass)
		}
	}
	if hasAge && req.IdentityPath == "" {
		req.IdentityPath = cfg.KeyFile
	}

	result, err := svc.DecryptEnvInlineFile(req)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Env inline decrypted %s -> %s\n", req.InputPath, result.OutputPath)
	fmt.Fprintf(opts.stdout, "Updated keys (%d): %s\n", len(result.Updated), strings.Join(result.Updated, ", "))
	return nil
}

func runEnvSet(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvSetArgs(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.key) == "" {
		return fmt.Errorf("env set requires --key <name>")
	}
	if parsed.filePath == "" {
		parsed.filePath, err = defaultRunSourcePath(opts.cwd, cfg.DefaultSuffix)
		if err != nil {
			return err
		}
	}

	value := parsed.value
	if !parsed.hasValue {
		value, err = prompt(opts, "Value: ")
		if err != nil {
			return err
		}
	}

	rule, hasRule := matchCreationRule(cfg, parsed.filePath)
	mode := strings.TrimSpace(strings.ToLower(parsed.mode))
	encrypt := parsed.encrypt
	if !encrypt && (mode != "" || parsed.passwordText != "" || parsed.recipientsText != "") {
		encrypt = true
	}
	if !encrypt && hasRule && containsString(rule.EncryptKeys, strings.TrimSpace(parsed.key)) {
		encrypt = true
		if mode == "" {
			mode = strings.TrimSpace(strings.ToLower(rule.Mode))
		}
	}
	if encrypt && mode == "" && hasRule {
		ruleMode := strings.TrimSpace(strings.ToLower(rule.Mode))
		if ruleMode == envelope.ModeAge || ruleMode == envelope.ModePassword {
			mode = ruleMode
		}
	}
	if encrypt && mode == "" {
		mode = chooseMode(parsed.passwordText, false, splitCSV(parsed.recipientsText), cfg, false)
	}
	if encrypt && mode != envelope.ModeAge && mode != envelope.ModePassword {
		return fmt.Errorf("unsupported env mode %q", mode)
	}

	req := app.EnvInlineSetRequest{
		InputPath:  parsed.filePath,
		OutputPath: parsed.outPath,
		Key:        strings.TrimSpace(parsed.key),
		Value:      value,
		Encrypt:    encrypt,
		Mode:       mode,
		KDFProfile: parsed.kdfProfile,
	}
	if encrypt {
		switch mode {
		case envelope.ModeAge:
			req.Recipients = splitCSV(parsed.recipientsText)
			if len(req.Recipients) == 0 {
				req.Recipients = append([]string{}, cfg.Age.Recipients...)
			}
		case envelope.ModePassword:
			req.Passphrase = []byte(parsed.passwordText)
			if len(req.Passphrase) == 0 {
				pass, err := promptSecretWithConfirmation(opts, "Password: ", "Confirm password: ")
				if err != nil {
					return err
				}
				req.Passphrase = []byte(pass)
			}
		}
	}

	result, err := svc.SetEnvInlineValue(req)
	if err != nil {
		return err
	}
	if req.Encrypt {
		fmt.Fprintf(opts.stdout, "Env key %s updated and encrypted (%s) -> %s\n", req.Key, req.Mode, result.OutputPath)
		return nil
	}
	fmt.Fprintf(opts.stdout, "Env key %s updated -> %s\n", req.Key, result.OutputPath)
	return nil
}

func runEnvUpdateKeys(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvUpdateKeysArgs(args)
	if err != nil {
		return err
	}
	if parsed.filePath == "" {
		parsed.filePath, err = defaultInlineSourcePath(opts.cwd, cfg.DefaultSuffix)
		if err != nil {
			return err
		}
	}
	recipients := splitCSV(parsed.recipientsText)
	if len(recipients) == 0 {
		return fmt.Errorf("env updatekeys requires --recipient <csv>")
	}
	req := app.EnvInlineUpdateRecipientsRequest{
		InputPath:    parsed.filePath,
		OutputPath:   parsed.outPath,
		IdentityPath: parsed.identityPath,
		Recipients:   recipients,
		SelectedKeys: splitCSV(parsed.keysText),
	}
	result, err := svc.UpdateEnvInlineRecipients(req)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.stdout, "Env inline recipients updated %s -> %s\n", req.InputPath, result.OutputPath)
	fmt.Fprintf(opts.stdout, "Updated keys (%d): %s\n", len(result.Updated), strings.Join(result.Updated, ", "))
	return nil
}

type envReadArgs struct {
	filePath     string
	passwordText string
	identityPath string
	key          string
}

func runEnvList(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvReadArgs(args)
	if err != nil {
		return err
	}
	if parsed.filePath == "" {
		parsed.filePath, err = defaultRunSourcePath(opts.cwd, cfg.DefaultSuffix)
		if err != nil {
			return err
		}
	}
	values, err := loadRuntimeEnvValues(svc, runArgs{
		filePath:     parsed.filePath,
		passwordText: parsed.passwordText,
		identityPath: parsed.identityPath,
	}, opts)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintln(opts.stdout, key)
	}
	return nil
}

func runEnvGet(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseEnvReadArgs(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.key) == "" {
		return fmt.Errorf("env get requires --key <name>")
	}
	if parsed.filePath == "" {
		parsed.filePath, err = defaultRunSourcePath(opts.cwd, cfg.DefaultSuffix)
		if err != nil {
			return err
		}
	}
	values, err := loadRuntimeEnvValues(svc, runArgs{
		filePath:     parsed.filePath,
		passwordText: parsed.passwordText,
		identityPath: parsed.identityPath,
	}, opts)
	if err != nil {
		return err
	}
	value, ok := values[parsed.key]
	if !ok {
		return fmt.Errorf("key %q not found", parsed.key)
	}
	fmt.Fprintln(opts.stdout, value)
	return nil
}

func parseEnvReadArgs(args []string) (envReadArgs, error) {
	var parsed envReadArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--identity":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --identity")
			}
			parsed.identityPath = args[i]
		case "--key":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --key")
			}
			parsed.key = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath != "" {
				return parsed, fmt.Errorf("unexpected argument %q", arg)
			}
			parsed.filePath = arg
		}
	}
	return parsed, nil
}

func parseEnvSetArgs(args []string) (envSetArgs, error) {
	var parsed envSetArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--key":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --key")
			}
			parsed.key = args[i]
		case "--value":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --value")
			}
			parsed.value = args[i]
			parsed.hasValue = true
		case "--encrypt":
			parsed.encrypt = true
		case "--mode":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --mode")
			}
			parsed.mode = strings.ToLower(strings.TrimSpace(args[i]))
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--kdf-profile":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --kdf-profile")
			}
			parsed.kdfProfile = args[i]
		case "--recipient":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --recipient")
			}
			parsed.recipientsText = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath != "" {
				return parsed, fmt.Errorf("unexpected argument %q", arg)
			}
			parsed.filePath = arg
		}
	}
	return parsed, nil
}

func parseEnvUpdateKeysArgs(args []string) (envUpdateKeysArgs, error) {
	var parsed envUpdateKeysArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--identity":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --identity")
			}
			parsed.identityPath = args[i]
		case "--recipient":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --recipient")
			}
			parsed.recipientsText = args[i]
		case "--keys":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --keys")
			}
			parsed.keysText = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath != "" {
				return parsed, fmt.Errorf("unexpected argument %q", arg)
			}
			parsed.filePath = arg
		}
	}
	return parsed, nil
}

type runArgs struct {
	filePath     string
	passwordText string
	identityPath string
	command      []string
}

func runRun(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	parsed, err := parseRunArgs(args)
	if err != nil {
		return err
	}
	if parsed.filePath == "" {
		parsed.filePath, err = defaultRunSourcePath(opts.cwd, cfg.DefaultSuffix)
		if err != nil {
			return err
		}
	}

	envValues, err := loadRuntimeEnvValues(svc, parsed, opts)
	if err != nil {
		return err
	}
	if len(envValues) == 0 {
		return fmt.Errorf("no environment variables loaded from %s", parsed.filePath)
	}

	cmd := exec.Command(parsed.command[0], parsed.command[1:]...)
	cmd.Stdin = opts.stdin
	cmd.Stdout = opts.stdout
	cmd.Stderr = opts.stderr
	cmd.Env = mergeCommandEnv(os.Environ(), envValues)
	return cmd.Run()
}

func parseRunArgs(args []string) (runArgs, error) {
	var parsed runArgs
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		return parsed, fmt.Errorf(`run requires command separator "--", for example: dpx run .env.dpx -- app`)
	}
	parsed.command = append([]string{}, args[separator+1:]...)
	if len(parsed.command) == 0 {
		return parsed, fmt.Errorf("run requires a command after --")
	}
	left := args[:separator]
	for i := 0; i < len(left); i++ {
		arg := left[i]
		switch arg {
		case "--file", "-f":
			i++
			if i >= len(left) {
				return parsed, fmt.Errorf("missing value for %s", arg)
			}
			parsed.filePath = left[i]
		case "--password":
			i++
			if i >= len(left) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = left[i]
		case "--identity":
			i++
			if i >= len(left) {
				return parsed, fmt.Errorf("missing value for --identity")
			}
			parsed.identityPath = left[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath != "" {
				return parsed, fmt.Errorf("unexpected argument %q", arg)
			}
			parsed.filePath = arg
		}
	}
	return parsed, nil
}

func defaultRunSourcePath(cwd, defaultSuffix string) (string, error) {
	candidates := []string{
		filepath.Join(cwd, ".env"),
		filepath.Join(cwd, ".env.dpx"),
	}
	if defaultSuffix != "" {
		candidates = append(candidates, filepath.Join(cwd, ".env"+defaultSuffix))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("run could not find default env source (.env or .env.dpx); pass --file")
}

func defaultInlineSourcePath(cwd, defaultSuffix string) (string, error) {
	candidates := []string{
		filepath.Join(cwd, ".env.dpx"),
	}
	if defaultSuffix != "" {
		withSuffix := filepath.Join(cwd, ".env"+defaultSuffix)
		if !containsString(candidates, withSuffix) {
			candidates = append(candidates, withSuffix)
		}
	}
	candidates = append(candidates, filepath.Join(cwd, ".env"))
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("env updatekeys could not find default source (.env.dpx or .env); pass file path")
}

func matchCreationRule(cfg config.Config, filePath string) (config.CreationRule, bool) {
	cleanedPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(filePath)))
	base := filepath.Base(cleanedPath)
	absPath := ""
	if resolvedAbs, err := filepath.Abs(filePath); err == nil {
		absPath = filepath.ToSlash(filepath.Clean(resolvedAbs))
	}

	for _, rule := range cfg.Policy.CreationRules {
		pattern := strings.TrimSpace(rule.Path)
		if pattern == "" {
			continue
		}
		pattern = filepath.ToSlash(pattern)
		if ok, _ := filepath.Match(pattern, cleanedPath); ok {
			return rule, true
		}
		if ok, _ := filepath.Match(pattern, base); ok {
			return rule, true
		}
		if absPath != "" {
			if ok, _ := filepath.Match(pattern, absPath); ok {
				return rule, true
			}
		}
	}
	return config.CreationRule{}, false
}

func loadRuntimeEnvValues(svc app.Service, parsed runArgs, opts runOptions) (map[string]string, error) {
	data, err := safeio.ReadFile(parsed.filePath)
	if err != nil {
		return nil, err
	}

	if meta, _, err := envelope.Unmarshal(data); err == nil {
		request := app.DecryptRequest{
			InputPath:    parsed.filePath,
			IdentityPath: parsed.identityPath,
			Passphrase:   []byte(parsed.passwordText),
		}
		if meta.Mode == envelope.ModePassword && len(request.Passphrase) == 0 {
			pass, err := promptSecret(opts, "Password: ")
			if err != nil {
				return nil, err
			}
			request.Passphrase = []byte(pass)
		}
		plaintext, err := decryptToTempAndReadEnvelope(svc, request)
		if err != nil {
			return nil, err
		}
		return parseRuntimeEnvMap(plaintext), nil
	}

	hasAge, hasPassword, err := svc.DetectEnvInlineModes(parsed.filePath)
	if err == nil && (hasAge || hasPassword) {
		request := app.EnvInlineDecryptRequest{
			InputPath:    parsed.filePath,
			IdentityPath: parsed.identityPath,
			Passphrase:   []byte(parsed.passwordText),
		}
		if hasPassword && len(request.Passphrase) == 0 {
			pass, err := promptSecret(opts, "Password: ")
			if err != nil {
				return nil, err
			}
			request.Passphrase = []byte(pass)
		}
		plaintext, err := decryptToTempAndReadInline(svc, request)
		if err != nil {
			return nil, err
		}
		return parseRuntimeEnvMap(plaintext), nil
	}

	return parseRuntimeEnvMap(data), nil
}

func decryptToTempAndReadEnvelope(svc app.Service, req app.DecryptRequest) ([]byte, error) {
	tempPath, cleanup, err := createTempEnvPath()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	req.OutputPath = tempPath
	if _, err := svc.DecryptFile(req); err != nil {
		return nil, err
	}
	return safeio.ReadFile(tempPath)
}

func decryptToTempAndReadInline(svc app.Service, req app.EnvInlineDecryptRequest) ([]byte, error) {
	tempPath, cleanup, err := createTempEnvPath()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	req.OutputPath = tempPath
	if _, err := svc.DecryptEnvInlineFile(req); err != nil {
		return nil, err
	}
	return safeio.ReadFile(tempPath)
}

func createTempEnvPath() (string, func(), error) {
	file, err := os.CreateTemp("", "dpx-run-*.env")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(path) }
	return path, cleanup, nil
}

func parseRuntimeEnvMap(data []byte) map[string]string {
	lines := strings.Split(string(data), "\n")
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		key, value, ok := parseRuntimeEnvLine(line)
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func parseRuntimeEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	separator := strings.Index(line, "=")
	if separator <= 0 {
		return "", "", false
	}
	left := strings.TrimSpace(line[:separator])
	if strings.HasPrefix(left, "export ") {
		left = strings.TrimSpace(strings.TrimPrefix(left, "export "))
	}
	if left == "" {
		return "", "", false
	}
	value := strings.TrimSpace(line[separator+1:])
	return left, trimRuntimeValue(value), true
}

func trimRuntimeValue(value string) string {
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err == nil {
				return unquoted
			}
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func mergeCommandEnv(base []string, overrides map[string]string) []string {
	envMap := make(map[string]string, len(base)+len(overrides))
	for _, pair := range base {
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		envMap[parts[0]] = parts[1]
	}
	for key, value := range overrides {
		envMap[key] = value
	}
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	merged := make([]string, 0, len(keys))
	for _, key := range keys {
		merged = append(merged, key+"="+envMap[key])
	}
	return merged
}

func runPolicy(args []string, opts runOptions) error {
	if len(args) == 0 {
		return fmt.Errorf("policy requires subcommand: check")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "check":
		return runPolicyCheck(args[1:], opts)
	default:
		return fmt.Errorf("unknown policy subcommand %q", args[0])
	}
}

func runPolicyCheck(args []string, opts runOptions) error {
	filePath := ".env"
	if len(args) > 1 {
		return fmt.Errorf("policy check expects at most one file path")
	}
	if len(args) == 1 {
		filePath = strings.TrimSpace(args[0])
		if filePath == "" {
			return fmt.Errorf("file path is required")
		}
	}

	data, err := safeio.ReadFile(filePath)
	if err != nil {
		return err
	}
	report := policy.Check(filePath, data)
	if report.SkipReason != "" {
		fmt.Fprintf(opts.stdout, "Policy OK (%s)\n", report.SkipReason)
		return nil
	}
	if len(report.Findings) == 0 {
		fmt.Fprintf(opts.stdout, "Policy OK: no plaintext sensitive keys found (%s)\n", report.Format)
		return nil
	}
	fmt.Fprintf(opts.stdout, "Policy findings: %d (%s)\n", len(report.Findings), report.Format)
	for _, finding := range report.Findings {
		if finding.Line > 0 {
			fmt.Fprintf(opts.stdout, "  - line %d key %s: %s\n", finding.Line, finding.Key, finding.Reason)
		} else {
			fmt.Fprintf(opts.stdout, "  - key %s: %s\n", finding.Key, finding.Reason)
		}
	}
	return fmt.Errorf("policy check failed with %d finding(s)", len(report.Findings))
}

func chooseEnvInlineSource(opts runOptions, svc app.Service, cwd string) (string, error) {
	candidates, err := svc.Discover(cwd)
	if err != nil {
		return "", err
	}
	options := candidatePaths(candidates)
	options = append(options, manualEncryptPathOption)
	choice, err := chooseString(opts, "Select a .env file for inline encryption", options)
	if err != nil {
		return "", err
	}
	if choice == manualEncryptPathOption {
		path, err := prompt(opts, "Env file path: ")
		if err != nil {
			return "", err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return "", fmt.Errorf("file path is required")
		}
		return path, nil
	}
	return choice, nil
}

func chooseEnvKeys(opts runOptions, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("no encryptable env keys found")
	}
	fmt.Fprintln(opts.stdout, "Select keys to encrypt:")
	for i, key := range keys {
		fmt.Fprintf(opts.stdout, "  %d. %s\n", i+1, key)
	}
	fmt.Fprintln(opts.stdout, `Enter comma-separated indexes or "all".`)
	input, err := prompt(opts, "Keys: ")
	if err != nil {
		return nil, err
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || input == "all" {
		return append([]string{}, keys...), nil
	}
	parts := strings.Split(input, ",")
	selected := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		index, err := strconv.Atoi(part)
		if err != nil || index < 1 || index > len(keys) {
			return nil, fmt.Errorf("invalid key index %q", part)
		}
		key := keys[index-1]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, key)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no keys selected")
	}
	return selected, nil
}

func runTUI(svc app.Service, cfg config.Config, opts runOptions) error {
	var err error
	if !shouldUseBubbleTUI(opts) {
		err = tui.RunFallback(svc, cfg, opts.cwd, opts.stdin, opts.stdout)
	} else {
		err = tui.Run(svc, cfg, opts.cwd, opts.stdin, opts.stdout)
	}
	if err == tui.ErrActionRotate {
		return runRotate(svc, cfg, []string{}, opts)
	}
	if err == tui.ErrActionRepasswordManual {
		return runRepassword(svc, []string{}, opts)
	}
	if err == tui.ErrActionRepasswordGenerate {
		return runRepassword(svc, []string{"--generate-password"}, opts)
	}
	if err == tui.ErrActionGeneratePassword {
		return runGeneratePassword([]string{}, opts)
	}
	if err == tui.ErrActionHookInstall {
		return runHook([]string{"install"}, opts)
	}
	if err == tui.ErrActionHookUninstall {
		return runHook([]string{"uninstall"}, opts)
	}
	return err
}

func shouldUseBubbleTUI(opts runOptions) bool {
	return shouldUseBubbleTUIForOS(opts, runtimeGOOS)
}

func shouldUseBubbleTUIForOS(opts runOptions, goos string) bool {
	_ = goos
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("DPX_TUI_MODE")))
	if mode == "fallback" || mode == "plain" {
		return false
	}
	inFile, inTTY := opts.stdin.(*os.File)
	outFile, outTTY := opts.stdout.(*os.File)
	if inTTY && outTTY {
		inFD, inOK := fileDescriptorInt(inFile)
		outFD, outOK := fileDescriptorInt(outFile)
		if inOK && outOK && term.IsTerminal(inFD) && term.IsTerminal(outFD) {
			return true
		}
	}
	return false
}

func runDirectPathInvocation(svc app.Service, cfg config.Config, args []string, opts runOptions) error {
	if len(args) == 0 {
		return fmt.Errorf("missing file path")
	}
	if isEncryptedPath(args[0], cfg.DefaultSuffix) {
		return runDecrypt(svc, args, opts)
	}
	if isEnvFile(args[0]) {
		opts.forcePassword = true
	}
	return runEncrypt(svc, cfg, args, opts)
}

func isDirectFileInvocation(arg, cwd string) bool {
	if strings.TrimSpace(arg) == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	if strings.Contains(arg, "/") || strings.Contains(arg, "\\") || strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~") {
		return true
	}
	if fileExists(arg) {
		return true
	}
	if cwd != "" && !filepath.IsAbs(arg) && fileExists(filepath.Join(cwd, arg)) {
		return true
	}
	return false
}

func isEncryptedPath(path, defaultSuffix string) bool {
	if strings.HasSuffix(path, ".dpx") {
		return true
	}
	if defaultSuffix != "" && defaultSuffix != ".dpx" && strings.HasSuffix(path, defaultSuffix) {
		return true
	}
	return false
}

func isEnvFile(path string) bool {
	name := filepath.Base(path)
	return name == ".env" || strings.HasPrefix(name, ".env.")
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := safeio.Stat(path)
	return err == nil
}

func chooseMode(passwordText string, useAge bool, recipients []string, cfg config.Config, forcePassword bool) string {
	if passwordText != "" {
		return envelope.ModePassword
	}
	if useAge || len(recipients) > 0 {
		return envelope.ModeAge
	}
	if forcePassword {
		return envelope.ModePassword
	}
	if len(cfg.Age.Recipients) > 0 {
		return envelope.ModeAge
	}
	return envelope.ModePassword
}

const manualEncryptPathOption = "[manual] Enter custom file path"
const searchEncryptPathOption = "[search] Find file by keyword"

type encryptScope string

const (
	encryptScopeAny encryptScope = "any"
	encryptScopeEnv encryptScope = "env"
)

func chooseEncryptPath(opts runOptions, svc app.Service, cwd string) (string, error) {
	scope := encryptScopeAny
	for {
		candidates, err := discoverEncryptCandidatesByScope(svc, cwd, scope)
		if err != nil {
			return "", err
		}
		choice, err := chooseCandidatePathWithScope(opts, encryptScopeTitle(scope), candidates, scope)
		if err != nil {
			return "", err
		}
		switch choice {
		case encryptScopeSwitchOption(scope):
			scope = toggleEncryptScope(scope)
			continue
		case searchEncryptPathOption:
			if len(candidates) == 0 {
				fmt.Fprintln(opts.stdout, "No files available to search in this mode.")
				continue
			}
			query, err := prompt(opts, "Search keyword: ")
			if err != nil {
				return "", err
			}
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}
			filtered := filterCandidatesByQuery(candidates, query)
			if len(filtered) == 0 {
				fmt.Fprintf(opts.stdout, "No files match %q in %s mode.\n", query, encryptScopeName(scope))
				continue
			}
			if len(filtered) == 1 {
				return filtered[0].Path, nil
			}
			picked, err := chooseString(opts, fmt.Sprintf("Search results for %q", query), append(candidatePaths(filtered), "[back] Back"))
			if err != nil {
				return "", err
			}
			if picked == "[back] Back" {
				continue
			}
			return picked, nil
		case manualEncryptPathOption:
			path, err := prompt(opts, "File to encrypt: ")
			if err != nil {
				return "", err
			}
			path = strings.TrimSpace(path)
			if path == "" {
				return "", fmt.Errorf("file path is required")
			}
			return path, nil
		default:
			return choice, nil
		}
	}
}

func chooseCandidatePathWithScope(opts runOptions, title string, candidates []discovery.Candidate, scope encryptScope) (string, error) {
	labels := candidatePaths(candidates)
	labels = append(labels, manualEncryptPathOption)
	if len(candidates) > 0 {
		labels = append(labels, searchEncryptPathOption)
	}
	labels = append(labels, encryptScopeSwitchOption(scope))
	return chooseString(opts, title, labels)
}

func discoverEncryptCandidatesByScope(svc app.Service, cwd string, scope encryptScope) ([]discovery.Candidate, error) {
	switch scope {
	case encryptScopeEnv:
		return svc.Discover(cwd)
	default:
		return svc.DiscoverEncryptTargets(cwd)
	}
}

func candidatePaths(candidates []discovery.Candidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		labels = append(labels, candidate.Path)
	}
	return labels
}

func filterCandidatesByQuery(candidates []discovery.Candidate, query string) []discovery.Candidate {
	lower := strings.ToLower(query)
	filtered := make([]discovery.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		base := strings.ToLower(filepath.Base(candidate.Path))
		path := strings.ToLower(candidate.Path)
		if strings.Contains(base, lower) || strings.Contains(path, lower) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func toggleEncryptScope(scope encryptScope) encryptScope {
	if scope == encryptScopeEnv {
		return encryptScopeAny
	}
	return encryptScopeEnv
}

func encryptScopeSwitchOption(scope encryptScope) string {
	if scope == encryptScopeEnv {
		return "[scope] Switch to all files mode"
	}
	return "[scope] Switch to .env mode"
}

func encryptScopeTitle(scope encryptScope) string {
	if scope == encryptScopeEnv {
		return "Select a file to encrypt (.env mode)"
	}
	return "Select a file to encrypt (all files mode)"
}

func encryptScopeName(scope encryptScope) string {
	if scope == encryptScopeEnv {
		return ".env"
	}
	return "all files"
}

func chooseString(opts runOptions, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options available")
	}
	fmt.Fprintln(opts.stdout, title)
	for idx, option := range options {
		fmt.Fprintf(opts.stdout, "  %d. %s\n", idx+1, option)
	}
	response, err := prompt(opts, "Select option: ")
	if err != nil {
		return "", err
	}
	if response == "" {
		return options[0], nil
	}
	idx := 0
	if _, err := fmt.Sscanf(response, "%d", &idx); err != nil || idx < 1 || idx > len(options) {
		return "", fmt.Errorf("invalid selection %q", response)
	}
	return options[idx-1], nil
}

func prompt(opts runOptions, label string) (string, error) {
	fmt.Fprint(opts.stdout, label)
	text, err := getReader(opts).ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return strings.TrimSpace(text), nil
		}
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func promptSecret(opts runOptions, label string) (string, error) {
	if file, ok := opts.stdin.(*os.File); ok {
		if fd, fdOK := fileDescriptorInt(file); fdOK && term.IsTerminal(fd) {
			fmt.Fprint(opts.stdout, label)
			secret, err := term.ReadPassword(fd)
			fmt.Fprintln(opts.stdout)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(secret)), nil
		}
	}
	return prompt(opts, label)
}

func promptSecretWithConfirmation(opts runOptions, label, confirmLabel string) (string, error) {
	for {
		pass, err := promptSecret(opts, label)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(pass) == "" {
			return "", fmt.Errorf("password is required")
		}
		confirm, err := promptSecret(opts, confirmLabel)
		if err != nil {
			return "", err
		}
		if pass == confirm {
			return pass, nil
		}
		fmt.Fprintln(opts.stdout, "Password confirmation does not match. Try again.")
	}
}

func getReader(opts runOptions) *bufio.Reader {
	if opts.reader != nil {
		return opts.reader
	}
	if opts.stdin == nil {
		return bufio.NewReader(strings.NewReader(""))
	}
	return bufio.NewReader(opts.stdin)
}

func loadConfig(cwd string) (config.Config, configSource, error) {
	source, err := resolveConfigSource(cwd)
	if err != nil {
		return config.Config{}, configSource{}, err
	}
	if source.Exists {
		cfg, err := config.Load(source.Path)
		return cfg, source, err
	}
	return config.Default(), source, nil
}

var commandAliases = map[string]string{
	"init":              "init",
	"doctor":            "doctor",
	"uninstall":         "uninstall",
	"update":            "update",
	"rollback":          "rollback",
	"genpass":           "genpass",
	"passgen":           "genpass",
	"generate-password": "genpass",
	"hook":              "hook",
	"policy":            "policy",
	"run":               "run",
	"env":               "env",
	"keygen":            "keygen",
	"encrypt":           "encrypt",
	"enc":               "encrypt",
	"e":                 "encrypt",
	"decrypt":           "decrypt",
	"dec":               "decrypt",
	"d":                 "decrypt",
	"inspect":           "inspect",
	"rotate":            "rotate",
	"rekey":             "rotate",
	"repassword":        "repassword",
	"passwd":            "repassword",
	"tui":               "tui",
	"dlx":               "dlx",
	"fetch":             "dlx",
	"version":           "version",
	"--version":         "version",
	"-v":                "version",
	"help":              "help",
	"--help":            "help",
	"-h":                "help",
}

func resolveCommand(input string) (string, error) {
	if canonical, ok := commandAliases[input]; ok {
		return canonical, nil
	}

	matches := make(map[string]struct{})
	for alias, canonical := range commandAliases {
		if strings.HasPrefix(alias, input) {
			matches[canonical] = struct{}{}
		}
	}
	if len(matches) == 1 {
		for canonical := range matches {
			return canonical, nil
		}
	}
	if len(matches) > 1 {
		candidates := make([]string, 0, len(matches))
		for canonical := range matches {
			candidates = append(candidates, canonical)
		}
		sort.Strings(candidates)
		return "", fmt.Errorf("ambiguous command %q, candidates: %s", input, strings.Join(candidates, ", "))
	}

	suggestion := suggestCommand(input)
	if suggestion != "" {
		return "", fmt.Errorf("unknown command %q, did you mean %q?", input, suggestion)
	}
	return "", fmt.Errorf("unknown command %q", input)
}

func suggestCommand(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	candidates := []string{"init", "doctor", "uninstall", "update", "rollback", "genpass", "hook", "env", "keygen", "encrypt", "decrypt", "inspect", "rotate", "repassword", "tui", "dlx", "version", "help"}
	best := ""
	bestDistance := 99
	for _, candidate := range candidates {
		distance := levenshtein(input, candidate)
		if distance < bestDistance {
			bestDistance = distance
			best = candidate
		}
	}
	if bestDistance <= 3 {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		copy(prev, curr)
	}

	return prev[len(b)]
}

func minInt(a, b, c int) int {
	min := a
	if b < min {
		min = b
	}
	if c < min {
		min = c
	}
	return min
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", appName, version)
}

func resolveConfigSource(cwd string) (configSource, error) {
	primaryPath := filepath.Join(cwd, primaryConfig)
	if _, err := os.Stat(primaryPath); err == nil {
		return configSource{Path: primaryPath, Exists: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return configSource{}, err
	}

	legacyPath := filepath.Join(cwd, legacyConfig)
	if _, err := os.Stat(legacyPath); err == nil {
		return configSource{Path: legacyPath, Exists: true, Legacy: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return configSource{}, err
	}

	return configSource{Path: primaryPath}, nil
}

func collectDoctorReport(cwd string) (doctorReport, error) {
	source, err := resolveConfigSource(cwd)
	if err != nil {
		return doctorReport{}, err
	}
	report := doctorReport{Config: source}

	cfg := config.Default()
	if source.Exists {
		loaded, err := config.Load(source.Path)
		if err != nil {
			report.ConfigError = err
		} else {
			cfg = loaded
		}
	}

	report.RecipientCount = len(cfg.Age.Recipients)
	report.KeyPath = expandHome(cfg.KeyFile)
	if _, err := os.Stat(report.KeyPath); err == nil {
		report.KeyExists = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return doctorReport{}, err
	} else if cfg.KeyFile == config.DefaultKeyFile {
		legacyPath := expandHome(config.LegacyKeyFile)
		if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
			report.KeyPath = legacyPath
			report.KeyExists = true
			report.KeyUsesLegacy = true
		} else if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
			return doctorReport{}, legacyErr
		}
	}

	candidates, err := discovery.FindCandidates(cwd)
	if err != nil {
		return doctorReport{}, err
	}
	report.SuggestedFiles = len(candidates)

	encryptedFiles, err := findEncryptedFiles(cwd)
	if err != nil {
		return doctorReport{}, err
	}
	report.EncryptedFiles = len(encryptedFiles)
	return report, nil
}

func printDoctorReport(w io.Writer, report doctorReport) {
	fmt.Fprintln(w, doctorTitle)
	fmt.Fprintln(w)

	switch {
	case report.ConfigError != nil:
		fmt.Fprintf(w, "Config: ERROR (%s)\n", report.Config.Path)
		fmt.Fprintf(w, "Config Error: %v\n", report.ConfigError)
	case report.Config.Exists && report.Config.Legacy:
		fmt.Fprintf(w, "Config: OK (%s, %s)\n", report.Config.Path, legacyDoctorNote)
	case report.Config.Exists:
		fmt.Fprintf(w, "Config: OK (%s)\n", report.Config.Path)
	default:
		fmt.Fprintf(w, "Config: MISSING (%s)\n", report.Config.Path)
	}

	switch {
	case report.KeyExists && report.KeyUsesLegacy:
		fmt.Fprintf(w, "Key File: OK (%s, legacy fallback)\n", report.KeyPath)
	case report.KeyExists:
		fmt.Fprintf(w, "Key File: OK (%s)\n", report.KeyPath)
	default:
		fmt.Fprintf(w, "Key File: MISSING (%s)\n", report.KeyPath)
	}

	fmt.Fprintf(w, "Recipients: %d\n", report.RecipientCount)
	fmt.Fprintf(w, "Suggested Files: %d\n", report.SuggestedFiles)
	fmt.Fprintf(w, "Encrypted Files: %d\n", report.EncryptedFiles)

	printDoctorSuggestions(w, report)
}

// printDoctorSuggestions emits actionable next steps based on the report
// state. Empty when everything is healthy.
func printDoctorSuggestions(w io.Writer, report doctorReport) {
	var hints []string

	if report.ConfigError != nil {
		hints = append(hints, fmt.Sprintf("Fix the config syntax error: %v", report.ConfigError))
	} else if !report.Config.Exists {
		hints = append(hints, "Run `dpx init` to create a .dpx.yaml in this directory")
	}

	if !report.KeyExists && report.RecipientCount == 0 {
		hints = append(hints, "Run `dpx keygen` to generate an age key pair for the age backend")
	} else if !report.KeyExists && report.RecipientCount > 0 {
		hints = append(hints, "Config references recipients but no key file found at "+report.KeyPath)
	}

	if report.SuggestedFiles == 0 && report.EncryptedFiles == 0 && report.Config.Exists {
		hints = append(hints, "Drop a .env (or similar secret file) here, then run `dpx encrypt <file>`")
	}

	if report.EncryptedFiles > 0 && !report.KeyExists {
		hints = append(hints, "Encrypted files exist but no key file is available — restore the key from backup to decrypt")
	}

	if len(hints) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Suggestions:")
	for i, hint := range hints {
		fmt.Fprintf(w, "  %d. %s\n", i+1, hint)
	}
}

func findEncryptedFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".dpx") {
			files = append(files, filepath.Join(root, entry.Name()))
		}
	}
	return files, nil
}

func splitCSV(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func hasLongFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

type encryptArgs struct {
	filePath       string
	outPath        string
	passwordText   string
	recipientsText string
	useAge         bool
	kdfProfile     string
}

func parseEnvEncryptArgs(args []string) (envEncryptArgs, error) {
	var parsed envEncryptArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--mode":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --mode")
			}
			parsed.mode = strings.ToLower(strings.TrimSpace(args[i]))
		case "--keys":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --keys")
			}
			parsed.keysText = args[i]
		case "--recipient":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --recipient")
			}
			parsed.recipientsText = args[i]
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--kdf-profile":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --kdf-profile")
			}
			parsed.kdfProfile = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath == "" {
				parsed.filePath = arg
				continue
			}
			return parsed, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return parsed, nil
}

func parseEnvDecryptArgs(args []string) (envDecryptArgs, error) {
	var parsed envDecryptArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--identity":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --identity")
			}
			parsed.identityPath = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath == "" {
				parsed.filePath = arg
				continue
			}
			return parsed, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return parsed, nil
}

func parseEncryptArgs(args []string) (encryptArgs, error) {
	var parsed encryptArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--kdf-profile":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --kdf-profile")
			}
			parsed.kdfProfile = args[i]
		case "--recipient":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --recipient")
			}
			parsed.recipientsText = args[i]
		case "--age":
			parsed.useAge = true
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath == "" {
				parsed.filePath = arg
				continue
			}
			return parsed, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return parsed, nil
}

type decryptArgs struct {
	filePath     string
	outPath      string
	passwordText string
	identityPath string
}

func parseDecryptArgs(args []string) (decryptArgs, error) {
	var parsed decryptArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--out":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --out")
			}
			parsed.outPath = args[i]
		case "--password":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --password")
			}
			parsed.passwordText = args[i]
		case "--identity":
			i++
			if i >= len(args) {
				return parsed, fmt.Errorf("missing value for --identity")
			}
			parsed.identityPath = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("unknown flag %q", arg)
			}
			if parsed.filePath == "" {
				parsed.filePath = arg
				continue
			}
			return parsed, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return parsed, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", appName, version)
	fmt.Fprintf(w, "%s <command> [flags]\n", appName)
	fmt.Fprintf(w, "%s <file> [flags]\n", appName)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Quick mode:")
	fmt.Fprintln(w, "  dpx .env                  # encrypt")
	fmt.Fprintln(w, "  dpx notes.txt             # encrypt any file")
	fmt.Fprintln(w, "  dpx .env.dpx              # decrypt")
	fmt.Fprintln(w, "  dpx run -- app            # load env then execute command")
	fmt.Fprintln(w, "  dpx policy check .env     # detect plaintext sensitive values")
	fmt.Fprintln(w, "  dpx encrypt               # interactive file picker + prompts")
	fmt.Fprintln(w, "  dpx env encrypt           # interactive .env inline flow")
	fmt.Fprintln(w, "  dpx e .env                # short alias for encrypt")
	fmt.Fprintln(w, "  dpx d .env.dpx            # short alias for decrypt")
	fmt.Fprintln(w, "  dpx encr .env             # prefix command (auto-resolve to encrypt)")
	fmt.Fprintln(w, "  dpx decr .env.dpx         # prefix command (auto-resolve to decrypt)")
	fmt.Fprintln(w, "  dpx uninstall --yes --remove-key --remove-encrypted")
	fmt.Fprintln(w, "  dpx update                # update to latest release")
	fmt.Fprintln(w, "  dpx update --version v1.2.3")
	fmt.Fprintln(w, "  dpx rollback              # restore previous binary backup")
	fmt.Fprintln(w, "  dpx genpass --length 32 --count 3 --no-symbols")
	fmt.Fprintln(w, "  dpx rotate                # regenerate key and re-encrypt everything")
	fmt.Fprintln(w, "  dpx repassword .env.dpx --generate-password")
	fmt.Fprintln(w, "  dpx env encrypt .env --mode age --keys API_KEY,JWT_SECRET")
	fmt.Fprintln(w, "  dpx env decrypt .env.dpx --password <pass>")
	fmt.Fprintln(w, "  dpx env list .env.dpx --password <pass>")
	fmt.Fprintln(w, "  dpx env get .env.dpx --key API_KEY --password <pass>")
	fmt.Fprintln(w, "  dpx env set .env --key API_KEY --value abc --encrypt --mode age")
	fmt.Fprintln(w, "  dpx env updatekeys .env.dpx --recipient age1new...,age1team...")
	fmt.Fprintln(w, "  dpx keygen --import-file age-keys.txt")
	fmt.Fprintln(w, "  dpx dlx https://github.com/o/r/blob/main/file.go")
	fmt.Fprintln(w, "  dpx dlx https://github.com/o/r/tree/main/src")
	fmt.Fprintln(w, "  dpx dlx https://github.com/o/r/tree/main/src --glob '*.tsx'")
	fmt.Fprintln(w, "  dpx dlx https://example.com/backup.zip")
	fmt.Fprintln(w, "  dpx dlx url1 url2 url3 --output ./downloads")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  init")
	fmt.Fprintln(w, "  keygen")
	fmt.Fprintln(w, "  encrypt (enc)")
	fmt.Fprintln(w, "  decrypt (dec)")
	fmt.Fprintln(w, "  inspect")
	fmt.Fprintln(w, "  hook (install|uninstall)")
	fmt.Fprintln(w, "  rotate (rekey)")
	fmt.Fprintln(w, "  repassword (passwd)")
	fmt.Fprintln(w, "  run")
	fmt.Fprintln(w, "  policy (check)")
	fmt.Fprintln(w, "  tui")
	fmt.Fprintln(w, "  dlx (fetch)")
	fmt.Fprintln(w, "  doctor")
	fmt.Fprintln(w, "  uninstall")
	fmt.Fprintln(w, "  update")
	fmt.Fprintln(w, "  rollback")
	fmt.Fprintln(w, "  genpass (passgen)")
	fmt.Fprintln(w, "  env (encrypt|decrypt|list|get|set|updatekeys)")
	fmt.Fprintln(w, "  version")
	fmt.Fprintln(w, "  help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --version, -v")
	fmt.Fprintln(w, "  uninstall: --yes --remove-key --remove-encrypted")
	fmt.Fprintln(w, "  update: [--version]")
	fmt.Fprintln(w, "  genpass: [--length] [--count] [--no-symbols]")
	fmt.Fprintln(w, "  keygen: [--out] [--regen] [--import-file] [--import-stdin] [--no-config-update]")
	fmt.Fprintln(w, "  encrypt: [file] [--password] [--age] [--recipient] [--kdf-profile] [--out]")
	fmt.Fprintln(w, "  repassword: [file] [--old-password] [--new-password|--generate-password] [--password-length] [--kdf-profile] [--out]")
	fmt.Fprintln(w, "  run: [--file|-f] [--password] [--identity] -- <command>")
	fmt.Fprintln(w, "  env encrypt: --mode --keys --recipient --password --kdf-profile --out")
	fmt.Fprintln(w, "  env decrypt: --password --identity --out")
	fmt.Fprintln(w, "  env list: [file] [--password] [--identity]")
	fmt.Fprintln(w, "  env get: [file] --key [--password] [--identity]")
	fmt.Fprintln(w, "  env set: [file] --key --value [--encrypt --mode --recipient --password --kdf-profile --out]")
	fmt.Fprintln(w, "  env updatekeys: [file] --recipient [--keys] [--identity] [--out]")
	fmt.Fprintln(w, "  dlx: <url>... [--output|-o] [--token] [--ref] [--glob] [--max-size] [--no-prefix] [--quiet]")
	fmt.Fprintln(w, "Notes:")
	fmt.Fprintln(w, "  - update/rollback use local binary backup file (.rollback)")
	fmt.Fprintln(w, "  - optional env DPX_UPDATE_BASE_URL overrides release asset base URL")
	fmt.Fprintln(w, "  - optional env DPX_TUI_MODE=fallback|fullscreen controls TUI mode")
	fmt.Fprintln(w, "  - Password prompts are interactive and require confirmation when encrypting")
	fmt.Fprintln(w, "  - Password mode supports --kdf-profile balanced|hardened|paranoid")
	fmt.Fprintln(w, "  - Omit file args to use guided picker/search flow")
}

func expandHome(path string) string {
	rest, ok := trimHomePrefix(path)
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	rest = strings.ReplaceAll(rest, "\\", string(os.PathSeparator))
	rest = strings.ReplaceAll(rest, "/", string(os.PathSeparator))
	return filepath.Join(home, rest)
}

func padRight(text string, width int) string {
	if len(text) >= width {
		return truncate(text, width)
	}
	return text + strings.Repeat(" ", width-len(text))
}

func truncate(text string, width int) string {
	if len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	return text[:width-1] + "…"
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

func fileDescriptorInt(file *os.File) (int, bool) {
	fd := file.Fd()
	maxInt := ^uintptr(0) >> 1
	if fd > maxInt {
		return 0, false
	}
	return int(fd), true
}

func trimHomePrefix(path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "~/"):
		return path[2:], true
	case strings.HasPrefix(path, "~\\"):
		return path[2:], true
	default:
		return "", false
	}
}
