package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent/utils"
	"github.com/Gu1llaum-3/vigil/internal/common"

	"github.com/fxamacker/cbor/v2"
	"github.com/lxzan/gws"
	"golang.org/x/crypto/ssh"
)

const (
	wsDeadline = 70 * time.Second
)

// WebSocketClient manages the WebSocket connection between the agent and hub.
// It handles authentication, message routing, and connection lifecycle management.
type WebSocketClient struct {
	gws.BuiltinEventHandler
	options *gws.ClientOption // WebSocket client configuration options
	agent   *Agent            // Reference to the parent agent
	// conn is the active connection; atomic because the event loop replaces it while the
	// previous connection's read loop may still run.
	conn   atomic.Pointer[gws.Conn]
	hubURL *url.URL // Parsed hub URL for connection
	// tokenMu guards token: the hub may issue a new one (SetAgentToken, read loop) while the
	// event loop reconnects.
	tokenMu sync.Mutex
	token   string // token sent to the hub: the stored per-agent token, else the configured one
	// configuredToken is TOKEN / TOKEN_FILE (usually the enrollment token), the fallback when
	// the hub rejects the stored token.
	configuredToken string
	hubKey          string // canonical hub URL the stored token is bound to
	// rejections counts consecutive 401 answers to the current token (event loop only).
	rejections         int
	fingerprint        string                              // System fingerprint for identification
	hubRequest         *common.HubRequest[cbor.RawMessage] // Reusable request structure for message parsing
	lastConnectAttempt time.Time                           // Timestamp of last connection attempt
	// verifiedConn is the connection on which the hub proved its identity (CheckFingerprint).
	// Verification is per connection: after a reconnect, the hub must prove it again.
	verifiedConn atomic.Pointer[gws.Conn]
	tlsConfig    *tls.Config // TLS config for the hub connection (verifies by default)
}

// newWebSocketClient creates a new WebSocket client for the given agent.
// It reads configuration from environment variables and validates the hub URL.
func newWebSocketClient(agent *Agent) (client *WebSocketClient, err error) {
	hubURLStr, exists := utils.GetEnv("HUB_URL")
	if !exists {
		return nil, errors.New("HUB_URL environment variable not set")
	}

	client = &WebSocketClient{}

	client.hubURL, err = url.Parse(hubURLStr)
	if err != nil {
		return nil, errors.New("invalid hub URL")
	}
	if err := checkHubURL(client.hubURL); err != nil {
		return nil, err
	}
	// build the TLS config for the hub connection (verifies the hub certificate by
	// default; opt-in to a custom CA or, explicitly, to skipping verification).
	client.tlsConfig, err = buildHubTLSConfig(client.hubURL.Hostname(), utils.GetEnv)
	if err != nil {
		return nil, err
	}
	client.agent = agent
	client.hubKey = hubKeyFor(client.hubURL)
	// The token issued to this agent by this hub wins over the configured one, which is
	// typically the enrollment token shared by every host (see tokenCandidates); the
	// configured token is only required while the hub has not issued one.
	var configuredErr error
	client.configuredToken, configuredErr = getToken()
	candidates := client.tokenCandidates()
	if len(candidates) == 0 {
		if configuredErr != nil {
			return nil, configuredErr
		}
		return nil, errors.New("must set TOKEN or TOKEN_FILE")
	}
	client.token = candidates[0]

	client.hubRequest = &common.HubRequest[cbor.RawMessage]{}
	client.fingerprint = agent.getFingerprint()

	return client, nil
}

// buildHubTLSConfig builds the TLS configuration for the agent→hub connection.
//
// By default it verifies the hub's certificate against the system trust store (so a
// network man-in-the-middle cannot capture the agent token). Two opt-ins are supported:
//   - HUB_CA_FILE: path to a PEM bundle to trust (private CA / self-signed hub / pinning).
//   - HUB_TLS_INSECURE=true: explicitly disable verification (development only; logs a warning).
func buildHubTLSConfig(serverName string, getEnv func(string) (string, bool)) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}

	if v, ok := getEnv("HUB_TLS_INSECURE"); ok && (v == "true" || v == "1") {
		slog.Warn("HUB_TLS_INSECURE is set: skipping hub TLS certificate verification — the connection is vulnerable to man-in-the-middle and token theft; use only in trusted/dev environments")
		cfg.InsecureSkipVerify = true
		return cfg, nil
	}

	if caFile, ok := getEnv("HUB_CA_FILE"); ok && caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read HUB_CA_FILE: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("HUB_CA_FILE %q contains no valid PEM certificates", caFile)
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}

// getToken returns the token for the WebSocket client.
// It first checks the TOKEN environment variable, then the TOKEN_FILE environment variable.
// If neither is set, it returns an error.
func getToken() (string, error) {
	// get token from env var
	token, _ := utils.GetEnv("TOKEN")
	if token != "" {
		return token, nil
	}
	// get token from file
	tokenFile, _ := utils.GetEnv("TOKEN_FILE")
	if tokenFile == "" {
		return "", errors.New("must set TOKEN or TOKEN_FILE")
	}
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(tokenBytes)), nil
}

// checkHubURL rejects a HUB_URL the agent cannot connect to safely: only http(s) and ws(s)
// are accepted (anything else used to fall back to cleartext ws), and a host is required.
// A plaintext scheme is allowed, for LAN hubs, but it sends the agent token and every
// inventory unencrypted, so it is logged loudly.
func checkHubURL(u *url.URL) error {
	switch u.Scheme {
	case "https", "wss":
	case "http", "ws":
		slog.Warn("HUB_URL is not encrypted (http:// or ws://): the agent token and all host data travel in cleartext. Use https:// unless the network between agent and hub is trusted.", "hub", u.Host)
	default:
		return fmt.Errorf("HUB_URL must start with https:// (or http:// on a trusted network), got %q", u.Redacted())
	}
	if u.Host == "" {
		return fmt.Errorf("HUB_URL has no host: %q", u.Redacted())
	}
	return nil
}

// getOptions returns the WebSocket client options, creating them if necessary.
// It configures the connection URL, TLS settings, and authentication headers.
func (client *WebSocketClient) getOptions() *gws.ClientOption {
	if client.options != nil {
		return client.connectOptions()
	}

	// update the hub url to use websocket scheme and api path (checkHubURL accepted it)
	if client.hubURL.Scheme == "https" || client.hubURL.Scheme == "wss" {
		client.hubURL.Scheme = "wss"
	} else {
		client.hubURL.Scheme = "ws"
	}
	client.hubURL.Path = path.Join(client.hubURL.Path, "api/app/agent-connect")

	client.options = &gws.ClientOption{
		Addr:      client.hubURL.String(),
		TlsConfig: client.tlsConfig,
		RequestHeader: http.Header{
			"User-Agent": []string{getUserAgent()},
			"X-App":      []string{app.Version},
		},
	}
	return client.connectOptions()
}

// connectOptions returns a copy of the cached options carrying the current token, so the
// token can change between connections without racing a dial in progress.
func (client *WebSocketClient) connectOptions() *gws.ClientOption {
	opts := *client.options
	opts.RequestHeader = client.options.RequestHeader.Clone()
	opts.RequestHeader.Set("X-Token", client.currentToken())
	return &opts
}

func (client *WebSocketClient) currentToken() string {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	return client.token
}

func (client *WebSocketClient) setToken(token string) {
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	client.token = token
}

// rejectionsBeforeFallback is how many consecutive 401 answers a token gets before the next
// candidate is tried: a single one may be transient.
const rejectionsBeforeFallback = 3

// tokenCandidates lists the tokens to try, in order: the token issued by this hub, then the
// one it replaced (the hub keeps accepting it until it has saved the new one), then the
// configured TOKEN only if it changed since the issuance (a deliberate reconfiguration,
// e.g. after rotating the token of an offline host) — last, because re-running the install
// command rewrites TOKEN with the current enrollment token. An unchanged configured token is
// never used again: a host deleted on the hub stays revoked, and a hub outage cannot make it
// re-enroll. Without an issued token: the configured one.
func (client *WebSocketClient) tokenCandidates() []string {
	var candidates []string
	add := func(t string) {
		if t != "" && !slices.Contains(candidates, t) {
			candidates = append(candidates, t)
		}
	}
	stored, ok := loadAgentToken(client.agent.dataDir, client.hubKey, keyFingerprints(client.agent.keys))
	if !ok {
		add(client.configuredToken)
		return candidates
	}
	add(stored.Token)
	add(stored.Previous)
	if client.configuredToken != "" && (stored.Configured == "" || tokenDigest(client.configuredToken) != stored.Configured) {
		add(client.configuredToken)
	}
	return candidates
}

// handleConnectError moves to the next token candidate once the hub has rejected the
// current one rejectionsBeforeFallback times in a row (host deleted on the hub, token
// rotated while offline), wrapping around so a transient rejection is never final. gws
// reports the hub's answer only as this message.
func (client *WebSocketClient) handleConnectError(err error) {
	if err == nil || !strings.Contains(err.Error(), "unexpected status code: 401") {
		client.rejections = 0
		return
	}
	client.rejections++
	if client.rejections < rejectionsBeforeFallback {
		return
	}
	client.rejections = 0
	candidates := client.tokenCandidates()
	if len(candidates) == 0 {
		return
	}
	current := client.currentToken()
	next := candidates[0]
	if i := slices.Index(candidates, current); i >= 0 {
		next = candidates[(i+1)%len(candidates)]
	}
	if next != current {
		slog.Warn("The hub keeps rejecting the agent token; trying the next one")
		client.setToken(next)
	}
}

// Connect establishes a WebSocket connection to the hub.
// It closes any existing connection before attempting to reconnect.
func (client *WebSocketClient) Connect() (err error) {
	client.lastConnectAttempt = time.Now()

	// make sure previous connection is closed
	client.Close()

	opts := client.getOptions()
	conn, _, err := gws.NewClient(client, opts)
	client.handleConnectError(err)
	if err != nil {
		return err
	}
	// The hub proves its identity by signing the token sent on this connection.
	conn.Session().Store(sentTokenKey, opts.RequestHeader.Get("X-Token"))
	client.conn.Store(conn)

	go conn.ReadLoop()

	return nil
}

// OnOpen handles WebSocket connection establishment.
// It sets a deadline for the connection to prevent hanging.
func (client *WebSocketClient) OnOpen(conn *gws.Conn) {
	conn.SetDeadline(time.Now().Add(wsDeadline))
}

// OnClose handles WebSocket connection closure.
// It logs the closure reason and notifies the connection manager.
func (client *WebSocketClient) OnClose(conn *gws.Conn, err error) {
	client.verifiedConn.CompareAndSwap(conn, nil)
	// A connection already replaced by Connect closes late: the replacement is live, so this
	// must not reset the connection state (and close it in turn).
	if conn != client.conn.Load() {
		return
	}
	if err != nil {
		slog.Warn("Connection closed", "err", strings.TrimPrefix(err.Error(), "gws: "))
	}
	client.agent.connectionManager.eventChan <- WebSocketDisconnect
}

// OnMessage handles incoming WebSocket messages from the hub.
// It decodes CBOR messages and routes them to appropriate handlers.
func (client *WebSocketClient) OnMessage(conn *gws.Conn, message *gws.Message) {
	defer message.Close()
	conn.SetDeadline(time.Now().Add(wsDeadline))

	if message.Opcode != gws.OpcodeBinary {
		return
	}

	var HubRequest common.HubRequest[cbor.RawMessage]

	err := cbor.Unmarshal(message.Data.Bytes(), &HubRequest)
	if err != nil {
		slog.Error("Error parsing message", "err", err)
		return
	}

	if err := client.handleHubRequest(conn, &HubRequest, HubRequest.Id); err != nil {
		slog.Error("Error handling message", "err", err)
	}
}

// OnPing handles WebSocket ping frames.
// It responds with a pong and updates the connection deadline.
func (client *WebSocketClient) OnPing(conn *gws.Conn, message []byte) {
	conn.SetDeadline(time.Now().Add(wsDeadline))
	// A failed pong surfaces as a read error and a reconnect.
	_ = conn.WritePong(message)
}

// handleAuthChallenge verifies the authenticity of the hub and returns the system's fingerprint.
func (client *WebSocketClient) handleAuthChallenge(conn *gws.Conn, msg *common.HubRequest[cbor.RawMessage], requestID *uint32) (err error) {
	var authRequest common.FingerprintRequest
	if err := cbor.Unmarshal(msg.Data, &authRequest); err != nil {
		return err
	}

	if err := client.verifySignature(conn, authRequest.Signature); err != nil {
		return err
	}

	client.verifiedConn.Store(conn)
	confirmAgentToken(client.agent.dataDir, client.sentToken(conn), client.configuredToken)
	client.agent.connectionManager.eventChan <- WebSocketConnect

	response := &common.FingerprintResponse{
		Fingerprint: client.fingerprint,
	}

	return client.sendResponse(conn, response, requestID)
}

// Connection session keys: the token sent on that connection, and the fingerprint of the
// hub key that verified it.
const (
	sentTokenKey   = "token"
	verifiedKeyKey = "hubKey"
)

// sentToken returns the token sent on conn ("" if unknown).
func (client *WebSocketClient) sentToken(conn *gws.Conn) string {
	if conn == nil {
		return ""
	}
	if t, ok := conn.Session().Load(sentTokenKey); ok {
		return t.(string)
	}
	return ""
}

// verifiedKeyFingerprint returns the fingerprint of the hub key that verified conn.
func (client *WebSocketClient) verifiedKeyFingerprint(conn *gws.Conn) string {
	if conn == nil {
		return ""
	}
	if fp, ok := conn.Session().Load(verifiedKeyKey); ok {
		return fp.(string)
	}
	return ""
}

// verifySignature verifies the hub's signature of the token sent on conn.
func (client *WebSocketClient) verifySignature(conn *gws.Conn, signature []byte) (err error) {
	token := client.currentToken()
	if conn != nil {
		if sent, ok := conn.Session().Load(sentTokenKey); ok {
			token = sent.(string)
		}
	}
	for _, pubKey := range client.agent.keys {
		sig := ssh.Signature{
			Format: pubKey.Type(),
			Blob:   signature,
		}
		if err = pubKey.Verify([]byte(token), &sig); err == nil {
			if conn != nil {
				conn.Session().Store(verifiedKeyKey, ssh.FingerprintSHA256(pubKey))
			}
			return nil
		}
	}
	return errors.New("invalid signature - check KEY value")
}

// Close closes the WebSocket connection gracefully.
// This method is safe to call multiple times.
func (client *WebSocketClient) Close() {
	if conn := client.conn.Load(); conn != nil {
		_ = conn.WriteClose(1000, nil)
	}
}

// handleHubRequest routes the request to the appropriate handler using the handler registry.
func (client *WebSocketClient) handleHubRequest(conn *gws.Conn, msg *common.HubRequest[cbor.RawMessage], requestID *uint32) error {
	ctx := &HandlerContext{
		Client:      client,
		Conn:        conn,
		Agent:       client.agent,
		Request:     msg,
		RequestID:   requestID,
		HubVerified: conn != nil && client.verifiedConn.Load() == conn,
		// Reply on the connection the request came from, never on a newer one that has
		// not proven the hub's identity yet.
		SendResponse: func(data any, requestID *uint32) error { return client.sendResponse(conn, data, requestID) },
	}
	return client.agent.handlerRegistry.Handle(ctx)
}

// sendMessage encodes data to CBOR and sends it as a binary message on conn.
func (client *WebSocketClient) sendMessage(conn *gws.Conn, data any) error {
	if conn == nil {
		return gws.ErrConnClosed
	}
	bytes, err := cbor.Marshal(data)
	if err != nil {
		return err
	}
	err = conn.WriteMessage(gws.OpcodeBinary, bytes)
	if err != nil {
		// If writing fails (e.g., broken pipe due to network issues), close that
		// connection to trigger the reconnection logic (#1263).
		_ = conn.WriteClose(1000, nil)
	}
	return err
}

// sendResponse sends a response with optional request ID.
// For ID-based requests, we must populate legacy typed fields for backward
// compatibility with older hubs (<= 0.17) that don't read the generic Data field.
func (client *WebSocketClient) sendResponse(conn *gws.Conn, data any, requestID *uint32) error {
	if requestID != nil {
		response := newAgentResponse(data, requestID)
		return client.sendMessage(conn, response)
	}
	// Legacy format - send data directly
	return client.sendMessage(conn, data)
}

// getUserAgent returns one of two User-Agent strings based on current time.
// This is used to avoid being blocked by Cloudflare or other anti-bot measures.
func getUserAgent() string {
	const (
		uaBase    = "Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
		uaWindows = "Windows NT 11.0; Win64; x64"
		uaMac     = "Macintosh; Intel Mac OS X 14_0_0"
	)
	if time.Now().UnixNano()%2 == 0 {
		return fmt.Sprintf(uaBase, uaWindows)
	}
	return fmt.Sprintf(uaBase, uaMac)
}
