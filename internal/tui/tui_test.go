package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dwirx/dpx/internal/app"
	"github.com/dwirx/dpx/internal/config"
	"github.com/dwirx/dpx/internal/crypto/agex"
	"github.com/dwirx/dpx/internal/discovery"
)

func TestModelEncryptWithoutCandidatesPromptsManualPath(t *testing.T) {
	t.Parallel()

	model, err := NewModel(app.New(config.Default()), config.Default(), t.TempDir(), nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	got := updated.(Model)

	if got.stage != stageEncryptFile {
		t.Fatalf("expected encrypt file stage, got %v", got.stage)
	}
	if !containsOption(got.options, manualEncryptPathOption) {
		t.Fatalf("expected manual option in stage options, got %#v", got.options)
	}
}

func TestModelEncryptWithCandidatesHasManualPathOption(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)

	if menu.stage != stageEncryptFile {
		t.Fatalf("expected encrypt file selection stage, got %v", menu.stage)
	}
	if len(menu.options) < 2 {
		t.Fatalf("expected candidate + manual option, got %#v", menu.options)
	}
	manualIdx := optionIndex(menu.options, manualEncryptPathOption)
	if manualIdx < 0 {
		t.Fatalf("expected manual option, got %#v", menu.options)
	}

	menu.selection = manualIdx
	updated, _ = menu.submitSelection()
	got := updated.(Model)
	if got.stage != stageEncryptManualPath {
		t.Fatalf("expected manual path input stage, got %v", got.stage)
	}
	if got.input.Prompt != "File to encrypt: " {
		t.Fatalf("unexpected prompt: %q", got.input.Prompt)
	}
}

func TestModelEncryptPasswordRequiresConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	menu.selection = 1
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEncryptPassword {
		t.Fatalf("expected password stage, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEncryptPasswordConfirm {
		t.Fatalf("expected password confirm stage, got %v", menu.stage)
	}

	menu.input.SetValue("wrong-secret")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEncryptPassword {
		t.Fatalf("expected password stage after mismatch, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEncryptPasswordConfirm {
		t.Fatalf("expected password confirm stage, got %v", menu.stage)
	}
	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEncryptOutput {
		t.Fatalf("expected encrypt output stage, got %v", menu.stage)
	}
}

func TestModelEnvInlineEncryptPasswordRequiresConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	envEncryptIdx := optionIndex(model.options, "Env Inline Encrypt")
	if envEncryptIdx < 0 {
		t.Fatalf("missing Env Inline Encrypt option: %#v", model.options)
	}
	model.selection = envEncryptIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageEnvEncryptFile {
		t.Fatalf("expected env encrypt file stage, got %v", menu.stage)
	}

	envFileIdx := -1
	for i, option := range menu.options {
		if strings.Contains(option, ".env") {
			envFileIdx = i
			break
		}
	}
	if envFileIdx < 0 {
		t.Fatalf("expected .env candidate, got %#v", menu.options)
	}
	menu.selection = envFileIdx
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptMode {
		t.Fatalf("expected env encrypt mode stage, got %v", menu.stage)
	}

	menu.selection = 1 // Password
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptKeys {
		t.Fatalf("expected env encrypt keys stage, got %v", menu.stage)
	}
	menu.input.SetValue("all")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptPassword {
		t.Fatalf("expected env encrypt password stage, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptPasswordConfirm {
		t.Fatalf("expected env encrypt password confirm stage, got %v", menu.stage)
	}

	menu.input.SetValue("wrong-secret")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptPassword {
		t.Fatalf("expected env encrypt password stage after mismatch, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptPasswordConfirm {
		t.Fatalf("expected env encrypt password confirm stage, got %v", menu.stage)
	}
	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvEncryptOutput {
		t.Fatalf("expected env encrypt output stage, got %v", menu.stage)
	}
}

func TestModelEnvInlineDecryptPasswordRequiresConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.Default()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	svc := app.New(cfg)
	encrypted, err := svc.EncryptEnvInlineFile(app.EnvInlineEncryptRequest{
		InputPath:    envPath,
		OutputPath:   envPath + cfg.DefaultSuffix,
		Mode:         "password",
		Passphrase:   []byte("secret-123"),
		SelectedKeys: []string{"API_KEY"},
	})
	if err != nil {
		t.Fatalf("encrypt env inline seed file: %v", err)
	}

	model, err := NewModel(svc, cfg, dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	envDecryptIdx := optionIndex(model.options, "Env Inline Decrypt")
	if envDecryptIdx < 0 {
		t.Fatalf("missing Env Inline Decrypt option: %#v", model.options)
	}
	model.selection = envDecryptIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageEnvDecryptFile {
		t.Fatalf("expected env decrypt file stage, got %v", menu.stage)
	}

	decFileIdx := optionIndex(menu.options, encrypted.OutputPath)
	if decFileIdx < 0 {
		t.Fatalf("expected encrypted env file option %q, got %#v", encrypted.OutputPath, menu.options)
	}
	menu.selection = decFileIdx
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEnvDecryptPassword {
		t.Fatalf("expected env decrypt password stage, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvDecryptPasswordConfirm {
		t.Fatalf("expected env decrypt password confirm stage, got %v", menu.stage)
	}

	menu.input.SetValue("wrong-secret")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvDecryptPassword {
		t.Fatalf("expected env decrypt password stage after mismatch, got %v", menu.stage)
	}

	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvDecryptPasswordConfirm {
		t.Fatalf("expected env decrypt password confirm stage, got %v", menu.stage)
	}
	menu.input.SetValue("secret-123")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEnvDecryptOutput {
		t.Fatalf("expected env decrypt output stage, got %v", menu.stage)
	}
}

func TestModelImportFromFilePrefillsOutputWithImportPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	identity, err := agex.GenerateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	importRaw := strings.Join([]string{
		"# created: 2026-03-17T11:47:02Z",
		"# public key: " + identity.PublicKey,
		identity.PrivateKey,
		"",
	}, "\n")
	importPath := filepath.Join(dir, "age-keys.txt")
	if err := os.WriteFile(importPath, []byte(importRaw), 0o600); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageImportSource {
		t.Fatalf("expected import source stage, got %v", menu.stage)
	}

	menu.selection = 0 // From file
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportFilePath {
		t.Fatalf("expected import file path stage, got %v", menu.stage)
	}

	menu.input.SetValue(importPath)
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportOutput {
		t.Fatalf("expected import output stage, got %v", menu.stage)
	}
	if menu.input.Value() != importPath {
		t.Fatalf("expected output prefill %q, got %q", importPath, menu.input.Value())
	}
}

func TestModelImportRawWaitsForAgeSecretKeyLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageImportSource {
		t.Fatalf("expected import source stage, got %v", menu.stage)
	}

	menu.selection = 1 // Paste private key
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected import raw stage, got %v", menu.stage)
	}

	menu.input.SetValue("# created: 2026-03-17T18:14:43Z")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected to stay in import raw stage until secret key line, got %v", menu.stage)
	}
	if !strings.Contains(strings.ToLower(menu.help), "age-secret-key") {
		t.Fatalf("expected guidance mentioning AGE-SECRET-KEY, got %q", menu.help)
	}
}

func TestModelImportFromFileDetectsPastedKeyBlockStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	menu.selection = 0 // From file
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportFilePath {
		t.Fatalf("expected import file path stage, got %v", menu.stage)
	}

	menu.input.SetValue("# created: 2026-03-17T18:14:43Z")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected import raw stage after pasted key block start, got %v", menu.stage)
	}
	if menu.importBuffer == "" {
		t.Fatal("expected import buffer to keep pasted metadata line")
	}
}

func TestModelImportRawAcceptsLineByLinePaste(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	identity, err := agex.GenerateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	menu.selection = 1 // Paste private key
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected import raw stage, got %v", menu.stage)
	}

	menu.input.SetValue("# created: 2026-03-17T18:14:43Z")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected stay in import raw stage after metadata, got %v", menu.stage)
	}

	menu.input.SetValue("# public key: " + identity.PublicKey)
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected stay in import raw stage after public key, got %v", menu.stage)
	}

	menu.input.SetValue(identity.PrivateKey)
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportOutput {
		t.Fatalf("expected import output stage after secret key line, got %v", menu.stage)
	}
	if menu.importRaw != identity.PrivateKey {
		t.Fatalf("expected extracted private key %q, got %q", identity.PrivateKey, menu.importRaw)
	}
}

func TestModelImportRawAllowsTypingQWithoutQuit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	menu.selection = 1 // Paste private key
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected import raw stage, got %v", menu.stage)
	}

	updatedModel, _ := menu.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got := updatedModel.(Model)
	if got.stage != stageImportRaw {
		t.Fatalf("expected stay in import raw stage, got %v", got.stage)
	}
	if got.input.Value() != "q" {
		t.Fatalf("expected input value to keep typed 'q', got %q", got.input.Value())
	}
}

func TestModelImportRawAcceptsAgeKeysBlockText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	identity, err := agex.GenerateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	importIdx := optionIndex(model.options, "Import Key")
	if importIdx < 0 {
		t.Fatalf("missing Import Key option: %#v", model.options)
	}
	model.selection = importIdx
	updated, _ := model.submitSelection()
	menu := updated.(Model)
	menu.selection = 1 // Paste private key
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageImportRaw {
		t.Fatalf("expected import raw stage, got %v", menu.stage)
	}

	block := strings.Join([]string{
		"# created: 2026-03-17T18:14:43Z",
		"# public key: " + identity.PublicKey,
		identity.PrivateKey,
	}, "\n")
	menu.input.SetValue(block)
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageImportOutput {
		t.Fatalf("expected import output stage, got %v", menu.stage)
	}
	if menu.importRaw != identity.PrivateKey {
		t.Fatalf("expected extracted private key %q, got %q", identity.PrivateKey, menu.importRaw)
	}
}

func TestModelEscBackReturnsToPreviousStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageEncryptFile {
		t.Fatalf("expected encrypt file stage, got %v", menu.stage)
	}

	backed, _ := menu.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := backed.(Model)
	if got.stage != stageAction {
		t.Fatalf("expected back to action stage, got %v", got.stage)
	}
}

func TestModelCtrlVTogglePasswordVisibility(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	menu.selection = 1
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEncryptPassword {
		t.Fatalf("expected password stage, got %v", menu.stage)
	}
	if menu.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("expected password echo mode, got %v", menu.input.EchoMode)
	}

	toggled, _ := menu.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	got := toggled.(Model)
	if got.input.EchoMode != textinput.EchoNormal {
		t.Fatalf("expected normal echo mode after toggle, got %v", got.input.EchoMode)
	}

	toggled, _ = got.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	got = toggled.(Model)
	if got.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("expected password echo mode after second toggle, got %v", got.input.EchoMode)
	}
}

func TestModelEncryptSuggestionsIncludeCommonFileTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{".env", "notes.txt", "README.md", "script.js", "payload.bin", "app.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("DATA\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	if menu.stage != stageEncryptFile {
		t.Fatalf("expected encrypt file stage, got %v", menu.stage)
	}
	if menu.title != "Select a file to encrypt (all files mode)" {
		t.Fatalf("unexpected title: %q", menu.title)
	}

	options := strings.Join(menu.options, "\n")
	for _, name := range []string{".env", "notes.txt", "README.md", "script.js", "payload.bin", "app.exe"} {
		if !strings.Contains(options, name) {
			t.Fatalf("expected %s in options, got %#v", name, menu.options)
		}
	}
}

func TestModelEncryptSearchFiltersCandidates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{".env", "notes.txt", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("DATA\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	searchIdx := optionIndex(menu.options, searchEncryptPathOption)
	if searchIdx < 0 {
		t.Fatalf("expected search option, got %#v", menu.options)
	}
	menu.selection = searchIdx
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEncryptSearchQuery {
		t.Fatalf("expected search query stage, got %v", menu.stage)
	}

	menu.input.SetValue("readme")
	updated, _ = menu.submitInput()
	menu = updated.(Model)
	if menu.stage != stageEncryptFile {
		t.Fatalf("expected back to encrypt file stage, got %v", menu.stage)
	}
	if !strings.Contains(strings.Join(menu.options, "\n"), "README.md") {
		t.Fatalf("expected filtered option to include README.md, got %#v", menu.options)
	}
	if strings.Contains(strings.Join(menu.options, "\n"), "notes.txt") {
		t.Fatalf("expected notes.txt to be filtered out, got %#v", menu.options)
	}
}

func TestModelEncryptSearchRealtimeSuggestions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{".env", "notes.txt", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("DATA\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	searchIdx := optionIndex(menu.options, searchEncryptPathOption)
	if searchIdx < 0 {
		t.Fatalf("expected search option, got %#v", menu.options)
	}
	menu.selection = searchIdx
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.stage != stageEncryptSearchQuery {
		t.Fatalf("expected search query stage, got %v", menu.stage)
	}

	menu.input.SetValue("readme.md")
	menu.syncEncryptSearchSuggestions()

	if menu.stage != stageEncryptSearchQuery {
		t.Fatalf("expected to stay in search stage during typing, got %v", menu.stage)
	}
	if len(menu.encryptShown) != 1 {
		t.Fatalf("expected one live suggestion, got %#v", menu.encryptShown)
	}
	if !strings.Contains(menu.encryptShown[0], "README.md") {
		t.Fatalf("expected README.md suggestion, got %#v", menu.encryptShown)
	}
	if !strings.Contains(menu.help, "suggestion") {
		t.Fatalf("expected suggestion help message, got %q", menu.help)
	}

	view := menu.View()
	if !strings.Contains(view, "Suggestions: 1") {
		t.Fatalf("expected suggestions counter in view, got %q", view)
	}
	if !strings.Contains(view, "README.md") {
		t.Fatalf("expected README.md in suggestions view, got %q", view)
	}
}

func TestModelEncryptCanSwitchScopeToEnvMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{".env", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("DATA\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	model, err := NewModel(app.New(config.Default()), config.Default(), dir, nil, io.Discard)
	if err != nil {
		t.Fatalf("new model: %v", err)
	}

	updated, _ := model.submitSelection()
	menu := updated.(Model)
	switchIdx := optionIndex(menu.options, encryptScopeSwitchOption(encryptScopeAny))
	if switchIdx < 0 {
		t.Fatalf("expected scope switch option, got %#v", menu.options)
	}
	menu.selection = switchIdx
	updated, _ = menu.submitSelection()
	menu = updated.(Model)
	if menu.title != "Select a file to encrypt (.env mode)" {
		t.Fatalf("expected env mode title, got %q", menu.title)
	}
	options := strings.Join(menu.options, "\n")
	if !strings.Contains(options, ".env") {
		t.Fatalf("expected .env option, got %#v", menu.options)
	}
	if strings.Contains(options, "notes.txt") {
		t.Fatalf("expected notes.txt excluded in env mode, got %#v", menu.options)
	}
}

func containsOption(options []string, target string) bool {
	return optionIndex(options, target) >= 0
}

func optionIndex(options []string, target string) int {
	for i, option := range options {
		if option == target {
			return i
		}
	}
	return -1
}

func TestSplitSearchMatch(t *testing.T) {
	t.Parallel()

	prefix, match, suffix, ok := splitSearchMatch("/tmp/README.md", "read")
	if !ok {
		t.Fatalf("expected match")
	}
	if prefix != "/tmp/" {
		t.Fatalf("unexpected prefix: %q", prefix)
	}
	if match != "READ" {
		t.Fatalf("unexpected match segment: %q", match)
	}
	if suffix != "ME.md" {
		t.Fatalf("unexpected suffix: %q", suffix)
	}

	_, _, _, ok = splitSearchMatch("/tmp/notes.txt", "read")
	if ok {
		t.Fatalf("did not expect match")
	}
}

func TestEncryptOptionsIncludesManualPath(t *testing.T) {
	t.Parallel()

	candidates := []discovery.Candidate{
		{Path: "/tmp/a.env"},
		{Path: "/tmp/b.env"},
	}
	got := encryptOptions(candidates)

	if len(got) != len(candidates)+1 {
		t.Fatalf("expected %d options, got %d", len(candidates)+1, len(got))
	}
	if got[len(got)-1] != manualEncryptPathOption {
		t.Fatalf("expected last option to be manual path, got %q", got[len(got)-1])
	}
	if got[0] != "/tmp/a.env" || got[1] != "/tmp/b.env" {
		t.Fatalf("expected candidate order preserved, got %#v", got)
	}

	if got := encryptOptions(nil); len(got) != 1 || got[0] != manualEncryptPathOption {
		t.Fatalf("expected single manual option for empty candidates, got %#v", got)
	}
}

func TestEncryptScopeName(t *testing.T) {
	t.Parallel()

	if got := encryptScopeName(encryptScopeEnv); got != ".env" {
		t.Fatalf("expected .env scope name, got %q", got)
	}
	if got := encryptScopeName(encryptScopeAny); got != "all files" {
		t.Fatalf("expected 'all files' scope name, got %q", got)
	}
	// Default scope (zero value) should also report "all files".
	if got := encryptScopeName(encryptScope("")); got != "all files" {
		t.Fatalf("expected zero scope to be 'all files', got %q", got)
	}
}

func TestIsEncryptedPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path   string
		suffix string
		want   bool
	}{
		{"/tmp/.env.dpx", ".dpx", true},
		{"/tmp/.env", ".dpx", false},
		{"/tmp/secrets.enc", ".enc", true},
		{"/tmp/secrets.enc", ".dpx", false},
		{"/tmp/secrets.dpx", ".enc", true}, // .dpx is always encrypted
		{"", ".dpx", false},
	}
	for _, tc := range cases {
		if got := isEncryptedPath(tc.path, tc.suffix); got != tc.want {
			t.Errorf("isEncryptedPath(%q, %q) = %v, want %v", tc.path, tc.suffix, got, tc.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,  c  ", []string{"a", "b", "c"}},
		{"a,,b,", []string{"a", "b"}}, // empty parts skipped
		{",", nil},
	}
	for _, tc := range cases {
		got := splitCSV(tc.in)
		if !equalStringSlice(got, tc.want) {
			t.Errorf("splitCSV(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestParseEnvKeysInput(t *testing.T) {
	t.Parallel()

	available := []string{"API_KEY", "DB_URL", "JWT_SECRET"}

	t.Run("empty selects all", func(t *testing.T) {
		t.Parallel()
		got, err := parseEnvKeysInput("", available)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStringSlice(got, available) {
			t.Fatalf("expected all keys, got %#v", got)
		}
	})

	t.Run("'all' keyword selects all", func(t *testing.T) {
		t.Parallel()
		got, err := parseEnvKeysInput("ALL", available)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStringSlice(got, available) {
			t.Fatalf("expected all keys, got %#v", got)
		}
	})

	t.Run("explicit subset", func(t *testing.T) {
		t.Parallel()
		got, err := parseEnvKeysInput("API_KEY, JWT_SECRET", available)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStringSlice(got, []string{"API_KEY", "JWT_SECRET"}) {
			t.Fatalf("unexpected subset, got %#v", got)
		}
	})

	t.Run("dedupe", func(t *testing.T) {
		t.Parallel()
		got, err := parseEnvKeysInput("API_KEY,API_KEY,JWT_SECRET", available)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equalStringSlice(got, []string{"API_KEY", "JWT_SECRET"}) {
			t.Fatalf("expected dedupe, got %#v", got)
		}
	})

	t.Run("unknown key errors", func(t *testing.T) {
		t.Parallel()
		_, err := parseEnvKeysInput("API_KEY,DOES_NOT_EXIST", available)
		if err == nil {
			t.Fatalf("expected error for unknown key")
		}
		if !strings.Contains(err.Error(), "DOES_NOT_EXIST") {
			t.Fatalf("expected error to mention unknown key, got %v", err)
		}
	})

	t.Run("only commas errors", func(t *testing.T) {
		t.Parallel()
		_, err := parseEnvKeysInput(",,,", available)
		if err == nil {
			t.Fatalf("expected error for empty input")
		}
	})
}

func TestExtractAgeSecretKey(t *testing.T) {
	t.Parallel()

	const key = "AGE-SECRET-KEY-1MT2MKR4WF6L6Q4X6ULU9CCFT78QCR2YJRU3ZWY2U0XX0DSAS2UXQJQJLRG"

	t.Run("plain", func(t *testing.T) {
		if got := extractAgeSecretKey(key); got != key {
			t.Fatalf("expected %q, got %q", key, got)
		}
	})

	t.Run("embedded in prose", func(t *testing.T) {
		got := extractAgeSecretKey("paste your key below:\n" + key + "\nthanks!")
		if got != key {
			t.Fatalf("expected key extraction from prose, got %q", got)
		}
	})

	t.Run("quoted", func(t *testing.T) {
		got := extractAgeSecretKey(`"age secret: ` + key + `"`)
		if got != key {
			t.Fatalf("expected key extraction from quoted text, got %q", got)
		}
	})

	t.Run("missing", func(t *testing.T) {
		if got := extractAgeSecretKey("nothing here"); got != "" {
			t.Fatalf("expected empty result, got %q", got)
		}
	})
}

func TestFilterPathsByQuery(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/tmp/notes.md",
		"/tmp/.env",
		"/tmp/secrets.yaml",
		"/home/user/data.json",
	}

	t.Run("substring on basename", func(t *testing.T) {
		got := filterPathsByQuery(paths, "notes")
		if !equalStringSlice(got, []string{"/tmp/notes.md"}) {
			t.Fatalf("expected single match on basename, got %#v", got)
		}
	})

	t.Run("substring on full path", func(t *testing.T) {
		got := filterPathsByQuery(paths, "user")
		if !equalStringSlice(got, []string{"/home/user/data.json"}) {
			t.Fatalf("expected full-path match, got %#v", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		got := filterPathsByQuery(paths, "SECRETS")
		if !equalStringSlice(got, []string{"/tmp/secrets.yaml"}) {
			t.Fatalf("expected case-insensitive match, got %#v", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		got := filterPathsByQuery(paths, "nope")
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %#v", got)
		}
	})

	t.Run("empty query returns all", func(t *testing.T) {
		got := filterPathsByQuery(paths, "")
		if len(got) != len(paths) {
			t.Fatalf("expected all paths when query empty, got %d", len(got))
		}
	})
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
