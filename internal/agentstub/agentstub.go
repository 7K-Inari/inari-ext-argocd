// Package agentstub is the reference executor for tunneled InvokeAction
// commands: it translates each command into a call against the tenant-local
// ArgoCD API, scoped to Inari-managed AppProjects, and answers with a
// CommandAck. It exists so the extension can be tested and demonstrated
// end-to-end (e2e/ and e2e/kind) before/independently of the real
// inari-agent, and documents the agent-side contract third parties can
// inspect. Fail closed: unmanaged projects and unreachable ArgoCD yield
// COMMAND_RESULT_FAILED, never a partial mutation.
package agentstub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
)

// ArgoCDClient talks to one tenant-local ArgoCD API.
type ArgoCDClient struct {
	// BaseURL e.g. "https://argocd-server.argocd.svc:443".
	BaseURL string
	// BearerToken, if the tenant-local ArgoCD requires one.
	BearerToken string
	// ManagedProjects restricts execution to Inari-managed AppProjects.
	ManagedProjects []string
	// HTTPClient is injectable for tests; defaults to a 30s-timeout client.
	HTTPClient *http.Client
}

func (c *ArgoCDClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *ArgoCDClient) managed(project string) bool {
	for _, p := range c.ManagedProjects {
		if p == project {
			return true
		}
	}
	return false
}

// Execute runs one InvokeAction against the tenant-local ArgoCD API and
// builds the CommandAck. Errors become COMMAND_RESULT_FAILED acks.
func (c *ArgoCDClient) Execute(ctx context.Context, cmd *agentv1.InvokeAction) *agentv1.CommandAck {
	ack := &agentv1.CommandAck{CommandId: cmd.GetCommandId(), Result: agentv1.CommandResult_COMMAND_RESULT_FAILED}
	if cmd.GetTimeout() != nil && cmd.GetTimeout().AsDuration() > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.GetTimeout().AsDuration())
		defer cancel()
	}
	if err := c.execute(ctx, cmd); err != nil {
		ack.Message = err.Error()
		return ack
	}
	ack.Result = agentv1.CommandResult_COMMAND_RESULT_APPLIED
	ack.Message = fmt.Sprintf("%s applied", cmd.GetAction())
	return ack
}

type appParams struct {
	Name      string
	Namespace string
	Project   string
}

func params(cmd *agentv1.InvokeAction) appParams {
	app := cmd.GetParameters().GetFields()["app"].GetStructValue()
	return appParams{
		Name:      app.GetFields()["name"].GetStringValue(),
		Namespace: app.GetFields()["namespace"].GetStringValue(),
		Project:   app.GetFields()["project"].GetStringValue(),
	}
}

func (c *ArgoCDClient) execute(ctx context.Context, cmd *agentv1.InvokeAction) error {
	p := params(cmd)
	if p.Name == "" || p.Project == "" {
		return fmt.Errorf("missing app identity in parameters")
	}
	if !c.managed(p.Project) {
		return fmt.Errorf("project %q is not managed by Inari; refusing imperative op", p.Project)
	}
	fields := cmd.GetParameters().GetFields()
	switch cmd.GetAction() {
	case "sync":
		body := map[string]any{
			"prune":  fields["prune"].GetBoolValue(),
			"dryRun": fields["dryRun"].GetBoolValue(),
		}
		if s := fields["strategy"].GetStringValue(); s != "" {
			body["strategy"] = map[string]any{s: map[string]any{}}
		}
		return c.do(ctx, http.MethodPost,
			fmt.Sprintf("/api/v1/applications/%s/sync", url.PathEscape(p.Name)), body)
	case "refresh":
		refresh := "normal"
		if fields["hard"].GetBoolValue() {
			refresh = "hard"
		}
		return c.do(ctx, http.MethodGet,
			fmt.Sprintf("/api/v1/applications/%s?refresh=%s", url.PathEscape(p.Name), refresh), nil)
	case "rollback":
		return c.do(ctx, http.MethodPost,
			fmt.Sprintf("/api/v1/applications/%s/rollback", url.PathEscape(p.Name)), map[string]any{
				"id":     rollbackID(fields),
				"prune":  fields["prune"].GetBoolValue(),
				"dryRun": fields["dryRun"].GetBoolValue(),
			})
	case "resource-action":
		res := fields["resource"].GetStructValue().GetFields()
		q := url.Values{}
		q.Set("kind", res["kind"].GetStringValue())
		q.Set("resourceName", res["name"].GetStringValue())
		if ns := res["namespace"].GetStringValue(); ns != "" {
			q.Set("namespace", ns)
		}
		if g := res["group"].GetStringValue(); g != "" {
			q.Set("group", g)
		}
		if v := res["version"].GetStringValue(); v != "" {
			q.Set("version", v)
		}
		// ArgoCD expects the request body to be the action name itself,
		// encoded as a JSON string — not an object.
		return c.do(ctx, http.MethodPost,
			fmt.Sprintf("/api/v1/applications/%s/resource/actions?%s", url.PathEscape(p.Name), q.Encode()),
			fields["resourceAction"].GetStringValue())
	default:
		return fmt.Errorf("unsupported action %q", cmd.GetAction())
	}
}

func (c *ArgoCDClient) do(ctx context.Context, method, path string, body any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.BearerToken != "" {
		req.Header.Set("authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("argocd api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("argocd api %s %s: %s: %s", method, path, resp.Status, string(b))
	}
	return nil
}

// rollbackID accepts the bare "id" (agent contract) with a legacy
// "revisionId" fallback for older plugins.
func rollbackID(fields map[string]*structpb.Value) float64 {
	if v, ok := fields["id"]; ok && v.GetNumberValue() != 0 {
		return v.GetNumberValue()
	}
	return fields["revisionId"].GetNumberValue()
}
