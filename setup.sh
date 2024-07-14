#!/usr/bin/env zsh

set -euo pipefail

#!/usr/bin/env zsh

SCRIPT_DIR=$(cd -- "$(dirname -- "${0}")" &> /dev/null && pwd)
DOTFILES_DIR=$SCRIPT_DIR/dotfiles

# '------------------------------------'
# ' Script setup
# '------------------------------------'

print_message() {
  echo "$1"
  say "$1"
}

ignore_dave_and_leave_him_in_space_to_suffocate() {
  print_message "...I'm sorry Dave, I'm afraid I can't do that"
}

print_log_header() {
  echo
  echo "##------------------------------------------------##"
  echo "##--------  $(date)  --------##"
  echo "##------------------------------------------------##"
}

loading_messages=()

while IFS= read -r line; do
  if [ -n "$line" ]; then
    loading_messages+=("$line")
  fi
done < './loading-messages.txt'

num_lines=${#loading_messages[@]}

print_loading_message() {
  local random_index=$((RANDOM % num_lines))
  local message="${loading_messages[random_index]}"

  sleep "$((RANDOM % 3))"
  echo "$message"
  say "$message"
  sleep "$((RANDOM % 3))"
}

spinner_pid=
spinner_message=""

start_spinner() {
  spinner_message="$1"
  set +m
  echo -n "$1         "

  { while :; do for X in '  •     ' '   •    ' '    •   ' '     •  ' '      • ' '     •  ' '    •   ' '   •    ' '  •     ' ' •      '; do
    echo -en "\b\b\b\b\b\b\b\b$X"
    sleep 0.1
  done; done & } 2> /dev/null

  spinner_pid=$!
  say "$1"
}

stop_spinner() {
  { kill -9 $spinner_pid && wait; } 2> /dev/null
  set -m
  echo -en "\033[2K\r"
  echo "${spinner_message}... done!"
}

trap 'ignore_dave_and_leave_him_in_space_to_suffocate' ERR
trap 'stop_spinner' EXIT

cd "$HOME" || exit 1

# '------------------------------------'
# ' Install Xcode Command Line Tools
# '------------------------------------'

print_message "Checking Command Line Tools for Xcode"
# Only run if the tools are not installed yet
# To check that try to print the SDK path
if ! xcode-select -p &> /dev/null; then
  # This temporary file prompts the 'softwareupdate' utility to list the Command Line Tools
  start_spinner "Command Line Tools for Xcode not found. Installing from software update utility"
  {
    touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
    version=$(softwareupdate -l | grep "\*.*Command Line" | tail -n 1 | sed 's/^[^C]* //')
    softwareupdate -i "$version" --verbose
  } &>> "$HOME/.xcode-select-install.log"
  stop_spinner
else
  print_message "Command Line Tools for Xcode are already installed."
fi

# '------------------------------------'
# ' Set Mac OS defaults
# '------------------------------------'

print_message 'Setting Mac OS defaults'
{
  print_log_header
  # global
  defaults write -g InitialKeyRepeat -int 20
  defaults write -g KeyRepeat -int 1
  defaults write -g AppleWindowTabbingMode -string always
  # dock
  defaults write com.apple.dock autohide -bool true
  defaults write com.apple.dock tilesize -int 52
  defaults write com.apple.dock show-recents -bool false
  defaults write com.apple.dock show-process-indicators -bool false
  defaults write com.apple.dock magnification -bool true
  defaults write com.apple.dock largesize -int 60
  defaults write com.apple.dock windowtabbing -string always
  killall Dock
  # finder
  defaults write com.apple.finder ShowPathbar -bool true
  defaults write com.apple.finder FXPreferredViewStyle -string clmv
  defaults write com.apple.finder _FXSortFoldersFirst -bool true
  defaults write com.apple.finder FXRemoveOldTrashItems -bool true
  defaults write com.apple.finder _FXSortFoldersFirstOnDesktop -bool true
  killall Finder
  # trackpad
  defaults write com.apple.AppleMultitouchTrackpad FirstClickThreshold -int 0
  # siri
  defaults write com.apple.Siri StatusMenuVisible -bool false
} &>> "$HOME/.setting_macos_defaults.log"

print_loading_message
print_loading_message

print_message 'Enabling Touch ID for sudo'
sed -i '.bak' '2i\
auth sufficient pam_tid.so
' /etc/pam.d/sudo

# '------------------------------------'
# ' Install bloatware
# '------------------------------------'

start_spinner 'Installing homebrew'
{
  print_log_header
  NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
} &>> "$HOME/.homebrew_install.log"
stop_spinner

print_message 'Checking for brew shellenv init'
if [ -f "$HOME/.zprofile" ] && grep -q '/opt/homebrew/bin/brew shellenv' "$HOME/.zprofile"; then
  print_message "Brew shellenv init already exists in $HOME/.zprofile"
else
  print_message "Adding brew shellenv init to $HOME/.zprofile"
  {
    echo
    echo 'eval "$(/opt/homebrew/bin/brew shellenv)"'
  } >> "$HOME/.zprofile"
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

print_loading_message
print_loading_message

start_spinner 'Installing bloatware'
{
  print_log_header
  brew bundle install --file "$DOTFILES_DIR/Brewfile"
} &>> "$HOME/.brew_bundle_install.log"
stop_spinner

print_message 'Holy shit, that took ages!'
print_loading_message
print_loading_message

# '------------------------------------'
# ' Setup shell
# '------------------------------------'

start_spinner 'Installing oh my zsh'
{
  print_log_header
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
} &>> "$HOME/.oh_my_zsh_install.log"
stop_spinner

print_message 'Configuring oh my zsh to update automatically'
sed -i '.bak' "s/# zstyle ':omz:update' mode auto/zstyle ':omz:update' mode auto/" "$HOME/.zshrc"

print_loading_message

print_message 'Checking for custom shell setup'
if [ -f "$HOME/.zshrc" ] && grep -q 'kill_it_with_fire_before_it_lays_eggs' "$HOME/.zshrc"; then
  print_message 'Custom shell setup already exists'
else
  print_message 'Adding custom shell setup to zshrc'
  cat "$DOTFILES_DIR/zshrc" >> "$HOME/.zshrc"
fi

print_loading_message
print_loading_message

print_message 'Creating starship config'
mkdir -p "$HOME/.config/"
cat "$DOTFILES_DIR/starship.toml" > "$HOME/.config/starship.toml"

# '------------------------------------'
# ' Configure Git
# '------------------------------------'

print_message 'Configuring git global config'
cat "$DOTFILES_DIR/gitignore_global" > "$HOME/.gitignore_global"

git config --global user.name 'dencoseca'
git config --global rerere.enabled true
git config --global core.excludesfile "$HOME/.gitignore_global"

print_loading_message
print_loading_message

print_message "Praise Poseidon, it's finally over!"
print_message "Let's all relax and drink some lemonade!"
