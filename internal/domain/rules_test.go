package domain

import (
	"testing"
	"time"
)

func TestNormalizeHostDefaultsAndRejects(t *testing.T) {
	h := Host{Hostname: "10.0.0.1"}
	if err := NormalizeHost(&h); err != nil || h.Label != "10.0.0.1" || h.Port != 22 || h.KeepaliveSec != 30 {
		t.Fatalf("%v %+v", err, h)
	}
	for name, bad := range map[string]Host{
		"empty":     {},
		"newline":   {Hostname: "a\nProxyCommand x"},
		"space":     {Hostname: "a b"},
		"user ctrl": {Hostname: "a", Username: "u\r"},
		"port":      {Hostname: "a", Port: 70000},
		"env":       {Hostname: "a", Env: "qa"},
	} {
		if err := NormalizeHost(&bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNormalizeForwardSafeBindDefaults(t *testing.T) {
	f := PortForward{Type: ForwardDynamic}
	if err := NormalizeForward(&f); err != nil || f.BindAddr != "127.0.0.1" {
		t.Fatalf("%v %+v", err, f)
	}
	pub := PortForward{Type: ForwardDynamic, BindAddr: "0.0.0.0"}
	if NormalizeForward(&pub) == nil {
		t.Fatal("public bind accepted without opt-in")
	}
	pub.AllowPublicBind = true
	if err := NormalizeForward(&pub); err != nil {
		t.Fatal(err)
	}
	if NormalizeForward(&PortForward{Type: ForwardLocal}) == nil {
		t.Fatal("local forward without destination accepted")
	}
	if NormalizeForward(&PortForward{Type: "x"}) == nil {
		t.Fatal("unknown type accepted")
	}
}

func TestIsLoopback(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "::1", "localhost", "LOCALHOST"} {
		if !IsLoopback(a) {
			t.Errorf("%s should be loopback", a)
		}
	}
	for _, a := range []string{"0.0.0.0", "192.168.1.1", "example.com", ""} {
		if IsLoopback(a) {
			t.Errorf("%s should not be loopback", a)
		}
	}
}

func TestCheckJumpChain(t *testing.T) {
	hosts := map[string]Host{
		"a": {Meta: Meta{ID: "a"}}, "b": {Meta: Meta{ID: "b"}, JumpHostID: "a"}, "c": {Meta: Meta{ID: "c"}, JumpHostID: "b"},
	}
	look := func(id string) (Host, bool) { h, ok := hosts[id]; return h, ok }
	if err := CheckJumpChain("d", "c", look); err != nil {
		t.Fatal(err)
	}
	if CheckJumpChain("a", "c", look) == nil {
		t.Fatal("loop accepted")
	}
	if CheckJumpChain("a", "a", look) == nil {
		t.Fatal("self jump accepted")
	}
	if CheckJumpChain("d", "zzz", look) == nil {
		t.Fatal("missing jump host accepted")
	}
}

func TestNormalizeSnippet(t *testing.T) {
	s := Snippet{Label: "  Docker clean  ", Script: "docker system prune -af\n"}
	if err := NormalizeSnippet(&s); err != nil || s.Label != "Docker clean" || s.Script != "docker system prune -af\n" {
		t.Fatalf("%v %+v", err, s)
	}
	big := make([]byte, MaxSnippetBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	for name, bad := range map[string]Snippet{
		"no label":    {Script: "ls"},
		"blank label": {Label: "   ", Script: "ls"},
		"no script":   {Label: "x"},
		"blank body":  {Label: "x", Script: " \n\t"},
		"NUL byte":    {Label: "x", Script: "ls\x00rm"},
		"too large":   {Label: "x", Script: string(big)},
	} {
		if NormalizeSnippet(&bad) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNormalizeCluster(t *testing.T) {
	c := Cluster{Label: "  prod  ", Env: EnvProd}
	if err := NormalizeCluster(&c); err != nil || c.Label != "prod" {
		t.Fatalf("%+v %v", c, err)
	}
	for name, bad := range map[string]Cluster{"empty": {}, "long": {Label: string(make([]byte, 101))}, "env": {Label: "x", Env: "moon"}} {
		if err := NormalizeCluster(&bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestMergeKeepsTheNewerClusterAndTheOneOnlyOneSideHas(t *testing.T) {
	old, now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	local := Bundle{Data: Data{Clusters: []Cluster{{Meta: Meta{ID: "a", UpdatedAt: old}, Label: "old name"}, {Meta: Meta{ID: "mine", UpdatedAt: old}, Label: "mine"}}}}
	remote := Bundle{Data: Data{Clusters: []Cluster{{Meta: Meta{ID: "a", UpdatedAt: now}, Label: "new name"}, {Meta: Meta{ID: "theirs", UpdatedAt: old}, Label: "theirs"}}}}
	got := Merge(local, remote).Data.Clusters
	names := map[string]string{}
	for _, c := range got {
		names[c.ID] = c.Label
	}
	if len(got) != 3 || names["a"] != "new name" || names["mine"] != "mine" || names["theirs"] != "theirs" {
		t.Fatalf("merge = %+v", got)
	}
	purged := PurgeTombstones(Bundle{Data: Data{Clusters: []Cluster{{Meta: Meta{ID: "x", UpdatedAt: old, DeletedAt: &old}}, {Meta: Meta{ID: "y"}}}}}, now)
	if len(purged.Data.Clusters) != 1 || purged.Data.Clusters[0].ID != "y" {
		t.Fatalf("purge = %+v", purged.Data.Clusters)
	}
}
