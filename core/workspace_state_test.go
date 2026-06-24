package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspacePool_GetOrCreate(t *testing.T) {
	pool := newWorkspacePool(15 * time.Minute)

	state1 := pool.GetOrCreate("test", "/workspace/a")
	state2 := pool.GetOrCreate("test", "/workspace/a")
	state3 := pool.GetOrCreate("test", "/workspace/b")

	if state1 != state2 {
		t.Error("expected same state for same workspace")
	}
	if state1 == state3 {
		t.Error("expected different state for different workspace")
	}
}

func TestWorkspacePool_Touch(t *testing.T) {
	pool := newWorkspacePool(15 * time.Minute)
	state := pool.GetOrCreate("test", "/workspace/a")

	before := state.LastActivity()
	time.Sleep(10 * time.Millisecond)
	state.Touch()
	after := state.LastActivity()

	if !after.After(before) {
		t.Error("expected lastActivity to advance after Touch()")
	}
}

func TestWorkspaceState_BeginEndTurn(t *testing.T) {
	state := newWorkspaceState("test", "/workspace/a")

	before := state.LastActivity()
	time.Sleep(10 * time.Millisecond)
	state.BeginTurn()
	if !state.HasActiveTurn() {
		t.Fatal("expected workspace to report an active turn after BeginTurn")
	}
	if !state.LastActivity().After(before) {
		t.Fatal("expected lastActivity to advance on BeginTurn")
	}

	time.Sleep(10 * time.Millisecond)
	mid := state.LastActivity()
	state.EndTurn()
	if state.HasActiveTurn() {
		t.Fatal("expected workspace to report no active turns after EndTurn")
	}
	if !state.LastActivity().After(mid) {
		t.Fatal("expected lastActivity to advance on EndTurn")
	}
}

func TestWorkspacePool_ReapIdle(t *testing.T) {
	pool := newWorkspacePool(50 * time.Millisecond)
	pool.GetOrCreate("test", normalizeWorkspacePath("/workspace/a"))

	time.Sleep(100 * time.Millisecond)
	reaped := pool.ReapIdle()

	wantWS := normalizeWorkspacePath("/workspace/a")
	if len(reaped) != 1 || reaped[0].workspace != wantWS {
		t.Errorf("expected [%s] reaped, got %v", wantWS, reaped)
	}

	if s := pool.Get("test", normalizeWorkspacePath("/workspace/a")); s != nil {
		t.Error("expected workspace removed after reap")
	}
}

func TestNormalizeWorkspacePath(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real-project")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(tmp, "link-project")
	if err := os.Symlink(realDir, symlink); err != nil {
		t.Skip("symlinks not supported")
	}

	// Resolve the expected path through EvalSymlinks so that the test works
	// on macOS where /var is a symlink to /private/var.
	resolvedRealDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trailing slash", realDir + "/", resolvedRealDir},
		{"double slash", filepath.Join(tmp, "real-project") + "//", resolvedRealDir},
		{"dot segment", filepath.Join(tmp, ".", "real-project"), resolvedRealDir},
		{"dotdot segment", filepath.Join(tmp, "real-project", "subdir", ".."), resolvedRealDir},
		{"symlink resolved", symlink, resolvedRealDir},
		{"nonexistent uses Clean only", "/nonexistent/path/./foo/../bar", "/nonexistent/path/bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeWorkspacePath(tt.input)
			if got != tt.want {
				t.Errorf("normalizeWorkspacePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeBeforePoolProducesSameKey(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "project")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pool := newWorkspacePool(15 * time.Minute)

	ws1 := pool.GetOrCreate("test", normalizeWorkspacePath(realDir+"/"))
	ws2 := pool.GetOrCreate("test", normalizeWorkspacePath(realDir))

	if ws1 != ws2 {
		t.Error("normalized trailing slash produced a different workspace state")
	}
}

func TestWorkspacePool_ReapIdle_KeepsActive(t *testing.T) {
	pool := newWorkspacePool(200 * time.Millisecond)
	state := pool.GetOrCreate("test", "/workspace/active")

	time.Sleep(100 * time.Millisecond)
	state.Touch()

	reaped := pool.ReapIdle()
	if len(reaped) != 0 {
		t.Errorf("expected no reaping for active workspace, got %v", reaped)
	}

	if s := pool.Get("test", "/workspace/active"); s == nil {
		t.Error("expected active workspace to still exist")
	}
}

func TestWorkspacePool_ReapIdle_SkipsBusyWorkspace(t *testing.T) {
	pool := newWorkspacePool(50 * time.Millisecond)
	state := pool.GetOrCreate("test", normalizeWorkspacePath("/workspace/busy"))
	state.BeginTurn()

	time.Sleep(100 * time.Millisecond)
	reaped := pool.ReapIdle()
	if len(reaped) != 0 {
		t.Fatalf("expected busy workspace to be preserved, got %v", reaped)
	}
	if got := pool.Get("test", normalizeWorkspacePath("/workspace/busy")); got == nil {
		t.Fatal("expected busy workspace to remain in pool")
	}

	state.EndTurn()
	time.Sleep(60 * time.Millisecond)
	reaped = pool.ReapIdle()
	wantWS := normalizeWorkspacePath("/workspace/busy")
	if len(reaped) != 1 || reaped[0].workspace != wantWS {
		t.Fatalf("expected busy workspace to reap after EndTurn, got %v", reaped)
	}
}

func TestInteractiveKeyForSessionKey_NormalizesWorkspace(t *testing.T) {
	tmp := t.TempDir()
	wsDir := filepath.Join(tmp, "ws1")
	if err := os.Mkdir(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := NewEngine("test", &stubAgent{}, []Platform{&stubPlatformEngine{n: "test"}}, "", LangEnglish)
	e.multiWorkspace = true
	e.baseDir = tmp
	e.workspaceBindings = NewWorkspaceBindingManager(filepath.Join(tmp, "bindings.json"))

	channelID := "chan123"
	sessionKey := "test:" + channelID

	// Bind with trailing slash (unnormalized)
	e.workspaceBindings.Bind("project:test", channelID, "chan", wsDir+"/")

	key := e.interactiveKeyForSessionKey(sessionKey)
	expected := normalizeWorkspacePath(wsDir) + ":" + sessionKey

	if key != expected {
		t.Errorf("interactiveKeyForSessionKey should normalize workspace path\ngot:  %s\nwant: %s", key, expected)
	}

	// Also verify it matches what we'd get with the clean path
	e.workspaceBindings.Bind("project:test", channelID, "chan", wsDir)
	key2 := e.interactiveKeyForSessionKey(sessionKey)
	if key != key2 {
		t.Errorf("keys should be identical regardless of trailing slash\nslash:   %s\nclean:   %s", key, key2)
	}
}

func TestWorkspacePool_KeyedByAgentType(t *testing.T) {
	pool := newWorkspacePool(15 * time.Minute)
	ws := "/path/to/ws"

	stateA := pool.GetOrCreate("claudecode", ws)
	stateB := pool.GetOrCreate("codex", ws)

	if stateA == stateB {
		t.Fatal("expected different states for different agent types with same workspace")
	}
	if len(pool.states) != 2 {
		t.Fatalf("expected 2 pool entries, got %d", len(pool.states))
	}

	stateA2 := pool.GetOrCreate("claudecode", ws)
	if stateA2 != stateA {
		t.Error("expected cache hit for same (agentType, workspace) pair")
	}
	if len(pool.states) != 2 {
		t.Fatalf("expected still 2 pool entries after cache hit, got %d", len(pool.states))
	}

	if stateA.agentType != "claudecode" {
		t.Errorf("stateA.agentType = %q, want %q", stateA.agentType, "claudecode")
	}
	if stateB.agentType != "codex" {
		t.Errorf("stateB.agentType = %q, want %q", stateB.agentType, "codex")
	}
}

// TestRegression_IdleReaper_DoesNotReapBusyTypedAgent verifies that the idle
// reaper preserves busy (activeTurns > 0) workspace states even when past the
// idle timeout, while reaping idle states in the same pool. The returned
// reapedWorkspace struct must carry the correct agentType — critical for
// logging the right agent when multiple agent types coexist.
//
// Regression for: after /agent switch codex, the pool contains entries for
// both "codex" and "claudecode". The reaper must not conflate them.
func TestRegression_IdleReaper_DoesNotReapBusyTypedAgent(t *testing.T) {
	pool := newWorkspacePool(50 * time.Millisecond)

	busyWS := normalizeWorkspacePath("/workspace/codex-busy")
	busyState := pool.GetOrCreate("codex", busyWS)
	busyState.BeginTurn()

	idleWS := normalizeWorkspacePath("/workspace/claude-idle")
	pool.GetOrCreate("claudecode", idleWS)

	time.Sleep(100 * time.Millisecond)

	reaped := pool.ReapIdle()

	if len(reaped) != 1 {
		t.Fatalf("expected 1 reaped workspace, got %d: %+v", len(reaped), reaped)
	}
	if reaped[0].agentType != "claudecode" {
		t.Errorf("reaped agentType = %q, want %q", reaped[0].agentType, "claudecode")
	}
	if reaped[0].workspace != idleWS {
		t.Errorf("reaped workspace = %q, want %q", reaped[0].workspace, idleWS)
	}

	if got := pool.Get("codex", busyWS); got == nil {
		t.Fatal("expected busy codex workspace to remain in pool after reap")
	}
	if got := pool.Get("claudecode", idleWS); got != nil {
		t.Fatal("expected idle claudecode workspace to be removed from pool")
	}

	busyState.EndTurn()
	time.Sleep(100 * time.Millisecond)

	reaped2 := pool.ReapIdle()

	if len(reaped2) != 1 {
		t.Fatalf("expected 1 reaped workspace after EndTurn, got %d: %+v", len(reaped2), reaped2)
	}
	if reaped2[0].agentType != "codex" {
		t.Errorf("reaped agentType = %q, want %q", reaped2[0].agentType, "codex")
	}
	if reaped2[0].workspace != busyWS {
		t.Errorf("reaped workspace = %q, want %q", reaped2[0].workspace, busyWS)
	}
}
