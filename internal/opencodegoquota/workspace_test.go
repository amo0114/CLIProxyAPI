package opencodegoquota

import "testing"

func TestResolveWorkspaceIDFromList(t *testing.T) {
	workspaces := []Workspace{
		{ID: "wrk_first", Name: "Default Workspace"},
		{ID: "wrk_second", Name: "Work"},
	}

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "default", raw: "Default", want: "wrk_first"},
		{name: "empty", raw: " ", want: "wrk_first"},
		{name: "name", raw: "work", want: "wrk_second"},
		{name: "url", raw: "https://opencode.ai/workspace/wrk_url/go", want: "wrk_url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkspaceIDFromList(tt.raw, workspaces)
			if err != nil {
				t.Fatalf("ResolveWorkspaceIDFromList() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveWorkspaceIDFromList() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseWorkspaceList(t *testing.T) {
	text := `1:[{id:"wrk_abc",name:"Personal"},{id:"wrk_def",displayName:"Team"}]`
	got := ParseWorkspaceList(text)
	if len(got) != 2 {
		t.Fatalf("workspace count = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "wrk_abc" || got[0].Name != "Personal" {
		t.Fatalf("first workspace = %+v", got[0])
	}
	if got[1].ID != "wrk_def" || got[1].Name != "Team" {
		t.Fatalf("second workspace = %+v", got[1])
	}
}

func TestParseWorkspaceListJSON(t *testing.T) {
	text := `{"data":{"workspaces":[{"id":"wrk_abc","name":"Personal"}]}}`
	got := ParseWorkspaceList(text)
	if len(got) != 1 {
		t.Fatalf("workspace count = %d, want 1: %+v", len(got), got)
	}
	if got[0].ID != "wrk_abc" || got[0].Name != "Personal" {
		t.Fatalf("workspace = %+v", got[0])
	}
}
