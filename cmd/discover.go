package cmd

import (
	"fmt"
	"time"

	"github.com/dHarshMakwana/lfess/internal/discovery"
	lfessmdns "github.com/dHarshMakwana/lfess/internal/discovery/mdns"
	"github.com/spf13/cobra"
)

var newPeerDiscoverer = func(serviceName string) discovery.Discoverer {
	return lfessmdns.NewDiscoverer(serviceName)
}

func newDiscoverCmd() *cobra.Command {
	timeout := 2 * time.Second
	serviceName := lfessmdns.DefaultServiceName

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover LFESS peers on the local network via mDNS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := newPeerDiscoverer(serviceName)
			peers, err := d.Discover(cmd.Context(), timeout)
			if err != nil {
				return err
			}
			if len(peers) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no peers discovered")
				return nil
			}
			for _, peer := range peers {
				fmt.Fprintln(cmd.OutOrStdout(), peer.Address)
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "mDNS discovery timeout")
	cmd.Flags().StringVar(&serviceName, "mdns-service", serviceName, "mDNS service name")

	return cmd
}
