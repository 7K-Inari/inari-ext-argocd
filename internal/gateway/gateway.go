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
	MetadataToken = "x-inari-extension-token"
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
	// Token is the shared extension-gateway gate token, sent as
	// x-inari-extension-token. Optional: only needed when the control plane
	// has INARI_EXTENSION_GATEWAY_TOKEN configured.
	Token string
}

// Client is a gRPC Gateway.
type Client struct {
	conn  *grpc.ClientConn
	token string
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
	return &Client{conn: conn, token: cfg.Token}, nil
}

// New wraps an existing connection as a Gateway. Used by Dial and by tests /
// the e2e harness that already hold a *grpc.ClientConn (e.g. bufconn).
func New(conn *grpc.ClientConn) *Client { return &Client{conn: conn} }

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
	if c.token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataToken, c.token)
	}
	ack := &agentv1.CommandAck{}
	err := c.conn.Invoke(ctx, InvokeMethod, req.Command, ack, grpc.CallContentSubtype(contentSubtype))
	if err != nil {
		return nil, err
	}
	return ack, nil
}
