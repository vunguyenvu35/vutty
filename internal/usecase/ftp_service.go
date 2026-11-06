package usecase

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"vutty/internal/domain"
)

const ftpPrefix = "ftp:"

// FTPID is the id that opens a saved host over FTP in the SFTP tab instead of over SSH.
func FTPID(hostID string) string { return ftpPrefix + hostID }

// ftpCredentials returns what to connect with: the address, the security mode, the login and the password. A password saved for
// FTP wins over the one of the host's identity (when that is a password); a host with no password at all logs in as anonymous.
func (c *Core) ftpCredentials(h domain.Host) (FTPConfig, error) {
	if !h.FTPEnabled {
		return FTPConfig{}, fmt.Errorf("FTP is not enabled for %q: edit the host and turn it on", h.Label)
	}
	if h.JumpHostID != "" {
		return FTPConfig{}, errors.New("FTP cannot go through a jump host: its data connections need a direct route")
	}
	cfg := FTPConfig{Addr: net.JoinHostPort(h.Hostname, strconv.Itoa(h.FTPPort)), Security: h.FTPSecurity, User: h.FTPUsername}
	if cfg.User == "" {
		cfg.User = h.Username
	}
	if pw, err := c.Vault.GetSecret(domain.FTPSecretRef(h.ID)); err == nil {
		cfg.Password = string(pw)
		return cfg, nil
	}
	if h.IdentityID != "" {
		id, err := c.Store.GetIdentity(h.IdentityID)
		if err != nil {
			return FTPConfig{}, fmt.Errorf("identity of %q: %w", h.Label, err)
		}
		if cfg.User == "" {
			cfg.User = id.Username
		}
		if id.AuthType == domain.AuthPassword {
			pw, err := c.Vault.GetSecret(id.SecretRef)
			if err != nil {
				return FTPConfig{}, vaultErr(err)
			}
			cfg.Password = string(pw)
		}
	}
	return cfg, nil
}

// openFTP is Open for a host reached with FTP: the same file session, with no SSH connection under it.
func (s *SftpService) openFTP(hostID string) (SftpInfo, error) {
	if s.c.OpenFTP == nil {
		return SftpInfo{}, errors.New("FTP is not available in this build")
	}
	h, err := s.c.Store.GetHost(strings.TrimPrefix(hostID, ftpPrefix))
	if err != nil {
		return SftpInfo{}, err
	}
	cfg, err := s.c.ftpCredentials(h)
	if err != nil {
		s.c.record(domain.ActSFTP, h.ID, h.Label, "FTP connection failed: "+err.Error(), false)
		return SftpInfo{}, err
	}
	fs, err := s.c.OpenFTP(cfg)
	if err != nil {
		s.c.record(domain.ActSFTP, h.ID, h.Label, "FTP connection failed: "+err.Error(), false)
		return SftpInfo{}, err
	}
	home, err := fs.Home()
	if err != nil {
		_ = fs.Close()
		return SftpInfo{}, fmt.Errorf("read the remote home directory: %w", err)
	}
	sid := domain.NewID()
	sess := &sftpSession{hostID: h.ID, label: h.Label, fs: fs, stop: make(chan struct{})}
	sess.q = newTransferQueue(s, sid, sess)
	sess.edits = newEditWatches(s, sid, sess)
	msg := "FTP connection opened"
	switch h.FTPSecurity {
	case "":
		msg = "FTP connection opened (unencrypted)"
	default:
		msg = "FTPS connection opened (encrypted)"
	}
	s.c.record(domain.ActSFTP, h.ID, h.Label, msg, true)
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	if p, ok := fs.(interface{ Ping() error }); ok {
		go s.pingFTP(sid, sess, p, time.Duration(h.KeepaliveSec)*time.Second)
	}
	return SftpInfo{SID: sid, Home: home, Host: h.Label}, nil
}

// pingFTP keeps the control connection open and notices when it is gone, as the keepalive of an SSH connection does.
func (s *SftpService) pingFTP(sid string, sess *sftpSession, p interface{ Ping() error }, every time.Duration) {
	if every <= 0 {
		every = 30 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-sess.stop:
			return
		case <-t.C:
			if err := p.Ping(); err != nil {
				s.mu.Lock()
				_, still := s.sessions[sid]
				delete(s.sessions, sid)
				s.mu.Unlock()
				sess.close()
				if still {
					s.c.emit("sftp:closed:"+sid, map[string]any{"reason": "connection lost"})
					s.c.record(domain.ActSFTP, sess.hostID, sess.label, "FTP connection lost", false)
				}
				return
			}
		}
	}
}
