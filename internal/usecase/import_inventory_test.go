package usecase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vutty/internal/domain"
)

const inventoryAWS = `{"Reservations":[{"Instances":[
 {"InstanceId":"i-1","State":{"Name":"running"},"PublicIpAddress":"54.1.2.3","Tags":[{"Key":"Name","Value":"web-1"},{"Key":"Environment","Value":"prod"}]},
 {"InstanceId":"i-2","State":{"Name":"running"},"PrivateIpAddress":"10.0.0.2","Tags":[{"Key":"Name","Value":"db-1"}]}
]}]}`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportInventoryAddsHostsAndAGroupAndSaysWhatItRead(t *testing.T) {
	h := newHarness(t).unlocked(t)
	rep, err := h.app.ImportExp.ImportInventory(writeTemp(t, "aws.json", inventoryAWS), "skip", InventoryImportOptions{User: "ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.HostsAdded != 2 || rep.GroupsAdded != 1 || !strings.Contains(rep.Format, "AWS EC2") {
		t.Fatalf("%+v", rep)
	}
	hosts := h.app.Host.ListHosts(domain.HostFilter{})
	by := map[string]domain.Host{}
	for _, x := range hosts {
		by[x.Label] = x
	}
	if w := by["web-1"]; w.Hostname != "54.1.2.3" || w.Username != "ubuntu" || w.Env != domain.EnvProd || w.Port != 22 || w.GroupID == "" || w.KeepaliveSec != 30 {
		t.Fatalf("%+v", w)
	}
	if by["db-1"].Hostname != "10.0.0.2" {
		t.Fatalf("%+v", by["db-1"])
	}
	groups := h.core.Store.Snapshot().Groups
	if len(groups) != 1 || groups[0].Name != "aws" {
		t.Fatalf("%+v", groups)
	}
}

func TestImportInventoryAgainFollowsTheConflictChoice(t *testing.T) {
	h := newHarness(t).unlocked(t)
	file := writeTemp(t, "aws.json", inventoryAWS)
	if _, err := h.app.ImportExp.ImportInventory(file, "skip", InventoryImportOptions{}); err != nil {
		t.Fatal(err)
	}
	rep, err := h.app.ImportExp.ImportInventory(file, "skip", InventoryImportOptions{})
	if err != nil || rep.HostsAdded != 0 || len(rep.Skipped) != 2 || rep.GroupsAdded != 0 {
		t.Fatalf("skip: %+v %v", rep, err)
	}
	rep, _ = h.app.ImportExp.ImportInventory(file, "duplicate", InventoryImportOptions{})
	if rep.HostsAdded != 2 || len(h.app.Host.ListHosts(domain.HostFilter{})) != 4 {
		t.Fatalf("duplicate: %+v", rep)
	}
	// overwrite: a changed address replaces the saved one, and the host keeps its id
	before := h.app.Host.ListHosts(domain.HostFilter{Query: "db-1"})[0]
	changed := strings.Replace(inventoryAWS, "10.0.0.2", "10.9.9.9", 1)
	rep, err = h.app.ImportExp.ImportInventory(writeTemp(t, "aws2.json", changed), "overwrite", InventoryImportOptions{Address: "private"})
	if err != nil || rep.HostsAdded == 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	for _, x := range h.app.Host.ListHosts(domain.HostFilter{Query: "db-1"}) {
		if x.ID == before.ID && x.Hostname != "10.9.9.9" {
			t.Fatalf("not overwritten: %+v", x)
		}
	}
}

func TestImportInventoryRefusesWhatItCannotRead(t *testing.T) {
	h := newHarness(t).unlocked(t)
	s := h.app.ImportExp
	good := writeTemp(t, "a.json", inventoryAWS)
	for name, call := range map[string]func() error{
		"missing file": func() error {
			_, e := s.ImportInventory(filepath.Join(t.TempDir(), "nope"), "skip", InventoryImportOptions{})
			return e
		},
		"bad conflict": func() error { _, e := s.ImportInventory(good, "merge", InventoryImportOptions{}); return e },
		"bad address": func() error {
			_, e := s.ImportInventory(good, "skip", InventoryImportOptions{Address: "both"})
			return e
		},
		"user with space": func() error { _, e := s.ImportInventory(good, "skip", InventoryImportOptions{User: "a b"}); return e },
		"group newline":   func() error { _, e := s.ImportInventory(good, "skip", InventoryImportOptions{Group: "a\nb"}); return e },
		"not a listing": func() error {
			_, e := s.ImportInventory(writeTemp(t, "x.json", `{"hello":1}`), "skip", InventoryImportOptions{})
			return e
		},
		"too large": func() error {
			_, e := s.ImportInventory(writeTemp(t, "big.csv", "hostname\n"+strings.Repeat("a.example.com\n", (maxInventoryFile/14)+10)), "skip", InventoryImportOptions{})
			return e
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if n := len(h.app.Host.ListHosts(domain.HostFilter{})); n != 0 {
		t.Fatalf("a refused import left %d hosts", n)
	}
}

func TestImportInventoryWorksWithTheVaultLocked(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.app.Vault.Lock() // a listing carries no secret: nothing needs the vault
	if rep, err := h.app.ImportExp.ImportInventory(writeTemp(t, "a.csv", "hostname,label\n10.1.1.1,one\n"), "skip", InventoryImportOptions{}); err != nil || rep.HostsAdded != 1 || rep.Format != "CSV" {
		t.Fatalf("%+v %v", rep, err)
	}
}
