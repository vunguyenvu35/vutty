package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vutty/internal/vault"
)

func newBridgeApp(t *testing.T) (*App, *Bridge, *[]string, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := Build(dir, vault.NewMemKeychain(), &vault.KDFParams{Memory: 64, Time: 1, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.db.Close() })
	var changed []string
	b := NewBridge(a, func(s string) { changed = append(changed, s) })
	if err := b.Start(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return a, b, &changed, dir
}

func TestBridgeConnectionFileIsPrivateAndRemovedOnClose(t *testing.T) {
	dir := t.TempDir()
	a, _ := Build(dir, vault.NewMemKeychain(), nil)
	defer a.db.Close()
	b := NewBridge(a, nil)
	if err := b.Start(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, bridgeFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("connection file: %v %v", st, err)
	}
	b.Close()
	if _, err := os.Stat(filepath.Join(dir, bridgeFile)); !os.IsNotExist(err) {
		t.Fatal("the connection file must be removed on close")
	}
}

func TestBridgeRefusesAWindowOfAnotherVersion(t *testing.T) {
	_, _, _, dir := newBridgeApp(t)
	c, err := ConnectBridge(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := Version
	defer func() { Version = old }()
	Version = "0.0.1-left-open" // this process is an older window; the main process answers with its own version
	if err := c.Ping(); err == nil || !strings.Contains(err.Error(), "close it and open it again") {
		t.Fatalf("a window of another version must be refused: %v", err)
	}
}

func TestBridgeCallsUseCasesAndNotifiesTheMainWindowOfChanges(t *testing.T) {
	_, _, changed, dir := newBridgeApp(t)
	c, err := ConnectBridge(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(); err != nil {
		t.Fatal(err)
	}
	got, err := c.CallApp("SettingsService", "Get", "[]")
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(got), &s); err != nil || s["zoom"] == nil {
		t.Fatalf("%s %v", got, err)
	}
	if len(*changed) != 0 {
		t.Fatalf("reading must not notify: %v", *changed)
	}
	s["zoom"] = 120
	arg, _ := json.Marshal([]any{s})
	saved, err := c.CallApp("SettingsService", "Save", string(arg))
	if err != nil || !strings.Contains(saved, `"zoom":120`) {
		t.Fatalf("%s %v", saved, err)
	}
	if len(*changed) != 1 || (*changed)[0] != "SettingsService" {
		t.Fatalf("the main window must be told: %v", *changed)
	}
}

func TestBridgePassesUseCaseErrorsThrough(t *testing.T) {
	_, _, _, dir := newBridgeApp(t)
	c, _ := ConnectBridge(dir)
	arg, _ := json.Marshal([]any{map[string]any{"zoom": 5000}})
	if _, err := c.CallApp("SettingsService", "Save", string(arg)); err == nil || !strings.Contains(err.Error(), "must be") {
		t.Fatalf("want the validation error, got %v", err)
	}
}

func TestBridgeChecksArgumentCountAndTypes(t *testing.T) {
	_, _, _, dir := newBridgeApp(t)
	c, _ := ConnectBridge(dir)
	if _, err := c.CallApp("VaultService", "SetAutoLock", "[]"); err == nil || !strings.Contains(err.Error(), "argument") {
		t.Fatalf("missing argument: %v", err)
	}
	if _, err := c.CallApp("VaultService", "SetAutoLock", `["x"]`); err == nil {
		t.Fatal("a string is not minutes")
	}
	if _, err := c.CallApp("VaultService", "Status", ""); err != nil {
		t.Fatalf("no arguments at all must work: %v", err)
	}
}

func TestBridgeRejectsAWrongToken(t *testing.T) {
	_, _, _, dir := newBridgeApp(t)
	c, _ := ConnectBridge(dir)
	c.token = "guess"
	if err := c.Ping(); err == nil {
		t.Fatal("ping with a wrong token must fail")
	}
	if _, err := c.CallApp("SettingsService", "Get", "[]"); err == nil {
		t.Fatal("call with a wrong token must fail")
	}
}

func TestConnectBridgeExplainsWhenTheMainAppIsNotRunning(t *testing.T) {
	if _, err := ConnectBridge(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("%v", err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, bridgeFile), []byte("{"), 0o600)
	if _, err := ConnectBridge(dir); err == nil {
		t.Fatal("a broken file must be reported")
	}
}

// A web page or a rebinding attack cannot reach the bridge even with a token it somehow learned.
func TestBridgeRefusesBrowserOriginsAndForeignHostNames(t *testing.T) {
	_, b, _, _ := newBridgeApp(t)
	for name, mod := range map[string]func(*http.Request){
		"origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"host":   func(r *http.Request) { r.Host = "evil.example:" + strings.Split(b.ln.Addr().String(), ":")[1] },
	} {
		req, _ := http.NewRequest("GET", "http://"+b.ln.Addr().String()+"/ping", nil)
		req.Header.Set("Authorization", "Bearer "+b.token)
		mod(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: got %d, want 403 even with the right token", name, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", "http://"+b.ln.Addr().String()+"/ping", nil)
	req.Header.Set("Authorization", "Bearer "+b.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("a plain client with the token must still work: %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestReadOnlyCallAssistantPolling(t *testing.T) {
	for _, m := range []string{"Providers", "Settings", "Models"} {
		if !readOnlyCall("AssistantService", m) {
			t.Errorf("AssistantService.%s must not broadcast a change", m)
		}
	}
	if readOnlyCall("AssistantService", "SetProviderEnabled") {
		t.Error("SetProviderEnabled changes data")
	}
}
