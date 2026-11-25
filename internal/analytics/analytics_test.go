package analytics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPostsOnlyTheFixedFields(t *testing.T) {
	var got struct {
		path, mid, secret, ctype string
		body                     map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.mid, got.secret, got.ctype = r.URL.Path, r.URL.Query().Get("measurement_id"), r.URL.Query().Get("api_secret"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), URL: srv.URL + "/mp/collect", MeasurementID: "G-TEST", APISecret: "s3cret"}
	err := c.Send(context.Background(), Event{Name: "app_open", ClientID: "11111111-2222-3333-4444-555555555555", App: "vutty", Version: "1.6.10", OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/mp/collect" || got.mid != "G-TEST" || got.secret != "s3cret" || got.ctype != "application/json" {
		t.Fatalf("request = %+v", got)
	}
	if got.body["client_id"] != "11111111-2222-3333-4444-555555555555" || got.body["non_personalized_ads"] != true {
		t.Fatalf("body = %v", got.body)
	}
	events := got.body["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
	ev := events[0].(map[string]any)
	if ev["name"] != "app_open" {
		t.Fatalf("event = %v", ev)
	}
	params := ev["params"].(map[string]any)
	want := map[string]any{"app_name": "vutty", "app_version": "1.6.10", "os": "darwin", "arch": "arm64", "engagement_time_msec": float64(1)}
	if len(params) != len(want) {
		t.Fatalf("params = %v, want exactly %v", params, want)
	}
	for k, v := range want {
		if params[k] != v {
			t.Fatalf("param %s = %v, want %v", k, params[k], v)
		}
	}
}

func TestSendDoesNothingWithoutAnIdOrAClient(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), URL: srv.URL, MeasurementID: "G-TEST", APISecret: "x"}
	if err := c.Send(context.Background(), Event{Name: "app_open"}); err != nil { // the user has no id: they did not agree
		t.Fatal(err)
	}
	var none *Client
	if err := none.Send(context.Background(), Event{Name: "app_open", ClientID: "id"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("an event was sent without an id or a client")
	}
}

func TestSendReportsAnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), URL: srv.URL, MeasurementID: "G-TEST", APISecret: "x"}
	if err := c.Send(context.Background(), Event{Name: "app_open", ClientID: "id"}); err == nil {
		t.Fatal("a 403 must be an error")
	}
}

func TestBuildWithoutAStreamIsDisabled(t *testing.T) {
	defer func(m, s string) { MeasurementID, APISecret = m, s }(MeasurementID, APISecret)
	MeasurementID, APISecret = "", ""
	if Enabled() || New() != nil {
		t.Fatal("a build without a measurement id must not send anything")
	}
	MeasurementID, APISecret = "G-X", ""
	if Enabled() {
		t.Fatal("an id without a secret is not enough")
	}
	MeasurementID, APISecret = "G-X", "s"
	if !Enabled() || New() == nil {
		t.Fatal("a build with both must be enabled")
	}
}
