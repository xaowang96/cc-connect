// Package core - CUJ multi-agent test fixtures.
//
// This file provides the cujStubAgent test fixture and its registry, used by
// the M-group CUJ tests (TestCUJ_M1/M2/M3) in cuj_test.go to verify the
// multi-agent group-selection feature end-to-end.
//
// The stub is registered under two type names ("cuj-claude" and "cuj-codex")
// so the engine's multi-agent routing (C5/C6/C8) can be exercised through the
// real ReceiveMessage → handleCommand → getOrCreateWorkspaceAgent path.
package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// cujStubAgent: a controllable Agent that records every prompt it receives
// across all its sessions. CUJ tests register two types ("cuj-claude" and
// "cuj-codex") so the engine's per-channel agent-type binding can be verified
// end-to-end — including across an engine restart.
// ---------------------------------------------------------------------------

type cujStubAgent struct {
	name string
	mu   sync.Mutex

	received []string                // every prompt Send'd to any session
	sessions []*cujStubAgentSession // all sessions created by StartSession
}

func (a *cujStubAgent) Name() string { return a.name }

func (a *cujStubAgent) StartSession(_ context.Context, _ string) (AgentSession, error) {
	s := &cujStubAgentSession{parent: a, events: make(chan Event, 4)}
	a.mu.Lock()
	a.sessions = append(a.sessions, s)
	a.mu.Unlock()
	return s, nil
}

func (a *cujStubAgent) ListSessions(_ context.Context) ([]AgentSessionInfo, error) {
	return nil, nil
}

func (a *cujStubAgent) Stop() error { return nil }

// recordReceived appends a prompt to the agent's received list. Called by
// cujStubAgentSession.Send under the parent's mutex.
func (a *cujStubAgent) recordReceived(prompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.received = append(a.received, prompt)
}

// getReceived returns a snapshot of all prompts the agent received.
func (a *cujStubAgent) getReceived() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.received))
	copy(out, a.received)
	return out
}

// sessionCount returns the number of sessions created by StartSession.
func (a *cujStubAgent) sessionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sessions)
}

// cujStubAgentSession is an AgentSession that records each Send on its parent
// cujStubAgent and emits a single EventResult so the engine can complete the
// turn and reply to the user.
type cujStubAgentSession struct {
	parent *cujStubAgent
	events chan Event
	closed atomic_bool
}

func (s *cujStubAgentSession) Send(prompt string, _ []ImageAttachment, _ []FileAttachment) error {
	s.parent.recordReceived(prompt)
	go func() {
		ev := Event{Type: EventResult, Content: "ok from " + s.parent.name, Done: true}
		select {
		case s.events <- ev:
		default:
		}
	}()
	return nil
}

func (s *cujStubAgentSession) RespondPermission(_ string, _ PermissionResult) error { return nil }
func (s *cujStubAgentSession) Events() <-chan Event                                 { return s.events }
func (s *cujStubAgentSession) CurrentSessionID() string                             { return "cuj-stub-" + s.parent.name }
func (s *cujStubAgentSession) Alive() bool                                          { return !s.closed.Get() }
func (s *cujStubAgentSession) Close() error {
	s.closed.Set(true)
	return nil
}

// ---------------------------------------------------------------------------
// Registry: captures every cujStubAgent created by the factory so tests can
// inspect which agent type handled each message — even after an engine
// restart, when the test no longer holds a direct reference to the
// factory-created (non-default) agent.
// ---------------------------------------------------------------------------

var cujStubAgentRegistry = struct {
	sync.Mutex
	byType map[string][]*cujStubAgent
}{
	byType: map[string][]*cujStubAgent{},
}

func registerCUJStubAgent(a *cujStubAgent) {
	cujStubAgentRegistry.Lock()
	defer cujStubAgentRegistry.Unlock()
	cujStubAgentRegistry.byType[a.name] = append(cujStubAgentRegistry.byType[a.name], a)
}

// cujStubAgentsOfType returns all registered cujStubAgent instances of the
// given type, in creation order.
func cujStubAgentsOfType(name string) []*cujStubAgent {
	cujStubAgentRegistry.Lock()
	defer cujStubAgentRegistry.Unlock()
	out := make([]*cujStubAgent, len(cujStubAgentRegistry.byType[name]))
	copy(out, cujStubAgentRegistry.byType[name])
	return out
}

// lastCUJStubAgent returns the most recently registered cujStubAgent of the
// given type, or nil if none exists.
func lastCUJStubAgent(name string) *cujStubAgent {
	cujStubAgentRegistry.Lock()
	defer cujStubAgentRegistry.Unlock()
	s := cujStubAgentRegistry.byType[name]
	if len(s) == 0 {
		return nil
	}
	return s[len(s)-1]
}

// resetCUJStubAgentRegistry clears all registered stub agents. Tests call
// this at the start so assertions are not polluted by prior tests.
func resetCUJStubAgentRegistry() {
	cujStubAgentRegistry.Lock()
	defer cujStubAgentRegistry.Unlock()
	cujStubAgentRegistry.byType = map[string][]*cujStubAgent{}
}

// init registers the two cuj stub agent types. RegisterAgent overwrites any
// prior registration, so this is safe to call at package init time. The type
// names are prefixed with "cuj-" to avoid collisions with other test stubs
// (e.g. "stub-a", "stub-b" in engine_agent_test.go).
func init() {
	RegisterAgent("cuj-claude", func(_ map[string]any) (Agent, error) {
		a := &cujStubAgent{name: "cuj-claude"}
		registerCUJStubAgent(a)
		return a, nil
	})
	RegisterAgent("cuj-codex", func(_ map[string]any) (Agent, error) {
		a := &cujStubAgent{name: "cuj-codex"}
		registerCUJStubAgent(a)
		return a, nil
	})
}

// ---------------------------------------------------------------------------
// CUJ-M helpers (used by TestCUJ_M1/M2/M3 in cuj_test.go)
// ---------------------------------------------------------------------------

// cujMWaitForSent polls the stub platform until at least minCount messages
// have been sent, or fails the test after timeout.
func cujMWaitForSent(t *testing.T, p *stubPlatformEngine, reason string, timeout time.Duration, minCount int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(p.getSent()) >= minCount {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (sent so far: %v)", reason, p.getSent())
}

// cujMSentContains reports whether any message sent to the platform contains
// needle.
func cujMSentContains(p *stubPlatformEngine, needle string) bool {
	for _, s := range p.getSent() {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// cujMReceivedContains reports whether any prompt in the agent's received
// list contains needle.
func cujMReceivedContains(a *cujStubAgent, needle string) bool {
	if a == nil {
		return false
	}
	for _, s := range a.getReceived() {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// cujMNewMultiAgentEngine builds a fresh Engine wired for multi-agent CUJ
// tests: default agent type "cuj-claude", template "cuj-codex", a
// ProjectStateStore at statePath, and adminFrom set to the provided value.
// The returned cujStubAgent is the default agent (e.agent) so the test can
// inspect what the default type received.
func cujMNewMultiAgentEngine(t *testing.T, storePath, statePath, adminFrom string) (*Engine, *stubPlatformEngine, *cujStubAgent) {
	t.Helper()
	defaultAgent := &cujStubAgent{name: "cuj-claude"}
	registerCUJStubAgent(defaultAgent)
	plat := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", defaultAgent, []Platform{plat}, storePath, LangEnglish)
	store := NewProjectStateStore(statePath)
	e.SetProjectStateStore(store)
	e.SetAgentTemplates(map[string]AgentConfigTemplate{
		"cuj-codex": {Type: "cuj-codex", Options: map[string]any{}},
	})
	if adminFrom != "" {
		e.SetAdminFrom(adminFrom)
	}
	return e, plat, defaultAgent
}

// cujMSendMsg drives the engine through ReceiveMessage with a standard
// message for the given user on the given sessionKey, then waits for the
// platform to receive at least one reply.
func cujMSendMsg(t *testing.T, e *Engine, p *stubPlatformEngine, sessionKey, userID, content string) {
	t.Helper()
	msg := &Message{
		SessionKey: sessionKey,
		Platform:   "test",
		MessageID:  "msg-" + userID + "-" + sanitizeForID(content),
		UserID:     userID,
		UserName:   userID,
		Content:    content,
		ReplyCtx:   "ctx-" + userID,
	}
	e.ReceiveMessage(p, msg)
	cujMWaitForSent(t, p, "reply to "+content, 3*time.Second, 1)
}

// sanitizeForID returns a short, ID-safe prefix of s for use in MessageID.
func sanitizeForID(s string) string {
	out := ""
	for _, r := range s {
		if len(out) >= 8 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out += string(r)
		} else {
			out += "_"
		}
	}
	if out == "" {
		return "msg"
	}
	return out
}
