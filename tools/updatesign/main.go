// Command updatesign makes and uses the key that releases are signed with, so the app can check an update before it installs it.
//
//	updatesign keygen <key file>          make a key; the file holds the private key (keep it secret and backed up), the public key is printed
//	updatesign sign <key file> <file>     write <file>.sig, the signature of <file>
//	updatesign verify <public key> <file> check <file> against <file>.sig
//
// The signing key can also come from the environment (UPDATE_SIGNING_KEY, the content of the key file) instead of a file: use "-"
// as the key file.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"vutty/internal/updater"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "updatesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: updatesign keygen <key file> | sign <key file> <file> | verify <public key> <file>")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return fmt.Errorf("usage: updatesign keygen <key file>")
		}
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		f, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // never overwrites a key
		if err != nil {
			return err
		}
		if _, err := f.WriteString(base64.StdEncoding.EncodeToString(seed) + "\n"); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		fmt.Println("private key written to", args[1], "(mode 0600): back it up, and never commit it")
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pub))
		return nil
	case "sign":
		if len(args) != 3 {
			return fmt.Errorf("usage: updatesign sign <key file> <file>")
		}
		seed, err := readSeed(args[1])
		if err != nil {
			return err
		}
		msg, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		sig, err := updater.Sign(seed, msg)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[2]+".sig", []byte(sig), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", args[2]+".sig")
		return nil
	case "verify":
		if len(args) != 3 {
			return fmt.Errorf("usage: updatesign verify <public key> <file>")
		}
		pub, err := updater.ParsePublicKey(args[1])
		if err != nil {
			return err
		}
		msg, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(args[2] + ".sig")
		if err != nil {
			return err
		}
		sig, err := updater.DecodeSignature(raw)
		if err != nil {
			return err
		}
		if !updater.Verify(pub, msg, sig) {
			return fmt.Errorf("the signature does not match")
		}
		fmt.Println("signature ok")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func readSeed(path string) ([]byte, error) {
	var text string
	if path == "-" {
		text = os.Getenv("UPDATE_SIGNING_KEY")
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("UPDATE_SIGNING_KEY is empty")
		}
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text = string(b)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("the key is not a signing key made by updatesign keygen")
	}
	return seed, nil
}
