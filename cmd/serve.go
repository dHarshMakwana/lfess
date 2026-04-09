package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	lfessmdns "github.com/dHarshMakwana/lfess/internal/discovery/mdns"
	lfessserver "github.com/dHarshMakwana/lfess/server"
	"github.com/spf13/cobra"
)

const (
	hostEnv = "LFESS_HOST"
	portEnv = "LFESS_PORT"
)

type mdnsAnnouncer interface {
	Close() error
}

var runHTTPServer = lfessserver.Run

var newServeAnnouncer = func(cfg lfessmdns.AnnouncerConfig) (mdnsAnnouncer, error) {
	return lfessmdns.NewAnnouncer(cfg)
}

func newServeCmd() *cobra.Command {
	host := defaultServeHost()
	port := defaultServePort()
	enableMDNS := false
	mdnsName := ""
	mdnsService := lfessmdns.DefaultServiceName

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start LFESS HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if enableMDNS {
				announcer, err := newServeAnnouncer(lfessmdns.AnnouncerConfig{
					ServiceName: mdnsService,
					Instance:    mdnsName,
					BindHost:    host,
					Port:        port,
					InfoFields:  []string{"lfess=1"},
				})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: mDNS announce disabled: %v\n", err)
				} else {
					defer func() {
						if closeErr := announcer.Close(); closeErr != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "warning: mDNS shutdown failed: %v\n", closeErr)
						}
					}()
				}
			}

			return runHTTPServer(cmd.Context(), lfessserver.Config{
				DataDir: dataDir,
				Host:    host,
				Port:    port,
			})
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "server bind host")
	cmd.Flags().IntVar(&port, "port", port, "server bind port")
	cmd.Flags().BoolVar(&enableMDNS, "mdns", enableMDNS, "enable mDNS peer announcement (discovery only)")
	cmd.Flags().StringVar(&mdnsName, "mdns-name", mdnsName, "mDNS instance name")
	cmd.Flags().StringVar(&mdnsService, "mdns-service", mdnsService, "mDNS service name")

	return cmd
}

func defaultServeHost() string {
	h := strings.TrimSpace(os.Getenv(hostEnv))
	if h == "" {
		return lfessserver.DefaultHost
	}
	return h
}

func defaultServePort() int {
	raw := strings.TrimSpace(os.Getenv(portEnv))
	if raw == "" {
		return lfessserver.DefaultPort
	}

	p, err := strconv.Atoi(raw)
	if err != nil || p < 1 || p > 65535 {
		return lfessserver.DefaultPort
	}
	return p
}
