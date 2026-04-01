package cmd

import (
	"context"
	"os"
	"strconv"
	"strings"

	lfessserver "github.com/dHarshMakwana/lfess/server"
	"github.com/spf13/cobra"
)

const (
	hostEnv = "LFESS_HOST"
	portEnv = "LFESS_PORT"
)

func newServeCmd() *cobra.Command {
	host := defaultServeHost()
	port := defaultServePort()

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start LFESS HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return lfessserver.Run(context.Background(), lfessserver.Config{
				DataDir: dataDir,
				Host:    host,
				Port:    port,
			})
		},
	}

	cmd.Flags().StringVar(&host, "host", host, "server bind host")
	cmd.Flags().IntVar(&port, "port", port, "server bind port")

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
