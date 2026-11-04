package usecase

import (
	"errors"
	"strings"
	"unicode/utf8"
	"vutty/internal/domain"
)

// runDevice runs a session that has no SSH connection: a shell on this computer or a serial port. There is
// no host key, no jump chain, no keepalive and no forwards; everything else (events, buffering, resize,
// reconnect with Enter, close) is shared with SSH sessions.
func (s *SessionService) runDevice(sess *session) {
	sess.mu.Lock()
	sess.state, sess.closedEv, sess.ready = "connecting", nil, nil
	cols, rows := sess.cols, sess.rows
	sess.mu.Unlock()

	batch := s.batcher(sess)
	var (
		sh    Shell
		err   error
		label string
	)
	if sess.hostID == LocalHostID {
		label = "This computer"
		if s.c.OpenLocalShell == nil {
			err = errors.New("the local terminal is not available in this build")
		} else {
			sh, err = s.c.OpenLocalShell(cols, rows, batch.Write)
		}
	} else if strings.HasPrefix(sess.hostID, kubeExecPrefix) {
		sh, label, err = s.openKubeShell(sess, cols, rows, batch.Write)
	} else if strings.HasPrefix(sess.hostID, telnetPrefix) {
		sh, label, err = s.openTelnet(sess, cols, rows, batch.Write)
	} else {
		var path string
		var baud int
		if path, baud, err = ParseSerialID(sess.hostID); err == nil {
			label = path
			if s.c.OpenSerial == nil {
				err = errors.New("serial ports are not available in this build")
			} else {
				sh, err = s.c.OpenSerial(path, baud, batch.Write)
			}
		}
	}
	if err != nil {
		batch.Close()
		s.markClosed(sess, err.Error(), false)
		return
	}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		sh.Close()
		batch.Close()
		return
	}
	sess.shell = sh
	sess.state, sess.ready = "ready", map[string]any{"host": label}
	s.c.record(domain.ActConnect, sess.hostID, label, s.openedMessage(sess, label), true)
	s.c.emit("term:ready:"+sess.sid, sess.ready)
	sess.mu.Unlock()

	<-sh.Done()
	err = sh.Err()
	batch.Close()
	sh.Close()

	sess.mu.Lock()
	wasClosed := sess.closed
	sess.shell = nil
	sess.mu.Unlock()
	if wasClosed {
		s.c.record(domain.ActDisconnect, sess.hostID, "", "Closed by you", true)
		return
	}
	if err == nil {
		s.markClosed(sess, "session ended", true)
		return
	}
	s.markClosed(sess, "connection lost: "+err.Error(), false)
}

// openTelnet connects a Telnet session for a saved host.
func (s *SessionService) openTelnet(sess *session, cols, rows int, onData func([]byte)) (Shell, string, error) {
	h, err := s.c.Store.GetHost(strings.TrimPrefix(sess.hostID, telnetPrefix))
	if err != nil {
		return nil, "", err
	}
	if s.c.OpenTelnet == nil {
		return nil, h.Label, errors.New("Telnet is not available in this build")
	}
	addr, user, pass, err := s.c.telnetCredentials(h)
	if err != nil {
		return nil, h.Label, err
	}
	if h.TelnetEncoding != "iso-8859-1" {
		sh, err := s.c.OpenTelnet(addr, cols, rows, user, pass, onData)
		return sh, h.Label, err
	}
	// Latin-1 servers: what arrives is turned into UTF-8 for the terminal, what is typed back into Latin-1
	sh, err := s.c.OpenTelnet(addr, cols, rows, user, pass, func(p []byte) { onData(latin1ToUTF8(p)) })
	if err != nil {
		return nil, h.Label, err
	}
	return latin1Shell{sh}, h.Label, nil
}

// openedMessage is the log line for a session that has no SSH connection.
func (s *SessionService) openedMessage(sess *session, label string) string {
	switch {
	case sess.hostID == LocalHostID:
		return "Opened a terminal on this computer"
	case strings.HasPrefix(sess.hostID, telnetPrefix):
		return "Connected with Telnet (unencrypted) to " + label
	case strings.HasPrefix(sess.hostID, kubeExecPrefix):
		return "Opened a shell in " + label
	}
	return "Opened the serial port " + label
}

// latin1ToUTF8 turns ISO-8859-1 bytes into UTF-8.
func latin1ToUTF8(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		out = utf8.AppendRune(out, rune(b))
	}
	return out
}

// latin1Shell types UTF-8 text as ISO-8859-1 (characters that do not exist there become "?").
type latin1Shell struct{ Shell }

func (l latin1Shell) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p))
	for _, r := range string(p) {
		if r > 0xff {
			r = '?'
		}
		out = append(out, byte(r))
	}
	if _, err := l.Shell.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
