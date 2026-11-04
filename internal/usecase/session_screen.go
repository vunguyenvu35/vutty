package usecase

// screenState is what the assistant sees of a session.
type screenState struct {
	hostID string
	raw    []byte
	seq    int64
	state  string
}

func (s *SessionService) screen(sid string) (screenState, error) {
	sess, err := s.get(sid)
	if err != nil {
		return screenState{}, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return screenState{hostID: sess.hostID, raw: append([]byte(nil), sess.screen...), seq: sess.screenSeq, state: sess.state}, nil
}

// screenSince returns the output that arrived after seq.
func (s *SessionService) screenSince(sid string, seq int64) ([]byte, int64, error) {
	sess, err := s.get(sid)
	if err != nil {
		return nil, 0, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	n := int(sess.screenSeq - seq)
	if n > len(sess.screen) {
		n = len(sess.screen)
	}
	if n < 0 {
		n = 0
	}
	return append([]byte(nil), sess.screen[len(sess.screen)-n:]...), sess.screenSeq, nil
}
