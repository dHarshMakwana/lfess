package discovery

import (
	"context"
	"time"
)

// Peer represents a discoverable LFESS node address for manual HTTP sync.
type Peer struct {
	Name    string
	Host    string
	Port    int
	Address string
}

// Discoverer returns available peers on the local network.
// Discovery is optional and separate from core sync protocol logic.
type Discoverer interface {
	Discover(ctx context.Context, timeout time.Duration) ([]Peer, error)
}

// Announcer publishes this node's address for local-network discovery.
// It is a replaceable component and must not affect HTTP sync semantics.
type Announcer interface {
	Close() error
}
