# Laptop setup

A personal, manual checklist for setting up a fresh Apple Silicon Mac.

This repository is intentionally just a guide. There is no bootstrap script,
installer, state file, resumable workflow, or setup application. Read each
section, edit anything you do not want, and run one block at a time. If a
command produces unexpected output, stop there and investigate it before
continuing.

The commands assume:

- an Apple Silicon Mac;
- an administrator account;
- an internet connection; and
- the default macOS Zsh shell.

Some steps execute official remote installer scripts. Follow the linked
documentation and inspect those scripts first if you are not comfortable
running them.

## 1. Xcode Command Line Tools

Git and several later installation steps need Apple's Command Line Tools.
Check whether they are already installed:

```shell
xcode-select -p
```

If that reports an error, open Apple's installer and complete the dialog:

```shell
xcode-select --install
```

Then verify the installation:

```shell
xcode-select -p
git --version
```

## 2. macOS preferences

Review these preferences and remove any you do not want before running the
block. Some changes may require logging out or restarting the Mac before they
appear everywhere.

```shell
# Keyboard
defaults write -g InitialKeyRepeat -int 20
defaults write -g KeyRepeat -int 1
defaults write -g AppleWindowTabbingMode -string always

# Dock
defaults write com.apple.dock autohide -bool true
defaults write com.apple.dock no-bouncing -bool true
defaults write com.apple.dock tilesize -int 60
defaults write com.apple.dock show-recents -bool false
defaults write com.apple.dock show-process-indicators -bool false
defaults write com.apple.dock magnification -bool true
defaults write com.apple.dock largesize -int 70
defaults write com.apple.dock windowtabbing -string always

# Finder
defaults write com.apple.finder ShowPathbar -bool true
defaults write com.apple.finder FXPreferredViewStyle -string clmv
defaults write com.apple.finder _FXSortFoldersFirst -bool true
defaults write com.apple.finder FXRemoveOldTrashItems -bool true
defaults write com.apple.finder _FXSortFoldersFirstOnDesktop -bool true

# Trackpad and Siri
defaults write com.apple.AppleMultitouchTrackpad FirstClickThreshold -int 0
defaults write com.apple.Siri StatusMenuVisible -bool false

killall Dock 2>/dev/null || true
killall Finder 2>/dev/null || true
```

Inspect any saved value with `defaults read`, for example:

```shell
defaults read -g KeyRepeat
defaults read com.apple.dock autohide
defaults read com.apple.finder ShowPathbar
```

## 3. Homebrew

Install Homebrew using its [official installation command](https://docs.brew.sh/Installation):

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Add the Apple Silicon Homebrew environment to Zsh without duplicating it on a
later run:

```shell
touch "$HOME/.zprofile"
grep -Fqx 'eval "$(/opt/homebrew/bin/brew shellenv)"' "$HOME/.zprofile" || \
  printf '%s\n' 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> "$HOME/.zprofile"
eval "$(/opt/homebrew/bin/brew shellenv)"
brew --version
```

## 4. Packages and applications

Delete anything you do not want from these lists before running them.

Command-line packages:

```shell
brew install \
  jq \
  bat \
  ripgrep \
  tree \
  watch \
  starship \
  fzf \
  git \
  docker \
  docker-buildx \
  docker-compose \
  docker-credential-helper \
  colima \
  switchaudio-osx
```

Desktop applications:

```shell
brew install --cask \
  alfred \
  appcleaner \
  brave-browser \
  jetbrains-toolbox \
  logi-options-plus \
  meetingbar \
  mos \
  rectangle \
  ghostty \
  signal
```

Review what Homebrew installed:

```shell
brew list --formula
brew list --cask
```

## 5. Node tooling

Install [Vite+](https://viteplus.dev/) to manage Node and project package
managers:

```shell
curl -fsSL https://vite.plus | bash
```

Open a new terminal, then verify it:

```shell
vp help
```

## 6. Docker

[Colima](https://github.com/abiosoft/colima) provides the Docker runtime. It
does not need a forced Docker context or `DOCKER_HOST` value. Homebrew's
Buildx and Compose formulae do need their plugin directory registered with
Docker, however.

Preserve any existing Docker settings, then add the Homebrew plugin directory.
On a fresh configuration, Docker credentials will use the macOS Keychain:

```shell
(
  set -eu
  umask 077

  docker_dir="$HOME/.docker"
  docker_config="$docker_dir/config.json"
  mkdir -p "$docker_dir"

  if [ -f "$docker_config" ]; then
    docker_backup="$docker_config.backup.$(date +%Y%m%d-%H%M%S)"
    cp "$docker_config" "$docker_backup"
    chmod 600 "$docker_backup"
  else
    printf '{}\n' > "$docker_config"
  fi

  docker_config_next="$(mktemp "$docker_dir/config.json.XXXXXX")"
  trap 'rm -f "$docker_config_next"' EXIT HUP INT TERM

  jq '
    .cliPluginsExtraDirs = (
      ((.cliPluginsExtraDirs // []) + ["/opt/homebrew/lib/docker/cli-plugins"])
      | unique
    )
    | .credsStore //= "osxkeychain"
  ' "$docker_config" > "$docker_config_next"

  chmod 600 "$docker_config_next"
  mv "$docker_config_next" "$docker_config"
)
```

Start Colima and verify the runtime and plugins:

```shell
colima start
docker run --rm hello-world
docker buildx version
docker compose version
```

Useful lifecycle commands:

```shell
colima status
colima stop
colima start
```

## 7. Shell configuration

Install [Oh My Zsh](https://github.com/ohmyzsh/ohmyzsh) without changing the
login shell or launching Zsh during installation:

```shell
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
```

Install the two external plugins used by the configuration below:

```shell
[ -d "$HOME/.oh-my-zsh/custom/plugins/zsh-autosuggestions" ] || \
  git clone --depth 1 https://github.com/zsh-users/zsh-autosuggestions \
  "$HOME/.oh-my-zsh/custom/plugins/zsh-autosuggestions"

[ -d "$HOME/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting" ] || \
  git clone --depth 1 https://github.com/zsh-users/zsh-syntax-highlighting \
  "$HOME/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting"
```

Back up the current Zsh configuration and replace it with a small, readable
one. Put machine-specific aliases and functions in `~/.zshrc.local` rather
than growing the main configuration indefinitely.

```shell
if [ -f "$HOME/.zshrc" ]; then
  cp "$HOME/.zshrc" "$HOME/.zshrc.backup.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOME/.zshrc" <<'EOF'
export ZSH="$HOME/.oh-my-zsh"
zstyle ':omz:update' mode disabled

plugins=(
  git
  fzf
  zsh-autosuggestions
  zsh-syntax-highlighting
)

if [[ -r "$ZSH/oh-my-zsh.sh" ]]; then
  source "$ZSH/oh-my-zsh.sh"
fi

if command -v starship >/dev/null 2>&1; then
  eval "$(starship init zsh)"
fi

if [[ -r "$HOME/.zshrc.local" ]]; then
  source "$HOME/.zshrc.local"
fi
EOF

touch "$HOME/.zshrc.local"
touch "$HOME/.hushlogin"
```

Configure Starship:

```shell
mkdir -p "$HOME/.config"

if [ -f "$HOME/.config/starship.toml" ]; then
  cp "$HOME/.config/starship.toml" \
    "$HOME/.config/starship.toml.backup.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOME/.config/starship.toml" <<'EOF'
[aws]
disabled = true

[gcloud]
disabled = true
EOF
```

Configure Ghostty. This block backs up an existing configuration before
replacing it:

```shell
mkdir -p "$HOME/.config/ghostty"

if [ -f "$HOME/.config/ghostty/config.ghostty" ]; then
  cp "$HOME/.config/ghostty/config.ghostty" \
    "$HOME/.config/ghostty/config.ghostty.backup.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOME/.config/ghostty/config.ghostty" <<'EOF'
font-family = JetBrains Mono
font-size = 13
font-thicken = true
font-thicken-strength = 24
adjust-cell-height = 4%

background = #000000
foreground = #E7E5EB
background-opacity = 1.0
cursor-color = #B7AEFF
cursor-text = #000000
cursor-style = bar
cursor-style-blink = true
selection-background = #2B2933
selection-foreground = #FFFFFF

palette = 0=#000000
palette = 1=#FF6B6B
palette = 2=#A7E46B
palette = 3=#F6C85F
palette = 4=#7AA2F7
palette = 5=#D89CFF
palette = 6=#55DDE0
palette = 7=#E7E5EB
palette = 8=#69676F
palette = 9=#FF8787
palette = 10=#B9F27C
palette = 11=#FFD975
palette = 12=#8FB3FF
palette = 13=#FF8AD8
palette = 14=#72E6E6
palette = 15=#FFFFFF

window-padding-x = 22
window-padding-y = 8
window-padding-balance = false
window-padding-color = background
window-width = 120

macos-titlebar-style = tabs
macos-window-buttons = visible
window-theme = dark
window-title-font-family = JetBrains Mono

split-divider-color = #242428
unfocused-split-opacity = 0.92

shell-integration = detect
window-inherit-working-directory = true
tab-inherit-working-directory = true
split-inherit-working-directory = true

mouse-hide-while-typing = true
cursor-click-to-move = true
scrollback-limit = 104857600
EOF
```

Check the Zsh syntax, then load the new configuration:

```shell
zsh -n "$HOME/.zshrc"
source "$HOME/.zshrc"
```

## 8. Git configuration

These commands update individual Git settings without replacing the entire
`~/.gitconfig`. Replace the name and email values first.

```shell
git config --global user.name "Your Name"
git config --global user.email "you@example.com"
git config --global init.defaultBranch main
git config --global core.autocrlf input
git config --global rerere.enabled true
```

Back up and write the global ignore file:

```shell
if [ -f "$HOME/.gitignore" ]; then
  cp "$HOME/.gitignore" "$HOME/.gitignore.backup.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOME/.gitignore" <<'EOF'
.DS_Store
.idea
EOF

git config --global core.excludesfile "$HOME/.gitignore"
```

Review the resulting configuration:

```shell
git config --global --list
```

## 9. Manual App Store installs

Install whichever of these you still use:

- [ ] Amphetamine
- [ ] Bear
- [ ] Bitwarden
- [ ] Things
- [ ] NordVPN

When everything is working, restart the Mac once and revisit any section whose
verification command did not produce the expected result.
