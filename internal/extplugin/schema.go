package extplugin

// JSON Schemas for action inputs/outputs. The control plane uses these to
// validate calls and to render forms (see pluginsdk.Action).

const appRefSchema = `{
  "type": "object",
  "required": ["name", "namespace", "project"],
  "properties": {
    "name":      {"type": "string", "description": "ArgoCD Application name"},
    "namespace": {"type": "string", "description": "Namespace of the Application (usually argocd)"},
    "project":   {"type": "string", "description": "AppProject; must be managed by Inari"}
  },
  "additionalProperties": false
}`

var resultSchema = []byte(`{
  "type": "object",
  "required": ["commandId", "outcome"],
  "properties": {
    "commandId": {"type": "string"},
    "outcome":   {"type": "string", "enum": ["applied", "accepted"]},
    "message":   {"type": "string"}
  },
  "additionalProperties": false
}`)

func schema(action string) []byte {
	switch action {
	case "sync":
		return []byte(`{
  "type": "object",
  "required": ["clusterId", "app"],
  "properties": {
    "clusterId": {"type": "string"},
    "app": ` + appRefSchema + `,
    "prune":    {"type": "boolean", "default": false},
    "dryRun":   {"type": "boolean", "default": false},
    "strategy": {"type": "string", "enum": ["apply", "hook"]}
  },
  "additionalProperties": false
}`)
	case "refresh":
		return []byte(`{
  "type": "object",
  "required": ["clusterId", "app"],
  "properties": {
    "clusterId": {"type": "string"},
    "app": ` + appRefSchema + `,
    "hard": {"type": "boolean", "default": false}
  },
  "additionalProperties": false
}`)
	case "rollback":
		return []byte(`{
  "type": "object",
  "required": ["clusterId", "app", "revisionId"],
  "properties": {
    "clusterId":  {"type": "string"},
    "app": ` + appRefSchema + `,
    "revisionId": {"type": "integer", "minimum": 1, "description": "ArgoCD deployment history ID"},
    "prune":      {"type": "boolean", "default": false},
    "dryRun":     {"type": "boolean", "default": false}
  },
  "additionalProperties": false
}`)
	case "resource-action":
		return []byte(`{
  "type": "object",
  "required": ["clusterId", "app", "resource", "action"],
  "properties": {
    "clusterId": {"type": "string"},
    "app": ` + appRefSchema + `,
    "resource": {
      "type": "object",
      "required": ["kind", "name"],
      "properties": {
        "group":     {"type": "string"},
        "kind":      {"type": "string"},
        "name":      {"type": "string"},
        "namespace": {"type": "string"}
      },
      "additionalProperties": false
    },
    "action": {"type": "string", "description": "Built-in or custom Lua resource action name", "pattern": "^[a-z][a-z0-9-]{0,63}$"},
    "params": {"type": "object", "description": "Action-specific parameters"}
  },
  "additionalProperties": false
}`)
	}
	return nil
}
