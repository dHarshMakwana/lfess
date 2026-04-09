package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dHarshMakwana/lfess/internal/discovery"
	hashicorpmdns "github.com/hashicorp/mdns"
)

const (
	DefaultServiceName = "_lfess._tcp"
	defaultTimeout     = 2 * time.Second
)

type AnnouncerConfig struct {
	ServiceName string
	Instance    string
	BindHost    string
	Port        int
	InfoFields  []string
}

type Announcer struct {
	server *hashicorpmdns.Server
}

func NewAnnouncer(cfg AnnouncerConfig) (*Announcer, error) {
	service := strings.TrimSpace(cfg.ServiceName)
	if service == "" {
		service = DefaultServiceName
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("mdns: invalid port %d", cfg.Port)
	}

	instance := strings.TrimSpace(cfg.Instance)
	if instance == "" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			instance = "lfess"
		} else {
			instance = hostname
		}
	}

	ips, err := advertisedIPs(cfg.BindHost)
	if err != nil {
		return nil, err
	}

	zone, err := hashicorpmdns.NewMDNSService(instance, service, "", "", cfg.Port, ips, cfg.InfoFields)
	if err != nil {
		return nil, fmt.Errorf("mdns: create service: %w", err)
	}

	srv, err := hashicorpmdns.NewServer(&hashicorpmdns.Config{Zone: zone})
	if err != nil {
		return nil, fmt.Errorf("mdns: start server: %w", err)
	}

	return &Announcer{server: srv}, nil
}

func (a *Announcer) Close() error {
	if a == nil || a.server == nil {
		return nil
	}
	return a.server.Shutdown()
}

type Discoverer struct {
	ServiceName string
}

func NewDiscoverer(serviceName string) *Discoverer {
	return &Discoverer{ServiceName: serviceName}
}

func (d *Discoverer) Discover(ctx context.Context, timeout time.Duration) ([]discovery.Peer, error) {
	service := strings.TrimSpace(d.ServiceName)
	if service == "" {
		service = DefaultServiceName
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	entries := make(chan *hashicorpmdns.ServiceEntry, 32)
	params := hashicorpmdns.DefaultParams(service)
	params.Timeout = timeout
	params.Entries = entries
	params.WantUnicastResponse = true

	collected := make([]*hashicorpmdns.ServiceEntry, 0, 16)
	done := make(chan struct{})
	go func() {
		for entry := range entries {
			collected = append(collected, entry)
		}
		close(done)
	}()

	err := hashicorpmdns.QueryContext(ctx, params)
	close(entries)
	<-done

	peersByAddress := make(map[string]discovery.Peer)
	for _, entry := range collected {
		peer, ok := peerFromEntry(entry)
		if !ok {
			continue
		}
		peersByAddress[peer.Address] = peer
	}
	peers := toSortedPeers(peersByAddress)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return peers, nil
		}
		return nil, fmt.Errorf("mdns: discover peers: %w", err)
	}

	return peers, nil
}

func toSortedPeers(peersByAddress map[string]discovery.Peer) []discovery.Peer {
	peers := make([]discovery.Peer, 0, len(peersByAddress))
	for _, peer := range peersByAddress {
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Address != peers[j].Address {
			return peers[i].Address < peers[j].Address
		}
		return peers[i].Name < peers[j].Name
	})
	return peers
}

func peerFromEntry(entry *hashicorpmdns.ServiceEntry) (discovery.Peer, bool) {
	if entry == nil || entry.Port <= 0 {
		return discovery.Peer{}, false
	}

	ip := entry.AddrV4
	if ip == nil {
		if entry.AddrV6IPAddr != nil {
			ip = entry.AddrV6IPAddr.IP
		} else {
			ip = entry.AddrV6
		}
	}
	if ip == nil {
		return discovery.Peer{}, false
	}

	host := ip.String()
	if host == "" {
		return discovery.Peer{}, false
	}

	address := httpAddress(host, entry.Port)
	return discovery.Peer{
		Name:    strings.TrimSpace(entry.Name),
		Host:    host,
		Port:    entry.Port,
		Address: address,
	}, true
}

func httpAddress(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("http://[%s]:%d", host, port)
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func advertisedIPs(bindHost string) ([]net.IP, error) {
	host := strings.TrimSpace(bindHost)
	switch host {
	case "", "0.0.0.0":
		ips, err := localLANIPv4s()
		if err != nil {
			return nil, err
		}
		return ips, nil
	case "127.0.0.1", "localhost":
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	default:
		if ip := net.ParseIP(host); ip != nil {
			return []net.IP{ip}, nil
		}
		resolved, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("mdns: resolve bind host %q: %w", host, err)
		}
		ips := make([]net.IP, 0, len(resolved))
		for _, ip := range resolved {
			if ip == nil {
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("mdns: no IP addresses found for bind host %q", host)
		}
		return ips, nil
	}
}

func localLANIPv4s() ([]net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("mdns: list interfaces: %w", err)
	}

	seen := map[string]struct{}{}
	ips := make([]net.IP, 0)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			key := ip.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ips = append(ips, ip)
		}
	}

	if len(ips) == 0 {
		return nil, errors.New("mdns: no non-loopback IPv4 address available for announcement")
	}
	return ips, nil
}
