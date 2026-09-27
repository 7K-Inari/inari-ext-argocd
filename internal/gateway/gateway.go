// Package gateway is the extension-side client of the control plane's Agent
// Gateway: it tunnels imperative ArgoCD operations (plan §5.3) to the agent
// of a specific tenant cluster and waits for the agent's CommandAck.
//
// Wire contract (candidate for upstreaming into inari-api — see README
// "SDK gaps"): a single unary method
//
//	/inari.extensions.v1.AgentGateway/InvokeAction
//
// with the agentv1.InvokeAction command as the request body and the
// agentv1.CommandAck as the response body, encoded as protojson (content
// subtype "json"). Tenant and cluster routing travel as gRPC metadata:
//
//	x-inari-tenant:  <tenant id from the authenticated AuthContext>
//	x-inari-cluster: <cluster id from the action payload>
//
// The gateway fails closed: when the target agent is disconnected it returns
// UNAVAILABLE, which this client surfaces as pluginsdk.CodeUnavailable.
package gateway

import (
	"context"
	"crypto/tls"
	"fmt"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// InvokeMethod is the full gRPC method name of the Agent Gateway tunnel.
const InvokeMethod = "/inari.extensions.v1.AgentGateway/InvokeAction"

// Metadata keys used for tenant/cluster routing.
const (
	MetadataTenant  = "x-inari-tenant"
	MetadataCluster = "x-inari-cluster"
	// MetadataToken carries the shared extension-gateway gate token
	// (server: INARI_EXTENSION_GATEWAY_TOKEN; empty = endpoint disabled).
	//
	// Deprecated: legacy pre-W2 authentication. Per-user OIDC SSO sessions
	// (MetadataUserCredential) replaced it; the gate token is only sent when
	// explicitly enabled via WithLegacyGateTokenEnabled for rolling server
	// upgrades, and is never a fallback when no user session exists.
	MetadataToken = "x-inari-extension-token"
	// MetadataUserCredential carries the raw per-user downstream credential
	// (from the host-injected X-Inari-Downstream-Authorization) over the
	// in-memory extension→gateway hop. The control plane mints a vault
	// reference for it; the persisted command payload carries only that
	// reference. The token is never logged and never persisted here.
	MetadataUserCredential = "x-inari-user-credential"
)

// contentSubtype selects the protojson codec registered below.
const contentSubtype = "json"

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

// jsonCodec encodes protobuf messages as protojson so the tunnel contract can
// be served by any gRPC implementation without generated service stubs. This
// extension is the reference for that contract (see package doc).
type jsonCodec struct{}

func (jsonCodec) Name() string { return contentSubtype }

func (jsonCodec) Marshal(v any) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("gateway json codec: %T is not a proto.Message", v)
	}
	return protojson.Marshal(m)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	m, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("gateway json codec: %T is not a proto.Message", v)
	}
	return protojson.Unmarshal(data, m)
}

// Request is one tunneled imperative operation.
type Request struct {
	// TenantID is the authenticated tenant, taken from the plugin AuthContext
	// — never from caller-supplied payload.
	TenantID string
	// ClusterID is the target tenant cluster.
	ClusterID string
	// Command is the agentv1 command to deliver to the cluster's agent.
	Command *agentv1.InvokeAction
	// UserToken is the raw per-user downstream credential for this call,
	// forwarded only as MetadataUserCredential hop metadata — never inside
	// Command (the persisted payload) and never logged. Empty when the host
	// injected no user session; the control plane fails closed upstream in
	// that case.
	UserToken string
}

// Gateway delivers imperative commands to tenant-cluster agents via the
// control plane and returns their acknowledgement.
type Gateway interface {
	InvokeAction(ctx context.Context, req Request) (*agentv1.CommandAck, error)
}

// Config configures the gRPC Agent Gateway client.
type Config struct {
	// Addr is the control plane's Agent Gateway endpoint, e.g.
	// "inari-agent-gateway.inari-system:443" inside the platform cluster.
	Addr string
	// Insecure disables TLS (local development only).
	Insecure bool
	// TLSServerName overrides the TLS server name when set.
	TLSServerName string
	// LegacyToken is the deprecated shared extension-gateway gate token
	// (pre-W2 servers with INARI_EXTENSION_GATEWAY_TOKEN). It is sent only
	// when LegacyTokenEnabled is also set; there is no silent fallback to
	// shared credentials.
	LegacyToken string
	// LegacyTokenEnabled explicitly opts into the deprecated shared gate
	// token path for rolling server upgrades. Default false.
	LegacyTokenEnabled bool
}

// Client is a gRPC Gateway.
type Client struct {
	conn *grpc.ClientConn
	// legacy gate token (deprecated; only sent when legacyEnabled).
	legacyToken   string
	legacyEnabled bool
}

// Option customizes a Client.
type Option func(*Client)

// WithLegacyGateToken configures the deprecated shared extension-gateway
// gate token. It is inert unless WithLegacyGateTokenEnabled(true) is also
// passed. Deprecated: retained only for rolling upgrades from pre-W2
// control planes; per-user OIDC SSO sessions are the supported path.
func WithLegacyGateToken(token string) Option {
	return func(c *Client) { c.legacyToken = token }
}

// WithLegacyGateTokenEnabled explicitly enables the deprecated shared gate
// token path. Default false: the token is never sent.
func WithLegacyGateTokenEnabled(enabled bool) Option {
	return func(c *Client) { c.legacyEnabled = enabled }
}

// Dial connects to the control plane's Agent Gateway. The connection is
// established lazily; unreachable endpoints surface as CodeUnavailable at
// call time (fail closed).
func Dial(cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("gateway address is required (set INARI_AGENT_GATEWAY_ADDR)")
	}
	var creds credentials.TransportCredentials
	if cfg.Insecure {
		creds = insecure.NewCredentials()
	} else {
		creds = credentials.NewTLS(&tls.Config{ServerName: cfg.TLSServerName, MinVersion: tls.VersionTLS13})
	}
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial agent gateway: %w", err)
	}
	return New(conn,
		WithLegacyGateToken(cfg.LegacyToken),
		WithLegacyGateTokenEnabled(cfg.LegacyTokenEnabled),
	), nil
}

// New wraps an existing connection as a Gateway. Used by Dial and by tests /
// the e2e harness that already hold a *grpc.ClientConn (e.g. bufconn).
func New(conn *grpc.ClientConn, opts ...Option) *Client {
	c := &Client{conn: conn}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Close releases the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// InvokeAction tunnels one command and waits for the agent's CommandAck.
func (c *Client) InvokeAction(ctx context.Context, req Request) (*agentv1.CommandAck, error) {
	if req.TenantID == "" || req.ClusterID == "" {
		return nil, fmt.Errorf("tenant and cluster are required")
	}
	if req.Command == nil {
		return nil, fmt.Errorf("command is required")
	}
	ctx = metadata.AppendToOutgoingContext(ctx,
		MetadataTenant, req.TenantID,
		MetadataCluster, req.ClusterID,
	)
	if req.UserToken != "" {
		// Per-user credential: hop metadata only — never inside the
		// persisted command payload, never logged.
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataUserCredential, req.UserToken)
	}
	if c.legacyEnabled && c.legacyToken != "" {
		// Deprecated pre-W2 shared gate token; opt-in only.
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataToken, c.legacyToken)
	}
	ack := &agentv1.CommandAck{}
	err := c.conn.Invoke(ctx, InvokeMethod, req.Command, ack, grpc.CallContentSubtype(contentSubtype))
	if err != nil {
		return nil, err
	}
	return ack, nil
}
