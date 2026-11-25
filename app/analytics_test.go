package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vutty/internal/analytics"
)

func withStream(t *testing.T) {
	t.Helper()
	m, s := analytics.MeasurementID, analytics.APISecret
	analytics.MeasurementID, analytics.APISecret = "G-TEST", "secret"
	t.Cleanup(func() { analytics.MeasurementID, analytics.APISecret = m, s })
}

func TestReportOpenSendsOnlyAfterYes(t *testing.T) {
	withStream(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	t.Setenv("VUTTY_ANALYTICS_URL", srv.URL)

	a := newTestApp(t)
	a.reportOpen(context.Background())
	if hits.Load() != 0 {
		t.Fatal("sent before the user was asked")
	}

	in := a.Settings.Get()
	in.AnalyticsConsent = "no"
	if _, err := a.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	a.reportOpen(context.Background())
	if hits.Load() != 0 {
		t.Fatal("sent after the user said no")
	}

	in.AnalyticsConsent = "yes"
	if _, err := a.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	a.reportOpen(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("hits after yes = %d, want 1", hits.Load())
	}
}

func TestReportOpenDoesNothingInABuildWithoutAStream(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	t.Setenv("VUTTY_ANALYTICS_URL", srv.URL)
	a := newTestApp(t)
	in := a.Settings.Get()
	in.AnalyticsConsent = "yes"
	if _, err := a.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	a.reportOpen(context.Background())
	if hits.Load() != 0 {
		t.Fatal("a build without a stream sent something")
	}
	if a.System.Info().Analytics {
		t.Fatal("Info must say the build has no stream")
	}
}
