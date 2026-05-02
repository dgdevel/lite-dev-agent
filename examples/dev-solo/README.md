# dev-solo

Single agent, self-spawning capability.

# Installation

1. Install [nixdevkit](https://github.com/dgdevel/nixdevkit)

2. Run `nixdevkit-setup-indexer --global`

3. Run `cp nixdevkit/mcps.yml $HOME/.config/nixdevkit/mcps.yml`

4. Run `cp -r lite-dev-agent $HOME/.config/`

5. Edit `$HOME/.config/lite-dev-agent/config.yml`

# For each project directory

1. Run `echo reindex | nixdevkit-indexer`

2. Run `lite-dev-agent`

# Tweaking

Remove `--enable-memory` and the `final_tool_calls` to disable persistent memory.

Run `nixdevkit-config set llama.reranker_enabled false` to speedup `relevant_code` (less precise).


