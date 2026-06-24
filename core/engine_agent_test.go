package core

import (
	"path/filepath"
	"strings"
	"testing"
)

// namedStubAgent is a stubAgent with a configurable Name(), used for
// testing agent-type-aware resolution where the default agent type
// must be a specific value (e.g. "claudecode").
type namedStubAgent struct {
	stubAgent
	name string
}

func (a *namedStubAgent) Name() string { return a.name }

func newResolveTestEngine(t *testing.T) (*Engine, *ProjectStateStore) {
	t.Helper()
	ag := &namedStubAgent{name: "claudecode"}
	e := NewEngine("test", ag, []Platform{&stubPlatformEngine{n: "test"}}, "", LangEnglish)
	store := NewProjectStateStore(filepath.Join(t.TempDir(), "state.json"))
	e.SetProjectStateStore(store)
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		"codex": {Type: "codex"},
	})
	return e, store
}

func TestEngine_ResolveAgentTypeForChannel_DefaultsWhenUnbound(t *testing.T) {
	e, _ := newResolveTestEngine(t)
	got := e.resolveAgentTypeForChannel("feishu:oc_123")
	if got != "claudecode" {
		t.Fatalf("resolveAgentTypeForChannel(unbound) = %q, want %q", got, "claudecode")
	}
}

func TestEngine_ResolveAgentTypeForChannel_ReturnsBinding(t *testing.T) {
	e, store := newResolveTestEngine(t)
	store.SetAgentBinding("feishu:oc_123", "codex")
	got := e.resolveAgentTypeForChannel("feishu:oc_123")
	if got != "codex" {
		t.Fatalf("resolveAgentTypeForChannel(bound) = %q, want %q", got, "codex")
	}
}

func TestEngine_ResolveAgentTypeForChannel_FallsBackWhenBindingTypeNotInTemplates(t *testing.T) {
	e, store := newResolveTestEngine(t)
	store.SetAgentBinding("feishu:oc_123", "gemini")
	got := e.resolveAgentTypeForChannel("feishu:oc_123")
	if got != "claudecode" {
		t.Fatalf("resolveAgentTypeForChannel(bound-to-unregistered) = %q, want default %q", got, "claudecode")
	}
}

// TestRegression_GetOrCreateWorkspaceAgent_HonorsAgentTypeArgument
// verifies that getOrCreateWorkspaceAgent creates an agent of the type
// passed as the agentType argument, not the project's default. This is
// the core behavioral change of C5: before C5 the function always
// created e.agent.Name(); after C5 it creates whatever agentType the
// caller passes, as long as that type is either the default or
// registered via SetAgentTemplates.
//
// The test also verifies:
//   - The default type still works (backward compatibility).
//   - An unregistered non-default type returns an error.
//   - The per-workspace session filename includes the agentType so
//     sessions for different agent types do not collide.
func TestRegression_GetOrCreateWorkspaceAgent_HonorsAgentTypeArgument(t *testing.T) {
	const defaultType = "stub-c5-reg-default"
	const altType = "stub-c5-reg-alt"

	RegisterAgent(defaultType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: defaultType}, nil
	})
	RegisterAgent(altType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: altType}, nil
	})

	defaultAgent := &namedStubAgent{name: defaultType}
	e := NewEngine("test-c5-reg", defaultAgent, []Platform{&stubPlatformEngine{n: "plain"}}, "", LangEnglish)
	e.SetMultiWorkspace(t.TempDir(), filepath.Join(t.TempDir(), "bindings.json"))
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		altType: {Type: altType, Options: map[string]any{}},
	})

	workspace := normalizeWorkspacePath(t.TempDir())

	// Non-default type: must create an agent whose Name() is altType,
	// not defaultType.
	altAgent, altSessions, err := e.getOrCreateWorkspaceAgent(altType, workspace)
	if err != nil {
		t.Fatalf("getOrCreateWorkspaceAgent(%q) error: %v", altType, err)
	}
	if got := altAgent.Name(); got != altType {
		t.Fatalf("alt agent Name() = %q, want %q", got, altType)
	}
	altStorePath := altSessions.StorePath()
	if !strings.Contains(altStorePath, altType) {
		t.Fatalf("alt session path %q does not contain agentType %q", altStorePath, altType)
	}

	// Default type: must create an agent whose Name() is defaultType
	// (backward-compatible path).
	defAgent, defSessions, err := e.getOrCreateWorkspaceAgent(defaultType, workspace)
	if err != nil {
		t.Fatalf("getOrCreateWorkspaceAgent(%q) error: %v", defaultType, err)
	}
	if got := defAgent.Name(); got != defaultType {
		t.Fatalf("default agent Name() = %q, want %q", got, defaultType)
	}
	defStorePath := defSessions.StorePath()
	if !strings.Contains(defStorePath, defaultType) {
		t.Fatalf("default session path %q does not contain agentType %q", defStorePath, defaultType)
	}

	// Session files for different agent types must be distinct.
	if altStorePath == defStorePath {
		t.Fatalf("session paths should differ: both = %q", altStorePath)
	}

	// Unregistered type: must return an error, not silently fall back.
	_, _, err = e.getOrCreateWorkspaceAgent("stub-c5-reg-unregistered", workspace)
	if err == nil {
		t.Fatal("getOrCreateWorkspaceAgent(unregistered) expected error, got nil")
	}
}

// TestRegression_SessionContextForKey_RoutesToBoundAgentType verifies that
// sessionContextForKey resolves the agent type per-channel via
// resolveAgentTypeForChannel (C6). When a channel has an agent-type binding
// and a template exists for that type, the returned Agent must be of the
// bound type — not the project default. Unbound channels continue to route
// to the default agent (zero regression for existing users).
//
// This is the core routing change of C6: before C6 the function always
// returned e.agent for the single-workspace fallback; after C6 it
// materializes a workspace-scoped agent of the bound type when the
// channel has a non-default binding.
func TestRegression_SessionContextForKey_RoutesToBoundAgentType(t *testing.T) {
	const defaultType = "stub-c6-route-default"
	const altType = "stub-c6-route-alt"

	RegisterAgent(defaultType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: defaultType}, nil
	})
	RegisterAgent(altType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: altType}, nil
	})

	defaultAgent := &namedStubAgent{name: defaultType}
	e := NewEngine("test-c6-route", defaultAgent, []Platform{&stubPlatformEngine{n: "plain"}}, "", LangEnglish)
	store := NewProjectStateStore(filepath.Join(t.TempDir(), "state.json"))
	e.SetProjectStateStore(store)
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		altType: {Type: altType, Options: map[string]any{}},
	})

	// Bound channel: must route to altType.
	store.SetAgentBinding("feishu:oc_bound", altType)
	boundAgent, _ := e.sessionContextForKey("feishu:oc_bound:user_1")
	if got := boundAgent.Name(); got != altType {
		t.Fatalf("bound channel: sessionContextForKey returned agent %q, want %q", got, altType)
	}

	// Unbound channel: must fall back to default agent (e.agent).
	unboundAgent, _ := e.sessionContextForKey("feishu:oc_other:user_2")
	if got := unboundAgent.Name(); got != defaultType {
		t.Fatalf("unbound channel: sessionContextForKey returned agent %q, want default %q", got, defaultType)
	}

	// Bound-to-default channel: must also return the default agent
	// (resolveAgentTypeForChannel short-circuits when bound == default).
	store.SetAgentBinding("feishu:oc_default", defaultType)
	defBoundAgent, _ := e.sessionContextForKey("feishu:oc_default:user_3")
	if got := defBoundAgent.Name(); got != defaultType {
		t.Fatalf("bound-to-default channel: sessionContextForKey returned agent %q, want default %q", got, defaultType)
	}
}

// TestRegression_CommandContextWithWorkspace_RoutesToBoundAgentType
// verifies that commandContextWithWorkspace resolves the agent type
// per-channel via resolveAgentTypeForChannel (C6). When a channel has a
// non-default agent-type binding, the returned Agent must be of the bound
// type. Unbound channels continue to route to the default agent.
func TestRegression_CommandContextWithWorkspace_RoutesToBoundAgentType(t *testing.T) {
	const defaultType = "stub-c6-cmd-default"
	const altType = "stub-c6-cmd-alt"

	RegisterAgent(defaultType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: defaultType}, nil
	})
	RegisterAgent(altType, func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: altType}, nil
	})

	defaultAgent := &namedStubAgent{name: defaultType}
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test-c6-cmd", defaultAgent, []Platform{p}, "", LangEnglish)
	store := NewProjectStateStore(filepath.Join(t.TempDir(), "state.json"))
	e.SetProjectStateStore(store)
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		altType: {Type: altType, Options: map[string]any{}},
	})

	// Bound channel: must route to altType.
	store.SetAgentBinding("feishu:oc_bound", altType)
	msg := &Message{
		SessionKey: "feishu:oc_bound:user_1",
		Platform:   "feishu",
		ChannelID:  "oc_bound",
		UserID:     "user_1",
	}
	boundAgent, _, _, _, err := e.commandContextWithWorkspace(p, msg)
	if err != nil {
		t.Fatalf("bound channel: commandContextWithWorkspace error: %v", err)
	}
	if got := boundAgent.Name(); got != altType {
		t.Fatalf("bound channel: commandContextWithWorkspace returned agent %q, want %q", got, altType)
	}

	// Unbound channel: must fall back to default agent.
	msgUnbound := &Message{
		SessionKey: "feishu:oc_other:user_2",
		Platform:   "feishu",
		ChannelID:  "oc_other",
		UserID:     "user_2",
	}
	unboundAgent, _, _, _, err := e.commandContextWithWorkspace(p, msgUnbound)
	if err != nil {
		t.Fatalf("unbound channel: commandContextWithWorkspace error: %v", err)
	}
	if got := unboundAgent.Name(); got != defaultType {
		t.Fatalf("unbound channel: commandContextWithWorkspace returned agent %q, want default %q", got, defaultType)
	}
}

// ──────────────────────────────────────────────────────────────
// /agent command tests (C8)
// ──────────────────────────────────────────────────────────────

func newAgentCmdTestEngine(t *testing.T) (*Engine, *stubPlatformEngine, *ProjectStateStore) {
	t.Helper()
	RegisterAgent("stub-a", func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: "stub-a"}, nil
	})
	RegisterAgent("stub-b", func(_ map[string]any) (Agent, error) {
		return &namedStubAgent{name: "stub-b"}, nil
	})
	defaultAgent := &namedStubAgent{name: "stub-a"}
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test-agent-cmd", defaultAgent, []Platform{p}, "", LangEnglish)
	store := NewProjectStateStore(filepath.Join(t.TempDir(), "state.json"))
	e.SetProjectStateStore(store)
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		"stub-b": {Type: "stub-b", Options: map[string]any{}},
	})
	return e, p, store
}

func agentCmdMsg(sessionKey, userID string) *Message {
	return &Message{
		SessionKey: sessionKey,
		Platform:   "feishu",
		UserID:     userID,
		ReplyCtx:   "ctx",
	}
}

func lastReply(t *testing.T, p *stubPlatformEngine) string {
	t.Helper()
	sent := p.getSent()
	if len(sent) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	return sent[len(sent)-1]
}

func allReplies(t *testing.T, p *stubPlatformEngine) string {
	t.Helper()
	return strings.Join(p.getSent(), "\n---\n")
}

func TestCmdAgent_ListShowsDefaultAndTemplates(t *testing.T) {
	e, p, _ := newAgentCmdTestEngine(t)
	msg := agentCmdMsg("feishu:oc_123:user_1", "user_1")

	e.handleCommand(p, msg, "/agent list")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "stub-a") {
		t.Fatalf("/agent list reply should contain default type stub-a, got: %q", reply)
	}
	if !strings.Contains(reply, "stub-b") {
		t.Fatalf("/agent list reply should contain template type stub-b, got: %q", reply)
	}
}

func TestCmdAgent_CurrentShowsBoundType(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	store.SetAgentBinding("feishu:oc_123", "stub-b")
	msg := agentCmdMsg("feishu:oc_123:user_1", "user_1")

	e.handleCommand(p, msg, "/agent current")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "stub-b") {
		t.Fatalf("/agent current reply should contain bound type stub-b, got: %q", reply)
	}
}

func TestCmdAgent_SwitchRejectsUnknownType(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	e.SetAdminFrom("admin")
	msg := agentCmdMsg("feishu:oc_123:admin", "admin")

	e.handleCommand(p, msg, "/agent switch invalid")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "invalid") || !strings.Contains(reply, "Unknown") {
		t.Fatalf("/agent switch invalid should reply with unknown-type error, got: %q", reply)
	}
	if got := store.AgentBinding("feishu:oc_123"); got != "" {
		t.Fatalf("no binding should be written for unknown type, got %q", got)
	}
}

func TestCmdAgent_SwitchRejectsNonAdmin(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	e.SetAdminFrom("alice")
	msg := agentCmdMsg("feishu:oc_123:bob", "bob")

	e.handleCommand(p, msg, "/agent switch stub-b")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "admin") {
		t.Fatalf("/agent switch by non-admin should reply with admin-required error, got: %q", reply)
	}
	if got := store.AgentBinding("feishu:oc_123"); got != "" {
		t.Fatalf("no binding should be written when non-admin is rejected, got %q", got)
	}
}

func TestCmdAgent_SwitchPersistsBinding(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	e.SetAdminFrom("admin")
	msg := agentCmdMsg("feishu:oc_123:admin", "admin")

	e.handleCommand(p, msg, "/agent switch stub-b")

	if got := store.AgentBinding("feishu:oc_123"); got != "stub-b" {
		t.Fatalf("after /agent switch stub-b, binding should be stub-b, got %q", got)
	}
	combined := allReplies(t, p)
	if !strings.Contains(combined, "Switched to stub-b") && !strings.Contains(combined, "stub-b") {
		t.Fatalf("/agent switch should reply with switch-ok message, got: %q", combined)
	}
}

func TestCmdAgent_SwitchToDefaultClearsBinding(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	e.SetAdminFrom("admin")
	msg := agentCmdMsg("feishu:oc_123:admin", "admin")

	e.handleCommand(p, msg, "/agent switch stub-b")
	if got := store.AgentBinding("feishu:oc_123"); got != "stub-b" {
		t.Fatalf("after first switch, binding should be stub-b, got %q", got)
	}

	p.clearSent()
	e.handleCommand(p, msg, "/agent switch stub-a")

	if got := store.AgentBinding("feishu:oc_123"); got != "" {
		t.Fatalf("after switching back to default, binding should be cleared, got %q", got)
	}
	combined := allReplies(t, p)
	if !strings.Contains(combined, "stub-a") {
		t.Fatalf("/agent switch stub-a should reply with switch-ok message mentioning stub-a, got: %q", combined)
	}
}

func TestCmdAgent_DisabledWhenNoTemplates(t *testing.T) {
	defaultAgent := &namedStubAgent{name: "stub-a"}
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test-no-templates", defaultAgent, []Platform{p}, "", LangEnglish)
	msg := agentCmdMsg("feishu:oc_123:user_1", "user_1")

	e.handleCommand(p, msg, "/agent list")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "disabled") && !strings.Contains(reply, "agent_templates") {
		t.Fatalf("/agent with no templates should reply with disabled message, got: %q", reply)
	}
}

func TestCmdAgent_NoArgsShowsUsage(t *testing.T) {
	e, p, _ := newAgentCmdTestEngine(t)
	msg := agentCmdMsg("feishu:oc_123:user_1", "user_1")

	e.handleCommand(p, msg, "/agent")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "Usage") && !strings.Contains(reply, "/agent") {
		t.Fatalf("/agent with no args should reply with usage, got: %q", reply)
	}
}

func TestCmdAgent_SwitchSameTypeIsNoOp(t *testing.T) {
	e, p, store := newAgentCmdTestEngine(t)
	e.SetAdminFrom("admin")
	msg := agentCmdMsg("feishu:oc_123:admin", "admin")

	e.handleCommand(p, msg, "/agent switch stub-a")

	reply := lastReply(t, p)
	if !strings.Contains(reply, "no change") && !strings.Contains(strings.ToLower(reply), "already") {
		t.Fatalf("/agent switch to same type should reply with no-change message, got: %q", reply)
	}
	if got := store.AgentBinding("feishu:oc_123"); got != "" {
		t.Fatalf("no binding should be written for same-type switch, got %q", got)
	}
}
