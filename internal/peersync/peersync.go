package peersync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dHarshMakwana/lfess/internal/store"
)

const (
	defaultHTTPTimeout = 15 * time.Second
	maxOpsResponseSize = 64 << 20 // 64 MiB
	maxErrorBodySize   = 4 << 10  // 4 KiB
)

var ErrInvalidPeerAddress = errors.New("peer address must be in the form http://<peer-ip>:<port>")

type Client struct {
	httpClient *http.Client
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{httpClient: httpClient}
}

// NormalizePeerAddress validates a manual peer address and returns a canonical
// base URL suitable for sync requests.
func NormalizePeerAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidPeerAddress
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPeerAddress, err)
	}
	if u.Scheme != "http" {
		return "", fmt.Errorf("%w: only http is supported", ErrInvalidPeerAddress)
	}
	if strings.TrimSpace(u.Hostname()) == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidPeerAddress)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: query and fragment are not allowed", ErrInvalidPeerAddress)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%w: path is not allowed", ErrInvalidPeerAddress)
	}

	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""

	return strings.TrimRight(u.String(), "/"), nil
}

// PullAndImport fetches encrypted lines from peer /ops and imports them into
// the local append-only log through store import flow.
func (c *Client) PullAndImport(ctx context.Context, peerAddr string, log *store.OpsLog) (int, error) {
	if log == nil {
		return 0, errors.New("ops log is required")
	}

	baseURL, err := NormalizePeerAddress(peerAddr)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/ops", nil)
	if err != nil {
		return 0, fmt.Errorf("build peer request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch peer ops: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return 0, fmt.Errorf("fetch peer ops: unexpected status %d: %s", resp.StatusCode, msg)
	}

	var lines []string
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxOpsResponseSize))
	if err := dec.Decode(&lines); err != nil {
		return 0, fmt.Errorf("decode peer ops response: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("decode peer ops response: trailing data")
	}

	imported, err := log.ImportEncryptedLines(lines)
	if err != nil {
		return 0, fmt.Errorf("import peer ops: %w", err)
	}

	return imported, nil
}
