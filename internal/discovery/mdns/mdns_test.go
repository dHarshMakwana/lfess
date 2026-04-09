package mdns

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	hashicorpmdns "github.com/hashicorp/mdns"
	"github.com/stretchr/testify/require"
)

func TestNewAnnouncer_InvalidPort(t *testing.T) {
	t.Parallel()

	_, err := NewAnnouncer(AnnouncerConfig{BindHost: "127.0.0.1", Port: 0})
	require.Error(t, err)
}

func TestAdvertisedIPs_Loopback(t *testing.T) {
	t.Parallel()

	ips, err := advertisedIPs("127.0.0.1")
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	require.Equal(t, "127.0.0.1", ips[0].String())
}

func TestPeerFromEntry_FormatsAddress(t *testing.T) {
	t.Parallel()

	peer, ok := peerFromEntry(&hashicorpmdns.ServiceEntry{
		Name:   "lfess-node",
		AddrV4: net.ParseIP("10.0.0.8"),
		Port:   7777,
	})
	require.True(t, ok)
	require.Equal(t, "http://10.0.0.8:7777", peer.Address)
	require.Equal(t, "10.0.0.8", peer.Host)
	require.Equal(t, 7777, peer.Port)
}

func TestDiscoverer_NonBlockingWithTimeout(t *testing.T) {
	t.Parallel()

	d := NewDiscoverer(DefaultServiceName)
	start := time.Now()
	peers, err := d.Discover(context.Background(), 100*time.Millisecond)
	duration := time.Since(start)
	require.NoError(t, err)
	require.NotNil(t, peers)
	require.Less(t, duration, 2*time.Second)
}

func TestDiscoverer_DiscoversAnnouncedPeer(t *testing.T) {
	port := freePort(t)
	instance := fmt.Sprintf("lfess-test-%d", time.Now().UnixNano())

	announcer, err := NewAnnouncer(AnnouncerConfig{
		ServiceName: DefaultServiceName,
		Instance:    instance,
		BindHost:    "127.0.0.1",
		Port:        port,
		InfoFields:  []string{"lfess=1"},
	})
	require.NoError(t, err)
	defer func() { _ = announcer.Close() }()

	d := NewDiscoverer(DefaultServiceName)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		peers, err := d.Discover(context.Background(), 750*time.Millisecond)
		require.NoError(t, err)
		for _, peer := range peers {
			if peer.Port == port {
				require.NotEmpty(t, peer.Address)
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	t.Fatalf("did not discover announced peer on port %d", port)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
