package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	lfessmdns "github.com/dHarshMakwana/lfess/internal/discovery/mdns"
	lfessserver "github.com/dHarshMakwana/lfess/server"
	"github.com/stretchr/testify/require"
)

func TestDefaultServeHost_UsesEnv(t *testing.T) {
	t.Setenv(hostEnv, "0.0.0.0")
	require.Equal(t, "0.0.0.0", defaultServeHost())
}

func TestDefaultServeHost_FallsBackWhenUnset(t *testing.T) {
	t.Setenv(hostEnv, "")
	require.Equal(t, "127.0.0.1", defaultServeHost())
}

func TestDefaultServePort_UsesEnv(t *testing.T) {
	t.Setenv(portEnv, "8890")
	require.Equal(t, 8890, defaultServePort())
}

func TestDefaultServePort_FallsBackOnInvalidEnv(t *testing.T) {
	t.Setenv(portEnv, "not-a-number")
	require.Equal(t, 7777, defaultServePort())

	t.Setenv(portEnv, "70000")
	require.Equal(t, 7777, defaultServePort())
}

func TestServeCommand_FlagDefaultsUseEnv(t *testing.T) {
	t.Setenv(hostEnv, "0.0.0.0")
	t.Setenv(portEnv, "9999")

	cmd := newServeCmd()
	host, err := cmd.Flags().GetString("host")
	require.NoError(t, err)
	port, err := cmd.Flags().GetInt("port")
	require.NoError(t, err)

	require.Equal(t, "0.0.0.0", host)
	require.Equal(t, 9999, port)
}

type testAnnouncer struct {
	closed bool
	err    error
}

func (a *testAnnouncer) Close() error {
	a.closed = true
	return a.err
}

func TestServeCommand_MDNSFailureIsNonBlocking(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	oldRun := runHTTPServer
	oldNewAnnouncer := newServeAnnouncer
	t.Cleanup(func() {
		runHTTPServer = oldRun
		newServeAnnouncer = oldNewAnnouncer
	})

	runCalled := false
	runHTTPServer = func(ctx context.Context, cfg lfessserver.Config) error {
		runCalled = true
		return nil
	}

	newServeAnnouncer = func(cfg lfessmdns.AnnouncerConfig) (mdnsAnnouncer, error) {
		return nil, errors.New("mdns unavailable")
	}

	cmd := newServeCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--mdns"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.True(t, runCalled)
	require.Contains(t, stderr.String(), "warning: mDNS announce disabled")
}

func TestServeCommand_MDNSEnabledStartsAndStopsAnnouncer(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	oldRun := runHTTPServer
	oldNewAnnouncer := newServeAnnouncer
	t.Cleanup(func() {
		runHTTPServer = oldRun
		newServeAnnouncer = oldNewAnnouncer
	})

	announcer := &testAnnouncer{}
	runHTTPServer = func(ctx context.Context, cfg lfessserver.Config) error {
		require.Equal(t, "0.0.0.0", cfg.Host)
		require.Equal(t, 7788, cfg.Port)
		return nil
	}

	newServeAnnouncer = func(cfg lfessmdns.AnnouncerConfig) (mdnsAnnouncer, error) {
		require.Equal(t, "0.0.0.0", cfg.BindHost)
		require.Equal(t, 7788, cfg.Port)
		require.Equal(t, "_lfess._tcp", cfg.ServiceName)
		require.Equal(t, "lfess-node", cfg.Instance)
		return announcer, nil
	}

	cmd := newServeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--mdns", "--host", "0.0.0.0", "--port", "7788", "--mdns-name", "lfess-node"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.True(t, announcer.closed)
}

func TestServeCommand_MDNSDisabledSkipsAnnouncer(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldDataDir })

	oldRun := runHTTPServer
	oldNewAnnouncer := newServeAnnouncer
	t.Cleanup(func() {
		runHTTPServer = oldRun
		newServeAnnouncer = oldNewAnnouncer
	})

	runHTTPServer = func(ctx context.Context, cfg lfessserver.Config) error {
		return nil
	}

	announceCalls := 0
	newServeAnnouncer = func(cfg lfessmdns.AnnouncerConfig) (mdnsAnnouncer, error) {
		announceCalls++
		return &testAnnouncer{}, nil
	}

	cmd := newServeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Equal(t, 0, announceCalls)
}
