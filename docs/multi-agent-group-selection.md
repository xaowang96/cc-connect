# Multi-Agent Group Selection

## Overview

cc-connect supports per-group-chat agent type switching within a single project. This means one bot can serve Claude Code, Codex, OpenCode, or any other supported agent — and users can switch between them with a slash command.

## Configuration

Declare additional agent types in your project config:

```toml
[[projects]]
name = "my-project"
work_dir = "/home/me/code"
admin_from = "alice"

# Default agent (used when no /agent binding exists)
[projects.agent]
type = "claudecode"

# Switchable agent types — only `type` is required.
# When options is omitted, the agent uses its own default config
# (e.g. ~/.codex/config.toml, opencode's own settings).
[[projects.agent_templates]]
type = "codex"

[[projects.agent_templates]]
type = "opencode"

# Optionally override specific options:
# [[projects.agent_templates]]
# type = "gemini"
# options = { model = "gemini-2.5-pro" }

[[projects.platforms]]
type = "feishu"
options = { app_id = "...", app_secret = "..." }
```

**Rules:**
- The `[projects.agent]` type is the default. Do NOT repeat it in `agent_templates` (validation rejects duplicates).
- Each template `type` must be unique.
- Each template `type` must be a registered agent (i.e., the agent binary is installed and not excluded via build tags).
- `options` is optional. Omit it to let the agent use its own default configuration. Only set fields you want to override.

## Usage

In any group chat (or DM) connected to the bot:

| Command | Admin Required | Effect |
|---------|---------------|--------|
| `/agent list` | No | Lists all available agent types, marking the current one |
| `/agent current` | No | Shows the agent type currently bound to this chat |
| `/agent switch <type>` | Yes | Switches the chat to the specified agent type |

After `/agent switch codex`, all subsequent messages in that chat route to a Codex agent process. Switching back: `/agent switch claudecode` (or whatever your default type is).

## Admin Gating

Only users listed in `admin_from` can run `/agent switch`. Any user can run `/agent list` and `/agent current`. This matches the philosophy that switching agent type is a privileged operation (it can change permission semantics), while viewing is harmless.

## Persistence

Agent bindings are stored per-project in `<data_dir>/projects/<project>.state.json` under the `agent_bindings` key. The binding survives process restarts. Clearing a binding (by switching back to the default type) removes the entry from the file.

## Session Isolation

Each agent type has its own independent session manager. Session files are named `<project>_<agentType>_ws_<hash>.json`. Switching from Claude Code to Codex and back does NOT lose history — each type's sessions are preserved independently on disk.

When you switch agent types, the current agent's active session is stopped (to prevent it from consuming messages meant for the new agent). The session history is preserved; only the live process is terminated.

## Known Limitations

- **Capability commands** (`/provider`, `/model`, `/reasoning`, `/mode`) operate on the channel's bound agent. If you switch to Codex and run `/model switch gpt-5`, it affects Codex's model, not Claude's.
- **Cron jobs** fire against the channel's currently bound agent. If you switch after creating a cron job, it will run on the new agent.
- **Provider wiring** for non-default agent types currently inherits the default agent's providers. Per-template provider configuration via `provider_refs` is stored but not fully wired yet.
- **Run-as-user** is project-level. If you need per-agent-type OS-user isolation, set `run_as_user` in the template's `options`.

## How It Works

1. **Config**: `[[projects.agent_templates]]` declares available types.
2. **State**: `ProjectStateStore.AgentBindings` maps `channelKey → agentType`.
3. **Routing**: `resolveAgentTypeForChannel(channelKey)` returns the bound type (or the default).
4. **Pool**: `workspacePool` keys entries by `(agentType, workspace)` so different types get independent agent processes and session managers.
5. **Creation**: `getOrCreateWorkspaceAgent(agentType, workspace)` builds opts from the template (for non-default types) or snapshots the default agent (for the default type), then calls `CreateAgent(agentType, opts)`.
