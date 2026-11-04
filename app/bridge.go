package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"vutty/internal/usecase"
)

// An app window (Duplicate in new window) is a second native window. Wails v2 can only create one window per
// process, so it is a second Vutty process that holds no data of its own: every read and write goes through this
// bridge to the main process, which owns the vault and the database. The bridge listens on loopback only and
// needs a random token that is stored in a 0600 file inside the data directory.

// bridgeFile is the connection file the app windows read (it kept the name of the Settings window that once used it too).
const bridgeFile = "settings-ipc.json"

type bridgeInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// appTargets are every use case service of the app: an app window (Duplicate in new window) is the whole
// app in another process and needs all of them, as the main window does.
type appTarget struct{ obj any }

// Bridge serves the use cases to the app windows.
type Bridge struct {
	token    string
	app      map[string]any
	onChange func(service string)
	srv      *http.Server
	ln       net.Listener
	file     string

	onMenu func(action string)

	evMu    sync.Mutex
	events  []BridgedEvent
	seq     int
	watched map[string]bool
	wake    chan struct{}
}

// BridgedEvent is a terminal event of a session that a window process opened through the bridge.
type BridgedEvent struct {
	Seq  int             `json:"seq"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// maxBridgedEvents bounds what a window that stopped reading can make the main process hold.
const maxBridgedEvents = 4096

// NewBridge exposes every use case service of the app to the app windows.
func NewBridge(a *App, onChange func(service string)) *Bridge {
	return &Bridge{
		onChange: onChange,
		watched:  map[string]bool{},
		app: map[string]any{
			"HostService": a.Host, "VaultService": a.Vault, "SessionService": a.Session, "ForwardService": a.Forward,
			"SyncService": a.Sync, "ImportExportService": a.ImportExp, "SettingsService": a.Settings, "SnippetService": a.Snippet,
			"SftpService": a.Sftp, "LocalService": a.Local, "SerialService": a.Serial, "LogService": a.Log, "KnownHostsService": a.KnownHost, "AssistantService": a.Assistant,
			"ExecService": a.Exec,
		},
		wake: make(chan struct{}),
	}
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// DispatchApp calls a method of a use case service with JSON arguments and returns its JSON result (for an app window: every method of every service).
func (b *Bridge) DispatchApp(service, method string, args []json.RawMessage) (json.RawMessage, error) {
	obj, ok := b.app[service]
	if !ok || method == "" || method[0] < 'A' || method[0] > 'Z' {
		return nil, fmt.Errorf("%s.%s is not available from an app window", service, method)
	}
	return b.invoke(obj, service, method, args)
}

func (b *Bridge) invoke(obj any, service, method string, args []json.RawMessage) (json.RawMessage, error) {
	m := reflect.ValueOf(obj).MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("%s.%s does not exist", service, method)
	}
	mt := m.Type()
	if mt.IsVariadic() || mt.NumIn() != len(args) {
		return nil, fmt.Errorf("%s.%s takes %d argument(s), got %d", service, method, mt.NumIn(), len(args))
	}
	in := make([]reflect.Value, len(args))
	for i, raw := range args {
		p := reflect.New(mt.In(i))
		if err := json.Unmarshal(raw, p.Interface()); err != nil {
			return nil, fmt.Errorf("%s.%s argument %d: %w", service, method, i+1, err)
		}
		in[i] = p.Elem()
	}
	out := m.Call(in)
	var result any
	for _, o := range out {
		if o.Type() == errorType {
			if !o.IsNil() {
				return nil, o.Interface().(error)
			}
			continue
		}
		result = o.Interface()
	}
	if service == "SessionService" && method == "Open" {
		if sid, ok := result.(string); ok {
			b.watch(sid)
		}
	}
	if service == "SftpService" && method == "Open" {
		if info, ok := result.(usecase.SftpInfo); ok {
			b.watch(info.SID)
		}
	}
	if b.onChange != nil && !readOnlyCall(service, method) {
		b.onChange(service)
	}
	if result == nil {
		return json.RawMessage("null"), nil
	}
	return json.Marshal(result)
}

// watch starts relaying the events of a session that a window process opened.
func (b *Bridge) watch(sid string) {
	b.evMu.Lock()
	b.watched[sid] = true
	b.evMu.Unlock()
}

// Publish offers an event of the main process to the app windows (the /events poll hands it over). The events
// of a terminal or an SFTP session are kept only for those that a window process opened.
func (b *Bridge) Publish(name string, data []any) {
	parts := strings.SplitN(name, ":", 3)
	b.evMu.Lock()
	defer b.evMu.Unlock()
	// a terminal's events go only to the window that opened it; everything else (hosts changed, the vault locked,
	// transfer progress, log lines...) goes to every app window
	if len(parts) == 3 && (parts[0] == "term" || parts[0] == "sftp") && !b.watched[parts[2]] {
		return
	}
	args, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.seq++
	b.events = append(b.events, BridgedEvent{Seq: b.seq, Name: name, Args: args})
	if len(b.events) > maxBridgedEvents {
		b.events = append([]BridgedEvent(nil), b.events[len(b.events)-maxBridgedEvents:]...)
	}
	close(b.wake)
	b.wake = make(chan struct{})
}

// eventsAfter waits (up to 20 s) for events newer than seq.
func (b *Bridge) eventsAfter(ctx context.Context, seq int) ([]BridgedEvent, int) {
	deadline := time.After(20 * time.Second)
	for {
		b.evMu.Lock()
		var out []BridgedEvent
		for _, e := range b.events {
			if e.Seq > seq {
				out = append(out, e)
			}
		}
		last, wake := b.seq, b.wake
		b.evMu.Unlock()
		if len(out) > 0 {
			return out, last
		}
		select {
		case <-wake:
		case <-deadline:
			return nil, last
		case <-ctx.Done():
			return nil, last
		}
	}
}

// SetMenuHandler says what to do when another window of the app (Settings, a terminal window) uses a menu
// item: the main window does it, as if the item were in its own menu bar.
func (b *Bridge) SetMenuHandler(f func(action string)) { b.onMenu = f }

// readOnlyCall is true for calls that only read or only drive a session: the other windows need not refresh.
func readOnlyCall(service, method string) bool {
	if service == "ExecService" || service == "SessionService" || service == "SftpService" || service == "LocalService" || service == "SerialService" || service == "LogService" {
		return true
	}
	if service == "AssistantService" && (method == "Providers" || method == "Settings" || method == "Models") {
		return true // the Settings page asks again while the tools are being looked for: no window needs to refresh for that
	}
	for _, p := range []string{"List", "Get", "Status", "Info", "Home", "Search", "Dump", "Export"} {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}

func (b *Bridge) authorised(r *http.Request) bool {
	got := r.Header.Get("Authorization")
	want := "Bearer " + b.token
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

type callRequest struct {
	Service string            `json:"service"`
	Method  string            `json:"method"`
	Args    []json.RawMessage `json:"args"`
}

type callResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (b *Bridge) handler() http.Handler {
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Only the app's own processes call the bridge. A request from a web page (it carries an Origin) or one
			// addressed to a name other than the loopback address (DNS rebinding) is refused before the token is looked at.
			if r.Header.Get("Origin") != "" || !strings.HasPrefix(r.Host, "127.0.0.1:") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !b.authorised(r) {
				http.Error(w, "unauthorised", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	version := Version // the version this main process started as
	mux.HandleFunc("/ping", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(versionHeader, version)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/appcall", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req callRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		res, err := b.DispatchApp(req.Service, req.Method, req.Args)
		resp := callResponse{Result: res}
		if err != nil {
			resp = callResponse{Error: err.Error()}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	mux.HandleFunc("/menu", guard(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req) != nil || !knownMenuAction(req.Action) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if b.onMenu != nil {
			b.onMenu(req.Action)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	// long poll: the terminal events of the sessions a window process opened, after the given sequence number
	mux.HandleFunc("/events", guard(func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		if after < 0 { // "where are you now": answer at once, nothing to wait for
			b.evMu.Lock()
			last := b.seq
			b.evMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []BridgedEvent{}, "last": last})
			return
		}
		evs, last := b.eventsAfter(r.Context(), after)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": evs, "last": last})
	}))
	return mux
}

// Start listens on a loopback port and writes the connection file into dir.
func (b *Bridge) Start(dir string) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	b.token = hex.EncodeToString(raw)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b.ln = ln
	info, _ := json.Marshal(bridgeInfo{Port: ln.Addr().(*net.TCPAddr).Port, Token: b.token})
	b.file = filepath.Join(dir, bridgeFile)
	tmp := b.file + ".tmp"
	if err := os.WriteFile(tmp, info, 0o600); err != nil {
		ln.Close()
		return err
	}
	if err := os.Rename(tmp, b.file); err != nil {
		ln.Close()
		return err
	}
	b.srv = &http.Server{Handler: b.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return nil
}

// Close stops the server and removes the connection file.
func (b *Bridge) Close() {
	if b.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = b.srv.Shutdown(ctx)
		_ = b.srv.Close()
	}
	if b.file != "" {
		_ = os.Remove(b.file)
	}
}
