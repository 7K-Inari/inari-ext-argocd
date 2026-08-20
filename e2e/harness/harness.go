// Package harness is a test/e2e implementation of the control plane's Agent
// Gateway tunnel contract (see internal/gateway): it accepts InvokeAction
// calls from the plugin, routes them by x-inari-cluster metadata to a
// registered agent session, and relays the CommandAck. It stands in for the
// M4-W2 server extension host's gateway until that component ships the same
// endpoint.
package harness

import (
	"context"
	"sync"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

// Session is one connected agent: commands in, acks out.
type Session struct {
	Commands chan *agentv1.InvokeAction
	acks     map[string]chan *agentv1.CommandAck
	mu       sync.Mutex
}

func newSession() *Session {
	return &Session{Commands: make(chan *agentv1.InvokeAction, 16), acks: map[string]chan *agentv1.CommandAck{}}
}

// Ack delivers the agent's acknowledgement for a command.
func (s *Session) Ack(ack *agentv1.CommandAck) {
	s.mu.Lock()
	ch, ok := s.acks[ack.GetCommandId()]
	s.mu.Unlock()
	if ok {
		ch <- ack
	}
}

// Server implements the Agent Gateway endpoint.
type Server struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// AgentTimeout bounds the wait for an agent's ack.
	AgentTimeout time.Duration
}

// NewServer builds a gateway server.
func NewServer() *Server {
	return &Server{AgentTimeout: 30 * time.Second, sessions: map[string]*Session{}}
}

// RegisterAgent connects an agent session for clusterID; the returned cancel
// function disconnects it (subsequent invokes fail closed with UNAVAILABLE).
func (s *Server) RegisterAgent(clusterID string) (*Session, func()) {
	sess := newSession()
	s.mu.Lock()
	s.sessions[clusterID] = sess
	s.mu.Unlock()
	return sess, func() {
		s.mu.Lock()
		delete(s.sessions, clusterID)
		s.mu.Unlock()
	}
}

// ServiceDesc is the hand-written gRPC service definition for the tunnel
// contract; registering it on any *grpc.Server serves the endpoint the
// plugin's gateway.Client dials.
func (s *Server) ServiceDesc() grpc.ServiceDesc {
	return grpc.ServiceDesc{
		ServiceName: "inari.extensions.v1.AgentGateway",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "InvokeAction",
			Handler:    s.invokeHandler,
		}},
		Streams:  []grpc.StreamDesc{},
		Metadata: "inari/extensions/v1/agent_gateway.proto",
	}
}

func (s *Server) invokeHandler(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	cmd := &agentv1.InvokeAction{}
	if err := dec(cmd); err != nil {
		return nil, err
	}
	info := &grpc.UnaryServerInfo{FullMethod: gateway.InvokeMethod}
	handler := func(ctx context.Context, req any) (any, error) {
		return s.invoke(ctx, req.(*agentv1.InvokeAction))
	}
	if interceptor != nil {
		return interceptor(ctx, cmd, info, handler)
	}
	return handler(ctx, cmd)
}

func (s *Server) invoke(ctx context.Context, cmd *agentv1.InvokeAction) (*agentv1.CommandAck, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	cluster := first(md[gateway.MetadataCluster])
	tenant := first(md[gateway.MetadataTenant])
	if cluster == "" || tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "x-inari-tenant and x-inari-cluster metadata are required")
	}

	s.mu.Lock()
	sess, ok := s.sessions[cluster]
	s.mu.Unlock()
	if !ok {
		// Fail closed: agent disconnected.
		return nil, status.Errorf(codes.Unavailable, "no connected agent for cluster %q (tenant %q)", cluster, tenant)
	}

	ch := make(chan *agentv1.CommandAck, 1)
	sess.mu.Lock()
	sess.acks[cmd.GetCommandId()] = ch
	sess.mu.Unlock()
	defer func() {
		sess.mu.Lock()
		delete(sess.acks, cmd.GetCommandId())
		sess.mu.Unlock()
	}()

	select {
	case sess.Commands <- cmd:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case ack := <-ch:
		return ack, nil
	case <-time.After(s.AgentTimeout):
		return nil, status.Errorf(codes.DeadlineExceeded, "agent did not ack command %q", cmd.GetCommandId())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RunAgent drains the session's commands, executes each against ArgoCD, and
// acks. executor is typically (&agentstub.ArgoCDClient{...}).Execute.
func RunAgent(ctx context.Context, sess *Session, executor func(context.Context, *agentv1.InvokeAction) *agentv1.CommandAck) {
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-sess.Commands:
			sess.Ack(executor(ctx, cmd))
		}
	}
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
