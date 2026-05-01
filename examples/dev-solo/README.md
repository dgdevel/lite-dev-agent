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

