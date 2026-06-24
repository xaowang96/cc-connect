package core

import (
	"path/filepath"
	"testing"
)

func TestProjectState_SaveLoadAndClear(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")

	store := NewProjectStateStore(statePath)
	store.SetWorkDirOverride("/tmp/demo")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkDirOverride(); got != "/tmp/demo" {
		t.Fatalf("WorkDirOverride() = %q, want %q", got, "/tmp/demo")
	}

	reloaded.ClearWorkDirOverride()
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkDirOverride(); got != "" {
		t.Fatalf("WorkDirOverride() after clear = %q, want empty", got)
	}
}

func TestWorkspaceDirOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	workspaceA := "/tmp/workspace-a"
	workspaceB := "/tmp/workspace-b"

	store := NewProjectStateStore(statePath)
	store.SetWorkDirOverride("/tmp/global")
	store.SetWorkspaceDirOverride(workspaceA, "/tmp/workspace-a/override")
	store.SetWorkspaceDirOverride(workspaceB, "/tmp/workspace-b/override")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkDirOverride(); got != "/tmp/global" {
		t.Fatalf("WorkDirOverride() = %q, want %q", got, "/tmp/global")
	}
	if got := reloaded.WorkspaceDirOverride(workspaceA); got != "/tmp/workspace-a/override" {
		t.Fatalf("WorkspaceDirOverride(%q) = %q, want %q", workspaceA, got, "/tmp/workspace-a/override")
	}
	if got := reloaded.WorkspaceDirOverride(workspaceB); got != "/tmp/workspace-b/override" {
		t.Fatalf("WorkspaceDirOverride(%q) = %q, want %q", workspaceB, got, "/tmp/workspace-b/override")
	}
	if got := reloaded.WorkspaceDirOverride("/tmp/missing"); got != "" {
		t.Fatalf("WorkspaceDirOverride(missing) = %q, want empty", got)
	}

	reloaded.ClearWorkspaceDirOverride(workspaceA)
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkDirOverride(); got != "/tmp/global" {
		t.Fatalf("WorkDirOverride() after workspace clear = %q, want %q", got, "/tmp/global")
	}
	if got := cleared.WorkspaceDirOverride(workspaceA); got != "" {
		t.Fatalf("WorkspaceDirOverride(%q) after clear = %q, want empty", workspaceA, got)
	}
	if got := cleared.WorkspaceDirOverride(workspaceB); got != "/tmp/workspace-b/override" {
		t.Fatalf("WorkspaceDirOverride(%q) after clearing other workspace = %q, want %q", workspaceB, got, "/tmp/workspace-b/override")
	}
}

func TestWorkspaceModelOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	workspaceA := "/tmp/workspace-a"
	workspaceB := "/tmp/workspace-b"

	store := NewProjectStateStore(statePath)
	store.SetWorkspaceModelOverride(workspaceA, "opus")
	store.SetWorkspaceModelOverride(workspaceB, "sonnet")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkspaceModelOverride(workspaceA); got != "opus" {
		t.Fatalf("WorkspaceModelOverride(%q) = %q, want %q", workspaceA, got, "opus")
	}
	if got := reloaded.WorkspaceModelOverride(workspaceB); got != "sonnet" {
		t.Fatalf("WorkspaceModelOverride(%q) = %q, want %q", workspaceB, got, "sonnet")
	}

	reloaded.ClearWorkspaceModelOverride(workspaceA)
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkspaceModelOverride(workspaceA); got != "" {
		t.Fatalf("WorkspaceModelOverride(%q) after clear = %q, want empty", workspaceA, got)
	}
	if got := cleared.WorkspaceModelOverride(workspaceB); got != "sonnet" {
		t.Fatalf("WorkspaceModelOverride(%q) after clearing other workspace = %q, want %q", workspaceB, got, "sonnet")
	}
}

func TestRegression_ProjectStateStore_AgentBindingsSurviveRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	channelKey := "feishu:oc_123"

	store := NewProjectStateStore(statePath)
	store.SetAgentBinding(channelKey, "codex")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.AgentBinding(channelKey); got != "codex" {
		t.Fatalf("AgentBinding(%q) after restart = %q, want %q", channelKey, got, "codex")
	}
	bindings := reloaded.ListAgentBindings()
	if len(bindings) != 1 {
		t.Fatalf("ListAgentBindings() after restart has %d entries, want 1", len(bindings))
	}
	if got := bindings[channelKey]; got != "codex" {
		t.Fatalf("ListAgentBindings()[%q] = %q, want %q", channelKey, got, "codex")
	}

	reloaded.ClearAgentBinding(channelKey)
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.AgentBinding(channelKey); got != "" {
		t.Fatalf("AgentBinding(%q) after clear = %q, want empty", channelKey, got)
	}
	if bindings := cleared.ListAgentBindings(); len(bindings) != 0 {
		t.Fatalf("ListAgentBindings() after clear has %d entries, want 0", len(bindings))
	}
}
