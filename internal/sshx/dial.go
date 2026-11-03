// Package sshx wraps golang.org/x/crypto/ssh: dialing through jump host chains,
// PTY shells, keepalive and output batching.
package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// MaxChainDepth bounds jump host recursion.
const MaxChainDepth = 8

// Hop is one machine in a chain: where to connect and how to authenticate.
type Hop struct {
	Label    string
	Hostname string
	Port     int
	Username string
	Auth     []ssh.AuthMethod
	// Password is the saved password: a keyboard-interactive question that asks for the password gets it.
	Password string
	// Ask is called with the questions the server asks that the saved password does not answer (a verification
	// code, a Duo choice…). With it, keyboard-interactive is offered after the methods in Auth.
	Ask Asker
}

func (h Hop) addr() string { return net.JoinHostPort(h.Hostname, strconv.Itoa(h.Port)) }

// Client is an SSH connection that owns every intermediate jump connection.
type Client struct {
	*ssh.Client
	jumps    []*ssh.Client
	agentFwd bool // shells of this connection ask the server to forward the agent
}

// ForwardAgentSocket lets the server use the ssh-agent at sock (SSH_AUTH_SOCK) for as long as this connection lasts, like ssh -A.
func (c *Client) ForwardAgentSocket(sock string) error {
	if err := agent.ForwardToRemote(c.Client, sock); err != nil {
		return err
	}
	c.agentFwd = true
	return nil
}

// ForwardAgentKeys lets the server use the keys of ag (an agent that lives in this process) for as long as this connection lasts.
func (c *Client) ForwardAgentKeys(ag agent.Agent) error {
	if err := agent.ForwardToAgent(c.Client, ag); err != nil {
		return err
	}
	c.agentFwd = true
	return nil
}

// Close closes the target connection first, then the jump hosts (innermost first).
func (c *Client) Close() error {
	err := c.Client.Close()
	for i := len(c.jumps) - 1; i >= 0; i-- {
		c.jumps[i].Close()
	}
	return err
}

// Dialer connects to the last hop of a chain via all previous ones.
type Dialer struct {
	HostKeys *HostKeyChecker
	Timeout  time.Duration
}

func (d *Dialer) cfg(h Hop) *ssh.ClientConfig {
	t := d.Timeout
	if t == 0 {
		t = 15 * time.Second
	}
	auth := h.Auth
	if h.Ask != nil || h.Password != "" {
		auth = append(append([]ssh.AuthMethod{}, h.Auth...), InteractiveAuth(h.Label, h.Password, h.Ask))
	}
	return &ssh.ClientConfig{
		User:            h.Username,
		Auth:            auth,
		HostKeyCallback: d.HostKeys.Callback(),
		Timeout:         t,
	}
}

// Dial connects through chain[0] -> chain[1] -> ... -> chain[n-1].
func (d *Dialer) Dial(chain []Hop) (*Client, error) {
	if len(chain) == 0 {
		return nil, errors.New("empty hop chain")
	}
	if len(chain) > MaxChainDepth {
		return nil, fmt.Errorf("jump chain longer than %d hops", MaxChainDepth)
	}
	if d.HostKeys == nil {
		return nil, errors.New("host key checker is required")
	}
	var jumps []*ssh.Client
	fail := func(err error) (*Client, error) {
		for i := len(jumps) - 1; i >= 0; i-- {
			jumps[i].Close()
		}
		return nil, err
	}
	first := chain[0]
	cur, err := ssh.Dial("tcp", first.addr(), d.cfg(first))
	if err != nil {
		return fail(fmt.Errorf("connect %s: %w", first.Label, err))
	}
	for _, hop := range chain[1:] {
		next, err := dialVia(cur, hop.addr(), d.cfg(hop))
		if err != nil {
			cur.Close()
			return fail(fmt.Errorf("connect %s: %w", hop.Label, err))
		}
		jumps = append(jumps, cur)
		cur = next
	}
	return &Client{Client: cur, jumps: jumps}, nil
}

// dialVia opens an SSH connection to addr through an existing client.
func dialVia(jump *ssh.Client, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := jump.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout > 0 {
		conn.SetDeadline(time.Now().Add(cfg.Timeout))
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// KeyAuth authenticates with a parsed private key.
func KeyAuth(s ssh.Signer) ssh.AuthMethod { return ssh.PublicKeys(s) }

// Prompt is one question of a keyboard-interactive round. Echo says whether what is typed may be shown.
type Prompt struct {
	Text string `json:"text"`
	Echo bool   `json:"echo"`
}

// Challenge is a round of keyboard-interactive questions that the user has to answer.
type Challenge struct {
	Host        string
	Name        string
	Instruction string
	Prompts     []Prompt
}

// Asker puts a Challenge to the user and returns one answer per prompt.
type Asker func(Challenge) ([]string, error)

// codeWords are what a question that is not asking for the password talks about.
var codeWords = regexp.MustCompile(`(?i)\b(verification|code|token|passcode|otp|one[- ]time|authenticator|2fa|two[- ]factor|duo|pin|factor|response|challenge)\b`)

// looksLikeCode reports whether a question asks for a second factor, not for the password.
func looksLikeCode(question string) bool { return codeWords.MatchString(question) }

// InteractiveAuth answers the keyboard-interactive rounds of a server: a question that asks for the password gets
// the saved one (once: a second ask means it was wrong), every other question goes to ask. With no ask a server that
// wants more than the password cannot be signed in to, and says so.
func InteractiveAuth(host, saved string, ask Asker) ssh.AuthMethod {
	usedSaved := false
	return ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		var prompts []Prompt
		var at []int
		for i, q := range questions {
			if saved != "" && !usedSaved && !looksLikeCode(q) {
				answers[i], usedSaved = saved, true
				continue
			}
			prompts = append(prompts, Prompt{Text: q, Echo: i < len(echos) && echos[i]})
			at = append(at, i)
		}
		if len(prompts) == 0 {
			return answers, nil
		}
		if ask == nil {
			return nil, fmt.Errorf("the server asks %q, which needs you to type the answer, and this connection cannot ask you", strings.TrimSpace(prompts[0].Text))
		}
		got, err := ask(Challenge{Host: host, Name: name, Instruction: instruction, Prompts: prompts})
		if err != nil {
			return nil, err
		}
		if len(got) != len(prompts) {
			return nil, errors.New("the answers do not match the questions")
		}
		for j, i := range at {
			answers[i] = got[j]
		}
		return answers, nil
	})
}

// PasswordAuth tries password then keyboard-interactive with the same password.
func PasswordAuth(pw string) []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.Password(pw),
		ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			out := make([]string, len(qs))
			for i := range out {
				out[i] = pw
			}
			return out, nil
		}),
	}
}

// AgentAuth uses the running ssh-agent from SSH_AUTH_SOCK.
func AgentAuth() (ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set: no ssh-agent available")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("ssh-agent: %w", err)
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers), nil
}

// Exec runs one command and returns its standard output, giving up after timeout. It is used for tiny
// read-only probes (server type detection), never for user commands.
func (c *Client) Exec(cmd string, timeout time.Duration) (string, error) {
	sess, err := c.Client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.Output(cmd)
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		return string(r.out), r.err
	case <-time.After(timeout):
		return "", errors.New("the command timed out")
	}
}

// SavedPassword authenticates with a password and nothing else: whatever else the server asks for goes through
// InteractiveAuth (see Hop.Password and Hop.Ask).
func SavedPassword(pw string) []ssh.AuthMethod { return []ssh.AuthMethod{ssh.Password(pw)} }
