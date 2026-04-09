package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dHarshMakwana/lfess/internal/discovery"
	"github.com/stretchr/testify/require"
)

type stubDiscoverer struct {
	peers          []discovery.Peer
	err            error
	capturedCtx    context.Context
	capturedTimout time.Duration
}

func (s *stubDiscoverer) Discover(ctx context.Context, timeout time.Duration) ([]discovery.Peer, error) {
	s.capturedCtx = ctx
	s.capturedTimout = timeout
	if s.err != nil {
		return nil, s.err
	}
	return s.peers, nil
}

func TestDiscoverCommand_PrintsPeers(t *testing.T) {
	stub := &stubDiscoverer{peers: []discovery.Peer{
		{Address: "http://192.168.1.20:7777"},
		{Address: "http://192.168.1.21:7777"},
	}}

	oldFactory := newPeerDiscoverer
	newPeerDiscoverer = func(serviceName string) discovery.Discoverer {
		require.Equal(t, "_lfess._tcp", serviceName)
		return stub
	}
	t.Cleanup(func() { newPeerDiscoverer = oldFactory })

	cmd := newDiscoverCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--timeout", "250ms"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, out.String(), "http://192.168.1.20:7777")
	require.Contains(t, out.String(), "http://192.168.1.21:7777")
	require.Equal(t, 250*time.Millisecond, stub.capturedTimout)
	require.NotNil(t, stub.capturedCtx)
}

func TestDiscoverCommand_NoPeersMessage(t *testing.T) {
	stub := &stubDiscoverer{}

	oldFactory := newPeerDiscoverer
	newPeerDiscoverer = func(serviceName string) discovery.Discoverer {
		return stub
	}
	t.Cleanup(func() { newPeerDiscoverer = oldFactory })

	cmd := newDiscoverCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, out.String(), "no peers discovered")
}

func TestDiscoverCommand_PropagatesDiscoverError(t *testing.T) {
	stub := &stubDiscoverer{err: errors.New("discover failed")}

	oldFactory := newPeerDiscoverer
	newPeerDiscoverer = func(serviceName string) discovery.Discoverer {
		return stub
	}
	t.Cleanup(func() { newPeerDiscoverer = oldFactory })

	cmd := newDiscoverCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "discover failed")
}
