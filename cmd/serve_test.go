package cmd

import (
	"testing"

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
