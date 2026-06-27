package opencodegoquota

import (
	"encoding/json"
	"regexp"
	"strings"
)

var workspaceIDPattern = regexp.MustCompile(`wrk_[A-Za-z0-9]+`)

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func ExtractWorkspaceID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if match := workspaceIDPattern.FindString(raw); match != "" {
		return match, true
	}
	return "", false
}

func ResolveWorkspaceID(raw string) (string, error) {
	if id, ok := ExtractWorkspaceID(raw); ok {
		return id, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "default") {
		return "", newError(
			ErrorWorkspaceLookupFailed,
			"workspace id is required; open the OpenCode Go dashboard and copy the wrk_xxx value from the URL",
			nil,
		)
	}
	return "", newError(
		ErrorWorkspaceLookupFailed,
		"unable to resolve workspace name; use a wrk_xxx workspace id or an OpenCode Go dashboard URL",
		nil,
	)
}

func ResolveWorkspaceIDFromList(raw string, workspaces []Workspace) (string, error) {
	if id, ok := ExtractWorkspaceID(raw); ok {
		return id, nil
	}
	if len(workspaces) == 0 {
		return "", newError(
			ErrorWorkspaceLookupFailed,
			"unable to resolve workspace; OpenCode returned no workspaces",
			nil,
		)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "default") {
		return workspaces[0].ID, nil
	}
	for _, workspace := range workspaces {
		if strings.EqualFold(strings.TrimSpace(workspace.Name), raw) {
			return workspace.ID, nil
		}
	}
	return "", newError(
		ErrorWorkspaceLookupFailed,
		"unable to resolve workspace name; use a wrk_xxx workspace id or an OpenCode Go dashboard URL",
		nil,
	)
}

func ParseWorkspaceList(text string) []Workspace {
	workspaces := parseWorkspaceListFromJSON(text)
	if len(workspaces) > 0 {
		return workspaces
	}
	return parseWorkspaceListFromText(text)
}

func parseWorkspaceListFromJSON(text string) []Workspace {
	var object any
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		return nil
	}
	var out []Workspace
	collectWorkspacesFromJSON(object, &out)
	return out
}

func collectWorkspacesFromJSON(object any, out *[]Workspace) {
	switch typed := object.(type) {
	case map[string]any:
		id, _ := typed["id"].(string)
		if strings.HasPrefix(id, "wrk_") {
			workspace := Workspace{ID: id}
			if name, ok := typed["name"].(string); ok {
				workspace.Name = strings.TrimSpace(name)
			}
			appendWorkspace(out, workspace)
		}
		for _, value := range typed {
			collectWorkspacesFromJSON(value, out)
		}
	case []any:
		for _, value := range typed {
			collectWorkspacesFromJSON(value, out)
		}
	}
}

func parseWorkspaceListFromText(text string) []Workspace {
	var out []Workspace
	objectPattern := regexp.MustCompile(`\{[^{}]*"?id"?\s*:\s*"(wrk_[^"]+)"[^{}]*\}`)
	for _, match := range objectPattern.FindAllStringSubmatch(text, -1) {
		if len(match) != 2 {
			continue
		}
		objectText := match[0]
		workspace := Workspace{ID: match[1], Name: extractWorkspaceName(objectText)}
		appendWorkspace(&out, workspace)
	}
	if len(out) > 0 {
		return out
	}
	for _, id := range workspaceIDPattern.FindAllString(text, -1) {
		appendWorkspace(&out, Workspace{ID: id})
	}
	return out
}

func extractWorkspaceName(text string) string {
	namePattern := regexp.MustCompile(`"?(?:name|displayName|title)"?\s*:\s*"([^"]*)"`)
	match := namePattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func appendWorkspace(out *[]Workspace, workspace Workspace) {
	workspace.ID = strings.TrimSpace(workspace.ID)
	if workspace.ID == "" {
		return
	}
	for _, existing := range *out {
		if existing.ID == workspace.ID {
			return
		}
	}
	*out = append(*out, workspace)
}
