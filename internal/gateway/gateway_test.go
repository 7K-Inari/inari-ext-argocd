package gateway

import (
	"bytes"
	"context"
	"net"
	"testing"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// captureServer implements the tunnel endpoint and records the inbound
// metadata + payload of the last call.
type captureServer struct {
	md  metadata.MD
	cmd *agentv1.InvokeAction
}

func (c *captureServer) desc() grpc.ServiceDesc {
	return grpc.ServiceDesc{
		ServiceName: "inari.extensions.v1.AgentGateway",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "InvokeAction",
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				cmd := &agentv1.InvokeAction{}
				if err := dec(cmd); err != nil {
					return nil, err
				}
				c.md, _ = metadata.FromIncomingContext(ctx)
				c.cmd = cmd
				return &agentv1.CommandAck{CommandId: cmd.GetCommandId(), Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}, nil
			},
		}},
	}
}

func dialCapture(t *testing.T) (*captureServer, *grpc.ClientConn) {
	t.Helper()
	cap := &captureServer{}
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	desc := cap.desc()
	s.RegisterService(&desc, cap)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return cap, conn
}

func invokeReq() *agentv1.InvokeAction {
	return &agentv1.InvokeAction{CommandId: "cmd-1", Action: "argocd.sync"}
}

func TestInvokeActionForwardsUserCredentialMetadata(t *testing.T) {
	cap, conn := dialCapture(t)
	c := New(conn)
	_, err := c.InvokeAction(context.Background(), Request{
		TenantID:  "tenant-acme",
		ClusterID: "cluster-1",
		Command:   invokeReq(),
		UserToken: "user-token-abc",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := cap.md.Get(MetadataUserCredential); len(got) != 1 || got[0] != "user-token-abc" {
		t.Fatalf("expected %s metadata to carry the user token, got %v", MetadataUserCredential, cap.md)
	}
}

func TestInvokeActionOmitsUserCredentialWhenEmpty(t *testing.T) {
	cap, conn := dialCapture(t)
	c := New(conn)
	_, err := c.InvokeAction(context.Background(), Request{
		TenantID:  "tenant-acme",
		ClusterID: "cluster-1",
		Command:   invokeReq(),
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := cap.md.Get(MetadataUserCredential); len(got) != 0 {
		t.Fatalf("expected no %s metadata without a user token, got %v", MetadataUserCredential, got)
	}
}

func TestUserTokenNeverEntersCommandPayload(t *testing.T) {
	cap, conn := dialCapture(t)
	c := New(conn)
	const token = "user-token-abc"
	_, err := c.InvokeAction(context.Background(), Request{
		TenantID:  "tenant-acme",
		ClusterID: "cluster-1",
		Command:   invokeReq(),
		UserToken: token,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	raw, err := proto.Marshal(cap.cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte(token)) {
		t.Fatalf("user token leaked into the persisted command payload: %q", raw)
	}
	if cap.cmd.GetUserCredentialRef() != "" {
		t.Fatalf("extension must not self-assert user_credential_ref, got %q", cap.cmd.GetUserCredentialRef())
	}
}

func TestLegacyGateTokenDisabledByDefault(t *testing.T) {
	cap, conn := dialCapture(t)
	// Even with a legacy token configured, it must not be sent unless the
	// legacy path is explicitly enabled.
	c := New(conn, WithLegacyGateToken("shared-gate-token"))
	_, err := c.InvokeAction(context.Background(), Request{
		TenantID:  "tenant-acme",
		ClusterID: "cluster-1",
		Command:   invokeReq(),
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := cap.md.Get(MetadataToken); len(got) != 0 {
		t.Fatalf("legacy gate token must be disabled by default, got %v", got)
	}
}

func TestLegacyGateTokenSentOnlyWhenExplicitlyEnabled(t *testing.T) {
	cap, conn := dialCapture(t)
	c := New(conn, WithLegacyGateToken("shared-gate-token"), WithLegacyGateTokenEnabled(true))
	_, err := c.InvokeAction(context.Background(), Request{
		TenantID:  "tenant-acme",
		ClusterID: "cluster-1",
		Command:   invokeReq(),
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := cap.md.Get(MetadataToken)
	if len(got) != 1 || got[0] != "shared-gate-token" {
		t.Fatalf("expected legacy gate token when explicitly enabled, got %v", got)
	}
}
