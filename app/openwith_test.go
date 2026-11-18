package app

import "testing"

func TestOpenWithAppsRefusesProgramFiles(t *testing.T) {
	apps, err := NewSystemService().OpenWithApps("run.sh")
	if err != nil || len(apps) != 0 {
		t.Fatalf("a file that runs programs lists no app: %v %v", apps, err)
	}
}

func TestOpenWithAppsListsNamedApps(t *testing.T) {
	apps, err := NewSystemService().OpenWithApps("application-2026-11-18.log")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) > maxOpenApps {
		t.Fatalf("too many apps: %d", len(apps))
	}
	for _, a := range apps {
		if a.Name == "" || a.Path == "" {
			t.Fatalf("an app needs a name and a path: %+v", a)
		}
	}
}

func TestSplitLines(t *testing.T) {
	if got := splitLines("a\n\n b \n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %q", got)
	}
}
