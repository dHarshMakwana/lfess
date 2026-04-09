package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/dHarshMakwana/lfess/internal/bootstrap"
	"github.com/dHarshMakwana/lfess/internal/peersync"
	"github.com/spf13/cobra"
)

const (
	defaultPairLocalServer = "http://127.0.0.1:7777"
	maxPairResponseSize    = 1 << 20 // 1 MiB
)

var newPairHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

type pairStartRequest struct {
	TTLSeconds int64 `json:"ttl_seconds"`
}

type pairStartResponse struct {
	SessionID string    `json:"session_id"`
	PairCode  string    `json:"pair_code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type pairExchangeRequest struct {
	SessionID          string `json:"session_id"`
	Proof              string `json:"proof"`
	EphemeralRecipient string `json:"ephemeral_recipient"`
}

type pairExchangeResponse struct {
	Payload string `json:"payload"`
}

func newPairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "One-time authenticated key bootstrap",
	}

	cmd.AddCommand(newPairStartCmd())
	cmd.AddCommand(newPairJoinCmd())

	return cmd
}

func newPairStartCmd() *cobra.Command {
	ttl := 2 * time.Minute
	serverAddr := defaultPairLocalServer

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a one-time bootstrap session on the local running server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl <= 0 {
				return fmt.Errorf("ttl must be greater than zero")
			}

			baseURL, err := peersync.NormalizePeerAddress(serverAddr)
			if err != nil {
				return fmt.Errorf("invalid local server address: %w", err)
			}

			payload, err := json.Marshal(pairStartRequest{TTLSeconds: int64(ttl / time.Second)})
			if err != nil {
				return fmt.Errorf("encode start request: %w", err)
			}

			resp, err := newPairHTTPClient().Post(baseURL+"/bootstrap/session", "application/json", bytes.NewReader(payload))
			if err != nil {
				return fmt.Errorf("start bootstrap session: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				msg, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
				if readErr != nil {
					return fmt.Errorf("start bootstrap session: unexpected status %d", resp.StatusCode)
				}
				return fmt.Errorf("start bootstrap session: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
			}

			var out pairStartResponse
			if err := decodeJSONStrict(io.LimitReader(resp.Body, maxPairResponseSize), &out); err != nil {
				return fmt.Errorf("decode start response: %w", err)
			}
			if out.PairCode == "" || out.SessionID == "" {
				return errors.New("start bootstrap session: response missing session details")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Pair code: %s\n", out.PairCode)
			fmt.Fprintf(cmd.OutOrStdout(), "Session ID: %s\n", out.SessionID)
			fmt.Fprintf(cmd.OutOrStdout(), "Expires at: %s\n", out.ExpiresAt.UTC().Format(time.RFC3339))
			return nil
		},
	}

	cmd.Flags().DurationVar(&ttl, "ttl", ttl, "bootstrap session time-to-live")
	cmd.Flags().StringVar(&serverAddr, "server", serverAddr, "local bootstrap server base URL (http://host:port)")

	return cmd
}

func newPairJoinCmd() *cobra.Command {
	var pairCode string

	cmd := &cobra.Command{
		Use:   "join <peer-addr>",
		Short: "Join a bootstrap session and install key.age",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peerBaseURL, err := peersync.NormalizePeerAddress(args[0])
			if err != nil {
				return err
			}

			sessionID, codeSecret, err := bootstrap.ParsePairCode(pairCode)
			if err != nil {
				return err
			}
			proof, err := bootstrap.ComputeProof(sessionID, codeSecret)
			if err != nil {
				return err
			}

			ephemeralID, err := age.GenerateX25519Identity()
			if err != nil {
				return fmt.Errorf("generate ephemeral identity: %w", err)
			}

			reqPayload, err := json.Marshal(pairExchangeRequest{
				SessionID:          sessionID,
				Proof:              proof,
				EphemeralRecipient: ephemeralID.Recipient().String(),
			})
			if err != nil {
				return fmt.Errorf("encode exchange request: %w", err)
			}

			resp, err := newPairHTTPClient().Post(peerBaseURL+"/bootstrap/exchange", "application/json", bytes.NewReader(reqPayload))
			if err != nil {
				return fmt.Errorf("bootstrap exchange: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				msg, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
				if readErr != nil {
					return fmt.Errorf("bootstrap exchange: unexpected status %d", resp.StatusCode)
				}
				return fmt.Errorf("bootstrap exchange: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
			}

			var exchangeResp pairExchangeResponse
			if err := decodeJSONStrict(io.LimitReader(resp.Body, maxPairResponseSize), &exchangeResp); err != nil {
				return fmt.Errorf("decode exchange response: %w", err)
			}
			if strings.TrimSpace(exchangeResp.Payload) == "" {
				return errors.New("bootstrap exchange: missing payload")
			}

			ciphertext, err := base64.StdEncoding.DecodeString(exchangeResp.Payload)
			if err != nil {
				return fmt.Errorf("decode bootstrap payload: %w", err)
			}
			r, err := age.Decrypt(bytes.NewReader(ciphertext), ephemeralID)
			if err != nil {
				return fmt.Errorf("decrypt bootstrap payload: %w", err)
			}
			keyPayload, err := io.ReadAll(r)
			if err != nil {
				return fmt.Errorf("read bootstrap payload: %w", err)
			}

			if err := bootstrap.InstallKeyAtomically(dataDir, keyPayload); err != nil {
				return fmt.Errorf("install key material: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "bootstrap completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&pairCode, "code", "", "one-time pair code in <session_id>.<secret> format")
	_ = cmd.MarkFlagRequired("code")

	return cmd
}

func decodeJSONStrict(r io.Reader, out any) error {
	dec := json.NewDecoder(r)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}
