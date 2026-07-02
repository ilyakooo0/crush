// Package copilot provides GitHub Copilot integration.
package copilot

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/charmbracelet/crush/internal/log"
)

var assistantRolePattern = regexp.MustCompile(`"role"\s*:\s*"assistant"`)

// NewClient creates a new HTTP client with a custom transport that adds the
// X-Initiator header based on message history in the request body.
func NewClient(isSubAgent, debug bool) *http.Client {
	t := &initiatorTransport{debug: debug, isSubAgent: isSubAgent}
	if debug {
		// Build the debug HTTP client once and reuse it on every RoundTrip.
		t.debugClient = log.NewHTTPClient()
	}
	return &http.Client{
		Transport: t,
	}
}

type initiatorTransport struct {
	debug       bool
	isSubAgent  bool
	debugClient *http.Client
}

func (t *initiatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	const (
		xInitiatorHeader = "X-Initiator"
		userInitiator    = "user"
		agentInitiator   = "agent"
	)

	if req == nil {
		return nil, fmt.Errorf("HTTP request is nil")
	}
	// Sub-agents are always attributed to the agent initiator regardless of
	// body content, so short-circuit before reading/cloning the body.
	if t.isSubAgent {
		req.Header.Set(xInitiatorHeader, agentInitiator)
		slog.Debug("Setting X-Initiator header to agent (sub-agent)")
		return t.roundTrip(req)
	}
	if req.Body == nil || req.Body == http.NoBody {
		// No body to inspect; default to user. A nil Body is valid for
		// bodyless requests (e.g. GET), and is distinct from http.NoBody,
		// so both must be handled before reading below.
		req.Header.Set(xInitiatorHeader, userInitiator)
		slog.Debug("Setting X-Initiator header to user (no request body)")
		return t.roundTrip(req)
	}

	// Clone request to avoid modifying the original.
	req = req.Clone(req.Context())

	// Read the original body into bytes so we can examine it.
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	defer req.Body.Close()

	// Restore the original body using the preserved bytes.
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Check for assistant messages using regex to handle whitespace
	// variations in the JSON while avoiding full unmarshalling overhead.
	initiator := userInitiator
	if assistantRolePattern.Match(bodyBytes) {
		slog.Debug("Setting X-Initiator header to agent (found assistant messages in history)")
		initiator = agentInitiator
	} else {
		slog.Debug("Setting X-Initiator header to user (no assistant messages)")
	}
	req.Header.Set(xInitiatorHeader, initiator)

	return t.roundTrip(req)
}

func (t *initiatorTransport) roundTrip(req *http.Request) (*http.Response, error) {
	if t.debug {
		return t.debugClient.Transport.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
