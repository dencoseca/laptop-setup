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

Git and several later installation steps need Apple's Command Line Tools. Open
Apple's installer and complete the dialog:

```shell
xcode-select --install
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

## 3. Homebrew

Install Homebrew using its [official installation command](https://docs.brew.sh/Installation):

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Add the Apple Silicon Homebrew environment to Zsh:

```shell
printf '%s\n' 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> "$HOME/.zprofile"
eval "$(/opt/homebrew/bin/brew shellenv)"
```

## 4. Packages and applications

Delete anything you do not want from these lists before running them.

Command-line packages:

```shell
brew install \
  btop \
  jq \
  bat \
  nano \
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
  logi-options+ \
  meetingbar \
  mos \
  rectangle \
  ghostty \
  signal
```

## 5. Node tooling

Install [Vite+](https://viteplus.dev/) to manage Node and project package
managers:

```shell
curl -fsSL https://vite.plus | bash
```

## 6. Docker

[Colima](https://github.com/abiosoft/colima) provides the Docker runtime. It
does not need a forced Docker context or `DOCKER_HOST` value. Homebrew's
Buildx and Compose formulae do need their plugin directory registered with
Docker, however. Create a fresh Docker configuration that registers that
directory and uses the macOS Keychain for credentials:

```shell
mkdir -p "$HOME/.docker"

cat > "$HOME/.docker/config.json" <<'EOF'
{
  "cliPluginsExtraDirs": [
    "/opt/homebrew/lib/docker/cli-plugins"
  ],
  "credsStore": "osxkeychain"
}
EOF

chmod 600 "$HOME/.docker/config.json"
```

## 7. Shell configuration

Install [Oh My Zsh](https://github.com/ohmyzsh/ohmyzsh) without changing the
login shell or launching Zsh during installation:

```shell
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
```

Install the two external plugins used by the configuration below:

```shell
git clone --depth 1 https://github.com/zsh-users/zsh-autosuggestions \
  "$HOME/.oh-my-zsh/custom/plugins/zsh-autosuggestions"

git clone --depth 1 https://github.com/zsh-users/zsh-syntax-highlighting \
  "$HOME/.oh-my-zsh/custom/plugins/zsh-syntax-highlighting"
```

Create a small, readable Zsh configuration. Oh My Zsh automatically loads
personal `.zsh` files from its custom directory, keeping aliases and functions
out of the main configuration.

```shell
cat > "$HOME/.zshrc" <<'EOF'
export ZSH="$HOME/.oh-my-zsh"
zstyle ':omz:update' mode disabled

plugins=(
  git
  fzf
  zsh-autosuggestions
  zsh-syntax-highlighting
)

source "$ZSH/oh-my-zsh.sh"
eval "$(starship init zsh)"
EOF

touch "$HOME/.hushlogin"
```

Add personal aliases and functions in Oh My Zsh's custom directory:

```shell
mkdir -p "$HOME/Developer/repos"
mkdir -p "$HOME/.oh-my-zsh/custom"

cat > "$HOME/.oh-my-zsh/custom/alias.zsh" <<'EOF'
ag() {
  alias | grep -i -- "$1"
}

alias repos='cd "$HOME/Developer/repos"'
alias dl='cd "$HOME/Downloads"'
alias dt='cd "$HOME/Desktop"'
alias npmls='npm list -g --depth=0'
alias l='ls -lh'
alias nq='networkQuality'
alias trc='tree -d -L 3 "$HOME/Developer/repos"'
alias oif='open -a Finder .'
alias src='source "$HOME/.zshrc"'
alias zshc='nano "$HOME/.zshrc"'
alias zshb='cp "$HOME/.zshrc" "$HOME/.zshrc.bak"'
alias upbrew='brew update && brew upgrade && brew cleanup && brew doctor'
alias upomz='omz update'
EOF
```

Configure Starship:

```shell
mkdir -p "$HOME/.config"

cat > "$HOME/.config/starship.toml" <<'EOF'
[aws]
disabled = true

[gcloud]
disabled = true
EOF
```

Configure Ghostty:

```shell
mkdir -p "$HOME/.config/ghostty"

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

Load the new Zsh configuration:

```shell
source "$HOME/.zshrc"
```

## 8. Git configuration

Write the global Git configuration for the new laptop. Replace the name and
email values first:

```shell
cat > "$HOME/.gitconfig" <<'EOF'
[user]
  name = Your Name
  email = you@example.com
[init]
  defaultBranch = main
[core]
  excludesfile = ~/.gitignore
  autocrlf = input
[rerere]
  enabled = true
EOF

cat > "$HOME/.gitignore" <<'EOF'
.DS_Store
.idea
EOF
```

## 9. Manual App Store installs

Install whichever of these you still use:

- [ ] Amphetamine
- [ ] Bear
- [ ] Bitwarden
- [ ] Things
- [ ] NordVPN

When everything is installed, restart the Mac once.
