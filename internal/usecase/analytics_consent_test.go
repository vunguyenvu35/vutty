package usecase

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyticsIsOffUntilTheUserAnswersYes(t *testing.T) {
	h := newHarness(t)
	s := h.app.Settings
	d := s.Get()
	if d.AnalyticsConsent != "" || d.AnalyticsID != "" {
		t.Fatalf("a new installation must not have agreed or have an id: %+v", d)
	}

	// Saving other settings, with the answer left empty, neither agrees nor makes an id.
	in := s.Get()
	in.Zoom = 110
	out, err := s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsConsent != "" || out.AnalyticsID != "" {
		t.Fatalf("an unrelated save changed the answer: %+v", out)
	}

	// Yes makes a random id and keeps it on later saves.
	in = s.Get()
	in.AnalyticsConsent = "yes"
	in.AnalyticsID = "chosen-by-the-ui" // the UI cannot choose the id
	out, err = s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	id := out.AnalyticsID
	if out.AnalyticsConsent != "yes" || len(id) != 36 || id == "chosen-by-the-ui" {
		t.Fatalf("after yes: %+v", out)
	}
	in = s.Get()
	in.Zoom = 100
	in.AnalyticsConsent = "" // a window that has not loaded the settings yet
	out, err = s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsConsent != "yes" || out.AnalyticsID != id {
		t.Fatalf("an empty answer must keep the choice: %+v", out)
	}

	// No forgets the id; a later yes is a new, unrelated installation.
	in = s.Get()
	in.AnalyticsConsent = "no"
	out, err = s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsConsent != "no" || out.AnalyticsID != "" {
		t.Fatalf("after no: %+v", out)
	}
	in = s.Get()
	in.AnalyticsConsent = "yes"
	out, err = s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsID == "" || out.AnalyticsID == id {
		t.Fatalf("a new yes must make a new id: %+v", out)
	}

	// An answer that is not yes or no changes nothing.
	in = s.Get()
	in.AnalyticsConsent = "maybe"
	out, err = s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsConsent != "yes" {
		t.Fatalf("unknown answer: %+v", out)
	}
}

func TestAnalyticsAnswerIsSavedAndNotSynced(t *testing.T) {
	dir := t.TempDir()
	c, err := testCore(t, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSettingsService(c)
	in := s.Get()
	in.AnalyticsConsent = "yes"
	out, err := s.Save(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "data", "settings.json"))
	if err != nil || !strings.Contains(string(b), `"analytics_consent": "yes"`) || !strings.Contains(string(b), out.AnalyticsID) {
		t.Fatalf("settings.json: %v %s", err, b)
	}
	// What goes to the other machines is only the preference groups: the answer and the id are not among them.
	for _, g := range prefGroups {
		if j := marshalForTest(t, g.get(out)); strings.Contains(j, "analytics") || strings.Contains(j, out.AnalyticsID) {
			t.Fatalf("preference group %s carries the analytics answer: %s", g.id, j)
		}
	}
}

func TestResetSettingsAsksAgainAboutStatistics(t *testing.T) {
	h := newHarness(t)
	in := h.app.Settings.Get()
	in.AnalyticsConsent = "yes"
	if _, err := h.app.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := h.app.Settings.ResetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if out.AnalyticsConsent != "" || out.AnalyticsID != "" {
		t.Fatalf("a reset must stop sending until the user answers again: %+v", out)
	}
}

func marshalForTest(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
