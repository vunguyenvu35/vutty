package app

import (
	"net"

	"vutty/internal/usecase"
	"vutty/internal/vncx"
)

// vncBridge gives a vncx.Bridge the shape the use case wants.
type vncBridge struct{ b *vncx.Bridge }

func (v vncBridge) Address() string { return v.b.URL }
func (v vncBridge) Close()          { v.b.Close() }

func startVNC(dial func() (net.Conn, error), onEnd func(error)) (usecase.VNCBridge, error) {
	b, err := vncx.Start(dial, onEnd)
	if err != nil {
		return nil, err
	}
	return vncBridge{b}, nil
}
