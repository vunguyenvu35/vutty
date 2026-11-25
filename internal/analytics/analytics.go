// Package analytics sends the one anonymous "the app was opened" event of Vutty to Google Analytics 4, through the
// Measurement Protocol, and only for a user who said yes (Settings → General → Share anonymous usage statistics).
//
// It is built so that nothing can be sent by accident:
//   - a build without a measurement id and an API secret (a developer build, a fork) has Enabled() == false and Send
//     does nothing;
//   - the caller passes the random id the user got when they said yes; there is no other identifier;
//   - the event has a fixed set of fields (see Event), so a new field is a visible change in this file.
//
// MeasurementID and APISecret are set at build time, one pair per app (one GA4 data stream per app):
//
//	-ldflags "-X vutty/internal/analytics.MeasurementID=G-XXXXXXXXXX -X vutty/internal/analytics.APISecret=..."
//
// The API secret of the Measurement Protocol is not a credential: it ships inside the binary and only lets someone add
// events to the stream. See docs/development/analytics.md.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Set at build time (see the package comment).
var (
	MeasurementID string
	APISecret     string
)

// Endpoint is where events go. Tests point it at a local server; GA4 also has /debug/mp/collect, which answers with
// what is wrong with an event instead of storing it.
const Endpoint = "https://www.google-analytics.com/mp/collect"

// Enabled reports whether this build knows where to send events.
func Enabled() bool { return MeasurementID != "" && APISecret != "" }

// Event is everything that is ever sent. Nothing about hosts, keys, files, commands or the user is in it.
type Event struct {
	Name     string // "app_open"
	ClientID string // the random id of this installation, made when the user said yes
	App      string // "vutty": which app of the owner sent it
	Version  string // the release, such as 1.6.10
	OS       string // runtime.GOOS
	Arch     string // runtime.GOARCH
}

// Client sends events. The zero value is not usable: use New.
type Client struct {
	HTTP          *http.Client
	URL           string
	MeasurementID string
	APISecret     string
}

// New returns a client for this build's stream, or nil when the build has none.
func New() *Client {
	if !Enabled() {
		return nil
	}
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}, URL: Endpoint, MeasurementID: MeasurementID, APISecret: APISecret}
}

type payload struct {
	ClientID          string         `json:"client_id"`
	NonPersonalizedAd bool           `json:"non_personalized_ads"`
	Events            []payloadEvent `json:"events"`
}

type payloadEvent struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}

// Send posts one event. A nil client, or an event without a client id, sends nothing and is not an error: the
// caller does not have to check whether the user agreed.
func (c *Client) Send(ctx context.Context, e Event) error {
	if c == nil || e.ClientID == "" || e.Name == "" {
		return nil
	}
	body, err := json.Marshal(payload{
		ClientID:          e.ClientID,
		NonPersonalizedAd: true,
		Events: []payloadEvent{{Name: e.Name, Params: map[string]any{
			"app_name":    e.App,
			"app_version": e.Version,
			"os":          e.OS,
			"arch":        e.Arch,
			// GA4 counts a user as active only when an event carries some engagement time.
			"engagement_time_msec": 1,
		}}},
	})
	if err != nil {
		return err
	}
	q := url.Values{"measurement_id": {c.MeasurementID}, "api_secret": {c.APISecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("analytics: %s", resp.Status)
	}
	return nil
}
