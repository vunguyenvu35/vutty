package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ---- client side (the Settings process) ----

// BridgeClient talks to the main process.
type BridgeClient struct {
	base  string
	token string
	hc    *http.Client
}

// ConnectBridge reads the connection file written by the main process.
func ConnectBridge(dir string) (*BridgeClient, error) {
	raw, err := os.ReadFile(filepath.Join(dir, bridgeFile))
	if err != nil {
		return nil, errors.New("Vutty is not running: open Settings from the main window")
	}
	var info bridgeInfo
	if err := json.Unmarshal(raw, &info); err != nil || info.Port == 0 || info.Token == "" {
		return nil, errors.New("the settings connection file is invalid")
	}
	return &BridgeClient{base: fmt.Sprintf("http://127.0.0.1:%d", info.Port), token: info.Token, hc: &http.Client{Timeout: 5 * time.Minute}}, nil
}

func (c *BridgeClient) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.hc.Do(req)
}

// versionHeader carries the app's version on /ping, so a window of another version can tell.
const versionHeader = "X-Vutty-Version"

// Ping reports whether the main process still answers, and that it is the same version as this process.
func (c *BridgeClient) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, "/ping", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errors.New(resp.Status)
	}
	// A window left over from another version of the app (the app was updated while it was open) must not keep
	// talking to the new main process: it would show, and ask for, things the two versions do not agree on.
	if v := resp.Header.Get(versionHeader); v != Version {
		return fmt.Errorf("this window is Vutty %s but the app is %s: close it and open it again", Version, v)
	}
	return nil
}

// Events blocks until the main process has terminal events after seq (or ~20 s pass) and returns them with
// the newest sequence number.
func (c *BridgeClient) Events(ctx context.Context, seq int) ([]BridgedEvent, int, error) {
	resp, err := c.do(ctx, http.MethodGet, "/events?after="+strconv.Itoa(seq), nil)
	if err != nil {
		return nil, seq, err
	}
	defer resp.Body.Close()
	var out struct {
		Events []BridgedEvent `json:"events"`
		Last   int            `json:"last"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, seq, err
	}
	return out.Events, out.Last, nil
}

// MenuAction asks the main window to do a menu action.
func (c *BridgeClient) MenuAction(action string) error {
	body, _ := json.Marshal(map[string]string{"action": action})
	resp, err := c.do(context.Background(), http.MethodPost, "/menu", body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errors.New(resp.Status)
	}
	return nil
}

// knownMenuAction is true for the actions of the menu bar (see MenuActions): nothing else is passed on.
func knownMenuAction(a string) bool {
	for _, k := range MenuActions {
		if a == k {
			return true
		}
	}
	return false
}

// CallApp is Call for an app window: any method of any use case service.
func (c *BridgeClient) CallApp(service, method, argsJSON string) (string, error) {
	var args []json.RawMessage
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
	}
	body, _ := json.Marshal(callRequest{Service: service, Method: method, Args: args})
	resp, err := c.do(context.Background(), http.MethodPost, "/appcall", body)
	if err != nil {
		return "", errors.New("Vutty is not running")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the main window refused the request (%s)", resp.Status)
	}
	var out callResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	return string(out.Result), nil
}
