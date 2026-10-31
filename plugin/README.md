# herd Claude Code plugin

Shipped with herd and installed to `~/.herd/plugin` at launch, then loaded into every step with
`claude --plugin-dir ~/.herd/plugin`. herd adds `commands/<name>.md` for each agent in
`~/.herd/agents.toml` (the `/herd:<name>` part of the agent's command), built from the agent's
instructions, so editing an agent in Settings > Agents rewrites its command.
