// Package argocd defines the action payloads accepted by this extension and
// translates them into agentv1.InvokeAction commands for the tenant-local
// ArgoCD API (plan §5.3). All validation happens here, before anything is
// sent to the agent; the agent independently re-scopes execution to
// Inari-managed resources.
package argocd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Action names registered by the plugin. These are also the action values
// carried in agentv1.InvokeAction.action.
const (
	ActionSync           = "argocd.sync"
	ActionRefresh        = "argocd.refresh"
	ActionRollback       = "argocd.rollback"
	ActionResourceAction = "argocd.resource-action"
)

// DefaultTimeout bounds one imperative round-trip through the agent.
const DefaultTimeout = 60 * time.Second

var (
	dns1123 = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	// Lua resource action names in ArgoCD are lowercase/dashed identifiers.
	luaAction = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// AppRef identifies the ArgoCD Application an action targets.
type AppRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// Project must be one of the extension's managed projects (see
	// Scoping.ManagedProjects); this is the extension-side enforcement that
	// only Inari-managed Applications are touched (plan §5.3).
	Project string `json:"project"`
}

func (a AppRef) validate() error {
	if !dns1123.MatchString(a.Name) {
		return fmt.Errorf("invalid application name %q", a.Name)
	}
	if !dns1123.MatchString(a.Namespace) {
		return fmt.Errorf("invalid application namespace %q", a.Namespace)
	}
	if a.Project == "" {
		return fmt.Errorf("application project is required")
	}
	if !dns1123.MatchString(a.Project) {
		return fmt.Errorf("invalid application project %q", a.Project)
	}
	return nil
}

// ResourceRef identifies a resource inside an Application for Lua resource
// actions.
type ResourceRef struct {
	Group string `json:"group,omitempty"`
	// Version is the resource API version (e.g. "v1"); ArgoCD's resource
	// lookup matches on group/kind/version, so set it for grouped resources.
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// SyncInput is the payload of argocd.sync.
type SyncInput struct {
	ClusterID string `json:"clusterId"`
	App       AppRef `json:"app"`
	Prune     bool   `json:"prune,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
	// Strategy is "hook" or "" (default apply).
	Strategy string `json:"strategy,omitempty"`
}

// RefreshInput is the payload of argocd.refresh.
type RefreshInput struct {
	ClusterID string `json:"clusterId"`
	App       AppRef `json:"app"`
	// Hard invalidates cached manifests when true.
	Hard bool `json:"hard,omitempty"`
}

// RollbackInput is the payload of argocd.rollback.
type RollbackInput struct {
	ClusterID string `json:"clusterId"`
	App       AppRef `json:"app"`
	// RevisionID is the ArgoCD deployment history ID to roll back to.
	RevisionID int64 `json:"revisionId"`
	Prune      bool  `json:"prune,omitempty"`
	DryRun     bool  `json:"dryRun,omitempty"`
}

// ResourceActionInput is the payload of argocd.resource-action: a built-in or
// custom Lua resource action on one resource of an Application.
type ResourceActionInput struct {
	ClusterID string         `json:"clusterId"`
	App       AppRef         `json:"app"`
	Resource  ResourceRef    `json:"resource"`
	Action    string         `json:"action"`
	Params    map[string]any `json:"params,omitempty"`
}

// Scoping constrains which Applications this extension will act on. The
// control plane enforces RBAC per caller; this is the defense-in-depth layer
// that keeps imperative ops inside Inari-managed ArgoCD projects.
type Scoping struct {
	// ManagedProjects is the allowlist of ArgoCD AppProjects Inari manages.
	// Requests for any other project are rejected before reaching the agent.
	ManagedProjects []string
}

func (s Scoping) allows(project string) bool {
	for _, p := range s.ManagedProjects {
		if p == project {
			return true
		}
	}
	return false
}

// Command is the validated, agent-ready form of an action request.
type Command struct {
	Action string
	Cmd    *agentv1.InvokeAction
}

func baseParams(app AppRef) map[string]any {
	return map[string]any{
		"app": map[string]any{
			"name":      app.Name,
			"namespace": app.Namespace,
			"project":   app.Project,
		},
	}
}

func buildCommand(commandID string, action string, timeout time.Duration, params map[string]any) (*agentv1.InvokeAction, error) {
	p, err := structpb.NewStruct(params)
	if err != nil {
		return nil, fmt.Errorf("encode parameters: %w", err)
	}
	// The agent-side allow-list uses bare verbs (sync|refresh|rollback);
	// the extension's public action names are namespaced (argocd.*).
	return &agentv1.InvokeAction{
		CommandId:  commandID,
		Action:     strings.TrimPrefix(action, "argocd."),
		Parameters: p,
		Timeout:    durationpb.New(timeout),
	}, nil
}

// BuildSync validates in and builds the sync command.
func BuildSync(commandID string, in SyncInput, sc Scoping, timeout time.Duration) (*agentv1.InvokeAction, error) {
	if err := validateCommon(in.ClusterID, in.App, sc); err != nil {
		return nil, err
	}
	if in.Strategy != "" && in.Strategy != "hook" && in.Strategy != "apply" {
		return nil, fmt.Errorf("invalid sync strategy %q", in.Strategy)
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	p := baseParams(in.App)
	p["prune"] = in.Prune
	p["dryRun"] = in.DryRun
	if in.Strategy != "" {
		p["strategy"] = in.Strategy
	}
	return buildCommand(commandID, ActionSync, timeout, p)
}

// BuildRefresh validates in and builds the refresh command.
func BuildRefresh(commandID string, in RefreshInput, sc Scoping, timeout time.Duration) (*agentv1.InvokeAction, error) {
	if err := validateCommon(in.ClusterID, in.App, sc); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	p := baseParams(in.App)
	p["hard"] = in.Hard
	return buildCommand(commandID, ActionRefresh, timeout, p)
}

// BuildRollback validates in and builds the rollback command.
func BuildRollback(commandID string, in RollbackInput, sc Scoping, timeout time.Duration) (*agentv1.InvokeAction, error) {
	if err := validateCommon(in.ClusterID, in.App, sc); err != nil {
		return nil, err
	}
	if in.RevisionID <= 0 {
		return nil, fmt.Errorf("revisionId must be a positive deployment history ID")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	p := baseParams(in.App)
	p["revisionId"] = in.RevisionID
	p["prune"] = in.Prune
	p["dryRun"] = in.DryRun
	return buildCommand(commandID, ActionRollback, timeout, p)
}

// BuildResourceAction validates in and builds the Lua resource action command.
func BuildResourceAction(commandID string, in ResourceActionInput, sc Scoping, timeout time.Duration) (*agentv1.InvokeAction, error) {
	if err := validateCommon(in.ClusterID, in.App, sc); err != nil {
		return nil, err
	}
	if in.Resource.Kind == "" || in.Resource.Name == "" {
		return nil, fmt.Errorf("resource kind and name are required")
	}
	if !luaAction.MatchString(in.Action) {
		return nil, fmt.Errorf("invalid resource action name %q", in.Action)
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	p := baseParams(in.App)
	res := map[string]any{
		"kind":      in.Resource.Kind,
		"name":      in.Resource.Name,
		"namespace": in.Resource.Namespace,
	}
	if in.Resource.Group != "" {
		res["group"] = in.Resource.Group
	}
	if in.Resource.Version != "" {
		res["version"] = in.Resource.Version
	}
	p["resource"] = res
	p["resourceAction"] = in.Action
	if len(in.Params) > 0 {
		p["actionParams"] = in.Params
	}
	return buildCommand(commandID, ActionResourceAction, timeout, p)
}

func validateCommon(clusterID string, app AppRef, sc Scoping) error {
	if clusterID == "" {
		return fmt.Errorf("clusterId is required")
	}
	if err := app.validate(); err != nil {
		return err
	}
	if !sc.allows(app.Project) {
		return fmt.Errorf("project %q is not managed by Inari", app.Project)
	}
	return nil
}
