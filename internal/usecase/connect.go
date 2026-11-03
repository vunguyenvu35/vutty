package usecase

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"vutty/internal/domain"
	"vutty/internal/sshx"
	"vutty/internal/vault"
)

// hopFor turns a host into a dialable hop, decrypting its identity inside Go.
func (c *Core) hopFor(h domain.Host) (sshx.Hop, error) {
	hop := sshx.Hop{Label: h.Label, Hostname: h.Hostname, Port: h.Port, Username: h.Username}
	if h.IdentityID == "" {
		a, err := sshx.AgentAuth()
		if err != nil {
			return hop, fmt.Errorf("host %q has no identity and no ssh-agent is available", h.Label)
		}
		hop.Auth = []ssh.AuthMethod{a}
		return hop, nil
	}
	id, err := c.Store.GetIdentity(h.IdentityID)
	if err != nil {
		return hop, fmt.Errorf("identity of %q: %w", h.Label, err)
	}
	if hop.Username == "" {
		hop.Username = id.Username
	}
	switch id.AuthType {
	case domain.AuthAgent:
		a, err := sshx.AgentAuth()
		if err != nil {
			return hop, err
		}
		hop.Auth = []ssh.AuthMethod{a}
	case domain.AuthPassword:
		pw, err := c.Vault.GetSecret(id.SecretRef)
		if err != nil {
			return hop, secretErr(id.Name, err)
		}
		hop.Auth, hop.Password = sshx.SavedPassword(string(pw)), string(pw)
	case domain.AuthKey:
		pem, err := c.Vault.GetSecret(id.SecretRef)
		if err != nil {
			return hop, secretErr(id.Name, err)
		}
		signer, err := vault.ParsePrivateKey(pem, nil)
		if err != nil {
			return hop, fmt.Errorf("identity %q: %w", id.Name, err)
		}
		if id.Certificate != "" {
			cs, err := certSigner(id.Certificate, signer)
			if err != nil {
				return hop, fmt.Errorf("identity %q: %w", id.Name, err)
			}
			signer = cs
		}
		hop.Auth = []ssh.AuthMethod{sshx.KeyAuth(signer)}
	default:
		return hop, fmt.Errorf("unknown auth type %q", id.AuthType)
	}
	if hop.Username == "" {
		return hop, fmt.Errorf("no username for host %q", h.Label)
	}
	return hop, nil
}

// secretErr explains why a saved secret could not be used. A secret that the key cannot open was sealed with another key
// (the raw reason, "message authentication failed", says nothing about what to do).
func secretErr(name string, err error) error {
	if strings.Contains(err.Error(), "message authentication failed") {
		return fmt.Errorf("the saved secret of %q cannot be read with this vault's key (it was sealed with another key): open Keychain, edit %q and enter it again", name, name)
	}
	return vaultErr(err)
}

func vaultErr(err error) error {
	if errors.Is(err, vault.ErrLocked) {
		return errNotUnlocked
	}
	return err
}

// progress tells the connecting screen how far a connection got: term:progress:<sid> with the stage
// "connecting" (dialling, key exchange, login), then "authenticated"; term:ready follows the opened shell.
func (c *Core) progress(sid, stage string) {
	c.emit("term:progress:"+sid, map[string]any{"stage": stage})
}

// hostKeyTimeout bounds how long a connection waits for the user to answer a fingerprint prompt.
const hostKeyTimeout = 2 * time.Minute

// authTimeout bounds how long a connection waits for a verification code: long enough to find the phone.
const authTimeout = 3 * time.Minute

// askUser is how a connection puts the questions of a server (a verification code, a Duo choice…) to the user: the
// term:auth:<promptKey> event carries them, AnswerAuth brings the answers back. What the user types is never kept or logged.
func (c *Core) askUser(promptKey string, abort <-chan struct{}) sshx.Asker {
	return func(ch sshx.Challenge) ([]string, error) {
		payload := map[string]any{"host": ch.Host, "name": ch.Name, "instruction": ch.Instruction, "prompts": ch.Prompts}
		reply := c.authPrompts.ask(promptKey, payload)
		defer c.authPrompts.clear(promptKey)
		c.emit("term:auth:"+promptKey, payload)
		select {
		case a := <-reply:
			if !a.ok {
				return nil, errors.New("sign-in cancelled")
			}
			if len(a.answers) != len(ch.Prompts) {
				return nil, errors.New("the answers do not match the questions")
			}
			return a.answers, nil
		case <-abort:
			return nil, errors.New("cancelled")
		case <-time.After(authTimeout):
			return nil, errors.New("timed out waiting for the verification code")
		}
	}
}

// connect dials the host (through its jump chain). Unknown host keys raise the
// term:hostkey:<promptKey> event and block until TrustHostKey answers.
func (c *Core) connect(hostID, promptKey string, abort <-chan struct{}) (*sshx.Client, domain.Host, error) {
	chain, err := c.Store.JumpChain(hostID)
	if err != nil {
		return nil, domain.Host{}, err
	}
	target := chain[len(chain)-1]
	hops := make([]sshx.Hop, len(chain))
	for i, h := range chain {
		if hops[i], err = c.hopFor(h); err != nil {
			return nil, target, err
		}
	}
	c.progress(promptKey, "connecting")
	cl, err := c.dialHops(hops, promptKey, abort)
	if err == nil {
		c.progress(promptKey, "authenticated") // the key exchange and the login are done
	}
	if errors.Is(err, sshx.ErrHostKeyChanged) {
		c.record(domain.ActHostKey, target.ID, target.Label, "HOST KEY CHANGED: connection blocked (possible man-in-the-middle)", false)
	}
	return cl, target, err
}

// dialHops dials a prepared chain. Unknown host keys raise term:hostkey:<promptKey> and wait for
// TrustHostKey; known keys are checked against known_hosts as usual.
func (c *Core) dialHops(hops []sshx.Hop, promptKey string, abort <-chan struct{}) (*sshx.Client, error) {
	hops = append([]sshx.Hop(nil), hops...)
	for i := range hops {
		hops[i].Ask = c.askUser(promptKey, abort)
	}
	d := &sshx.Dialer{Timeout: c.Dialer.Timeout, HostKeys: &sshx.HostKeyChecker{
		Path: c.Dialer.HostKeys.Path,
		Prompt: func(host string, key ssh.PublicKey) (bool, error) {
			fp := sshx.Fingerprint(key)
			payload := map[string]any{"host": host, "fingerprint": fp, "key_type": key.Type()}
			ch := c.prompts.ask(promptKey, fp, payload)
			defer c.prompts.clear(promptKey)
			c.emit("term:hostkey:"+promptKey, payload)
			select {
			case ok := <-ch:
				if ok {
					c.record(domain.ActHostKey, "", host, "Trusted the host key "+fp, true)
				} else {
					c.record(domain.ActHostKey, "", host, "Did not trust the host key "+fp, false)
				}
				return ok, nil
			case <-abort:
				return false, errors.New("cancelled")
			case <-time.After(hostKeyTimeout):
				return false, errors.New("timed out waiting for host key confirmation")
			}
		},
	}}
	return d.Dial(hops)
}

// certSigner makes a signer that presents the certificate along with the key.
func certSigner(line string, signer ssh.Signer) (ssh.Signer, error) {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, fmt.Errorf("stored certificate: %w", err)
	}
	cert, ok := parsed.(*ssh.Certificate)
	if !ok {
		return nil, errors.New("stored certificate is not a certificate")
	}
	return ssh.NewCertSigner(cert, signer)
}

// connectQuiet dials a host (through its jump chain) for something nobody is watching, such as running one command on many
// hosts. It never asks anything: a host key that is not trusted yet is refused, and a server that wants a verification code
// fails with a message that says so. Open the host once in a terminal tab to trust its key.
func (c *Core) connectQuiet(hostID string) (*sshx.Client, domain.Host, error) {
	chain, err := c.Store.JumpChain(hostID)
	if err != nil {
		return nil, domain.Host{}, err
	}
	target := chain[len(chain)-1]
	hops := make([]sshx.Hop, len(chain))
	for i, h := range chain {
		if hops[i], err = c.hopFor(h); err != nil {
			return nil, target, err
		}
	}
	d := &sshx.Dialer{Timeout: c.Dialer.Timeout, HostKeys: &sshx.HostKeyChecker{Path: c.Dialer.HostKeys.Path}} // no Prompt: unknown keys are refused
	cl, err := d.Dial(hops)
	if errors.Is(err, sshx.ErrHostKeyRejected) {
		return nil, target, fmt.Errorf("the host key of %q is not trusted yet: open the host once in a terminal tab and trust it", target.Label)
	}
	if errors.Is(err, sshx.ErrHostKeyChanged) {
		c.record(domain.ActHostKey, target.ID, target.Label, "HOST KEY CHANGED: connection blocked (possible man-in-the-middle)", false)
	}
	return cl, target, err
}

// enableAgentForwarding lets the host use the user's keys to sign in to other machines for as long as the connection lasts
// (ssh -A). It forwards the running ssh-agent when there is one; without it, the key of this host's own identity, which lives
// in the vault, from a keyring that exists only for this connection. When neither works the terminal opens without it and the
// Logs say why.
func (c *Core) enableAgentForwarding(cl *sshx.Client, host domain.Host) {
	fail := func(err error) {
		c.record(domain.ActConnect, host.ID, host.Label, "Agent forwarding is on for this host but could not be set up: "+err.Error(), false)
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if err := cl.ForwardAgentSocket(sock); err != nil {
			fail(err)
		}
		return
	}
	ag, err := c.vaultAgent(host)
	if err != nil {
		fail(err)
		return
	}
	if err := cl.ForwardAgentKeys(ag); err != nil {
		fail(err)
	}
}

// vaultAgent is an agent in this process that holds the saved key of the host's identity (and its certificate, if it has one).
func (c *Core) vaultAgent(host domain.Host) (agent.Agent, error) {
	if host.IdentityID == "" {
		return nil, errors.New("there is no ssh-agent running and the host has no saved key to offer")
	}
	id, err := c.Store.GetIdentity(host.IdentityID)
	if err != nil {
		return nil, err
	}
	if id.AuthType != domain.AuthKey {
		return nil, errors.New("there is no ssh-agent running, and the identity of this host is not a key")
	}
	pem, err := c.Vault.GetSecret(id.SecretRef)
	if err != nil {
		return nil, secretErr(id.Name, err)
	}
	raw, err := ssh.ParseRawPrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("identity %q: %w", id.Name, err)
	}
	add := agent.AddedKey{PrivateKey: normalizeKey(raw), Comment: id.Name}
	if id.Certificate != "" {
		parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(id.Certificate))
		if err != nil {
			return nil, fmt.Errorf("identity %q: %w", id.Name, err)
		}
		cert, ok := parsed.(*ssh.Certificate)
		if !ok {
			return nil, errors.New("stored certificate is not a certificate")
		}
		add.Certificate = cert
	}
	ring := agent.NewKeyring()
	if err := ring.Add(add); err != nil {
		return nil, err
	}
	return ring, nil
}
