#!/usr/bin/env zsh

# ████████╗ █████╗ ██╗  ██╗███████╗     █████╗     ██████╗ ███████╗███████╗██████╗     ██████╗ ██████╗ ███████╗ █████╗ ████████╗██╗  ██╗
# ╚══██╔══╝██╔══██╗██║ ██╔╝██╔════╝    ██╔══██╗    ██╔══██╗██╔════╝██╔════╝██╔══██╗    ██╔══██╗██╔══██╗██╔════╝██╔══██╗╚══██╔══╝██║  ██║
#    ██║   ███████║█████╔╝ █████╗      ███████║    ██║  ██║█████╗  █████╗  ██████╔╝    ██████╔╝██████╔╝█████╗  ███████║   ██║   ███████║
#    ██║   ██╔══██║██╔═██╗ ██╔══╝      ██╔══██║    ██║  ██║██╔══╝  ██╔══╝  ██╔═══╝     ██╔══██╗██╔══██╗██╔══╝  ██╔══██║   ██║   ██╔══██║
#    ██║   ██║  ██║██║  ██╗███████╗    ██║  ██║    ██████╔╝███████╗███████╗██║         ██████╔╝██║  ██║███████╗██║  ██║   ██║   ██║  ██║
#    ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝    ╚═╝  ╚═╝    ╚═════╝ ╚══════╝╚══════╝╚═╝         ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝

set -euo pipefail

print_usage() {
  cat <<- EOF
Provides the perfect environment to ignore time, hunger, friends and family,
most of life's responsibilities, stop blinking entirely, and eventually be able
to afford a second home, somewhere in the mediterranean, where you can talk
to your neighbours about how annoying taxes are.

Usage:
  sudo ./setup.sh -e <home|work>

Flags:
  -e   Required. Specify the environment ('home' or 'work')

Dependencies in dotfiles directory:
  - Brewfile.home
  - Brewfile.work
  - docker-config.json
  - gitignore
  - gitconfig
  - starship.toml
  - zshrc

EOF
}

print_message() {
  echo "$1"
  say "$1"
}

exit_with_message() {
  echo "$1"
  echo
  print_usage
  exit 1
}



# ██╗   ██╗ █████╗ ██╗     ██╗██████╗  █████╗ ████████╗███████╗    ██████╗ ██╗   ██╗███╗   ██╗
# ██║   ██║██╔══██╗██║     ██║██╔══██╗██╔══██╗╚══██╔══╝██╔════╝    ██╔══██╗██║   ██║████╗  ██║
# ██║   ██║███████║██║     ██║██║  ██║███████║   ██║   █████╗      ██████╔╝██║   ██║██╔██╗ ██║
# ╚██╗ ██╔╝██╔══██║██║     ██║██║  ██║██╔══██║   ██║   ██╔══╝      ██╔══██╗██║   ██║██║╚██╗██║
#  ╚████╔╝ ██║  ██║███████╗██║██████╔╝██║  ██║   ██║   ███████╗    ██║  ██║╚██████╔╝██║ ╚████║
#   ╚═══╝  ╚═╝  ╚═╝╚══════╝╚═╝╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚══════╝    ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝

ENV_FLAG=''
while getopts ":e:" OPTION; do
  case "${OPTION}" in
  e) ENV_FLAG=${OPTARG} ;;
  *)
    exit_with_message "Congratulations, you managed to screw up copying and pasting a command!"
    ;;
  esac
done

if [ -z "$ENV_FLAG" ]; then
  exit_with_message "Nope, don't delete that bit, that bit's important!"
elif [ "$ENV_FLAG" != "home" ] && [ "$ENV_FLAG" != "work" ]; then
  exit_with_message "It literally tells you what the options are!"
else
  print_message "Using $ENV_FLAG config for setup"
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${0}")" &> /dev/null && pwd)

HOME_BREWFILE="$SCRIPT_DIR/templates/Brewfile.home"
WORK_BREWFILE="$SCRIPT_DIR/templates/Brewfile.work"
DOCKER_CONFIG_FILE="$SCRIPT_DIR/templates/docker-config.json"
GITIGNORE_FILE="$SCRIPT_DIR/templates/gitignore"
GITCONFIG_FILE="$SCRIPT_DIR/templates/gitconfig"
STARSHIP_CONFIG_FILE="$SCRIPT_DIR/templates/starship.toml"
ZSHRC_CONFIG_FILE="$SCRIPT_DIR/templates/zshrc"
ASDF_CONFIG_FILE="$SCRIPT_DIR/templates/asdfrc"
LOADING_MESSAGES_FILE="$SCRIPT_DIR/templates/loading-messages.txt"

REQUIRED_FILES=(
  "$HOME_BREWFILE"
  "$WORK_BREWFILE"
  "$DOCKER_CONFIG_FILE"
  "$GITIGNORE_FILE"
  "$GITCONFIG_FILE"
  "$STARSHIP_CONFIG_FILE"
  "$ZSHRC_CONFIG_FILE"
  "$ASDF_CONFIG_FILE"
  "$LOADING_MESSAGES_FILE"
)

MISSING_FILES=()

for FILE in "${REQUIRED_FILES[@]}"; do
  if [ ! -f "$FILE" ]; then
    MISSING_FILES+=("$(basename "$FILE")")
  fi
done

if [ "${#MISSING_FILES[@]}" -ne 0 ]; then
  say "Where the good stuff at?:"
  for MISSING_FILE in "${MISSING_FILES[@]}"; do
    say "  Ain't no $MISSING_FILE"
  done
  exit_with_message "Get your shit together first"
fi



# ███████╗ ██████╗ ███╗   ███╗███████╗    ███████╗████████╗██╗   ██╗███████╗███████╗
# ██╔════╝██╔═══██╗████╗ ████║██╔════╝    ██╔════╝╚══██╔══╝██║   ██║██╔════╝██╔════╝
# ███████╗██║   ██║██╔████╔██║█████╗      ███████╗   ██║   ██║   ██║█████╗  █████╗
# ╚════██║██║   ██║██║╚██╔╝██║██╔══╝      ╚════██║   ██║   ██║   ██║██╔══╝  ██╔══╝
# ███████║╚██████╔╝██║ ╚═╝ ██║███████╗    ███████║   ██║   ╚██████╔╝██║     ██║
# ╚══════╝ ╚═════╝ ╚═╝     ╚═╝╚══════╝    ╚══════╝   ╚═╝    ╚═════╝ ╚═╝     ╚═╝

ignore_dave_and_leave_him_in_space_to_suffocate() {
  print_message "...I'm sorry Dave, I'm afraid I can't do that"
}

print_log_header() {
  echo
  echo "##------------------------------------------------##"
  echo "##--------  $(date)  --------##"
  echo "##------------------------------------------------##"
}

LOADING_MESSAGES=()

while IFS= read -r LINE; do
  if [ -n "$LINE" ]; then
    LOADING_MESSAGES+=("$LINE")
  fi
done < "$LOADING_MESSAGES_FILE"

NUM_LINES=${#LOADING_MESSAGES[@]}

print_loading_message() {
  local RANDOM_INDEX=$((RANDOM % NUM_LINES))
  local MESSAGE="${LOADING_MESSAGES[RANDOM_INDEX]}"

  sleep "$((RANDOM % 3))"
  echo "$MESSAGE"
  say "$MESSAGE"
  sleep "$((RANDOM % 3))"
}

SPINNER_PID=''
SPINNER_MESSAGE=''

start_spinner() {
  SPINNER_MESSAGE="$1"
  set +m
  echo -n "$1         "

  { while :; do for X in '  •     ' '   •    ' '    •   ' '     •  ' '      • ' '     •  ' '    •   ' '   •    ' '  •     ' ' •      '; do
    echo -en "\b\b\b\b\b\b\b\b$X"
    sleep 0.1
  done; done & } 2> /dev/null

  SPINNER_PID=$!
  say "$1"
}

stop_spinner() {
  { kill -9 $SPINNER_PID && wait; } 2> /dev/null
  set -m
  echo -en "\033[2K\r"
  echo "${SPINNER_MESSAGE}... done!"
}

trap 'ignore_dave_and_leave_him_in_space_to_suffocate' ERR
trap 'stop_spinner' EXIT



# ██╗  ██╗ ██████╗ ██████╗ ██████╗ ███████╗     ██████╗██╗     ██╗    ████████╗ ██████╗  ██████╗ ██╗     ███████╗
# ╚██╗██╔╝██╔════╝██╔═══██╗██╔══██╗██╔════╝    ██╔════╝██║     ██║    ╚══██╔══╝██╔═══██╗██╔═══██╗██║     ██╔════╝
#  ╚███╔╝ ██║     ██║   ██║██║  ██║█████╗      ██║     ██║     ██║       ██║   ██║   ██║██║   ██║██║     ███████╗
#  ██╔██╗ ██║     ██║   ██║██║  ██║██╔══╝      ██║     ██║     ██║       ██║   ██║   ██║██║   ██║██║     ╚════██║
# ██╔╝ ██╗╚██████╗╚██████╔╝██████╔╝███████╗    ╚██████╗███████╗██║       ██║   ╚██████╔╝╚██████╔╝███████╗███████║
# ╚═╝  ╚═╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝     ╚═════╝╚══════╝╚═╝       ╚═╝    ╚═════╝  ╚═════╝ ╚══════╝╚══════╝

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



# ███╗   ███╗ █████╗  ██████╗     ██████╗ ███████╗    ██████╗ ███████╗███████╗ █████╗ ██╗   ██╗██╗  ████████╗███████╗
# ████╗ ████║██╔══██╗██╔════╝    ██╔═══██╗██╔════╝    ██╔══██╗██╔════╝██╔════╝██╔══██╗██║   ██║██║  ╚══██╔══╝██╔════╝
# ██╔████╔██║███████║██║         ██║   ██║███████╗    ██║  ██║█████╗  █████╗  ███████║██║   ██║██║     ██║   ███████╗
# ██║╚██╔╝██║██╔══██║██║         ██║   ██║╚════██║    ██║  ██║██╔══╝  ██╔══╝  ██╔══██║██║   ██║██║     ██║   ╚════██║
# ██║ ╚═╝ ██║██║  ██║╚██████╗    ╚██████╔╝███████║    ██████╔╝███████╗██║     ██║  ██║╚██████╔╝███████╗██║   ███████║
# ╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝     ╚═════╝ ╚══════╝    ╚═════╝ ╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝   ╚══════╝

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



# ██╗███╗   ██╗███████╗████████╗ █████╗ ██╗     ██╗         ██████╗ ██╗      ██████╗  █████╗ ████████╗██╗    ██╗ █████╗ ██████╗ ███████╗
# ██║████╗  ██║██╔════╝╚══██╔══╝██╔══██╗██║     ██║         ██╔══██╗██║     ██╔═══██╗██╔══██╗╚══██╔══╝██║    ██║██╔══██╗██╔══██╗██╔════╝
# ██║██╔██╗ ██║███████╗   ██║   ███████║██║     ██║         ██████╔╝██║     ██║   ██║███████║   ██║   ██║ █╗ ██║███████║██████╔╝█████╗
# ██║██║╚██╗██║╚════██║   ██║   ██╔══██║██║     ██║         ██╔══██╗██║     ██║   ██║██╔══██║   ██║   ██║███╗██║██╔══██║██╔══██╗██╔══╝
# ██║██║ ╚████║███████║   ██║   ██║  ██║███████╗███████╗    ██████╔╝███████╗╚██████╔╝██║  ██║   ██║   ╚███╔███╔╝██║  ██║██║  ██║███████╗
# ╚═╝╚═╝  ╚═══╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚══════╝    ╚═════╝ ╚══════╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝    ╚══╝╚══╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝

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

BREWFILE=''
if [ "$ENV_FLAG" == 'home' ]; then
  BREWFILE=$HOME_BREWFILE
else
  BREWFILE=$WORK_BREWFILE
fi
start_spinner 'Installing bloatware'
{
  print_log_header
  brew bundle install --file "$BREWFILE"
} &>> "$HOME/.brew_bundle_install.log"
stop_spinner

print_message 'Holy shit, that took ages!'
print_loading_message
print_loading_message



#  ██████╗ ██████╗ ███╗   ██╗████████╗ █████╗ ██╗███╗   ██╗███████╗██████╗     ██████╗ ██╗   ██╗███╗   ██╗████████╗██╗███╗   ███╗███████╗
# ██╔════╝██╔═══██╗████╗  ██║╚══██╔══╝██╔══██╗██║████╗  ██║██╔════╝██╔══██╗    ██╔══██╗██║   ██║████╗  ██║╚══██╔══╝██║████╗ ████║██╔════╝
# ██║     ██║   ██║██╔██╗ ██║   ██║   ███████║██║██╔██╗ ██║█████╗  ██████╔╝    ██████╔╝██║   ██║██╔██╗ ██║   ██║   ██║██╔████╔██║█████╗
# ██║     ██║   ██║██║╚██╗██║   ██║   ██╔══██║██║██║╚██╗██║██╔══╝  ██╔══██╗    ██╔══██╗██║   ██║██║╚██╗██║   ██║   ██║██║╚██╔╝██║██╔══╝
# ╚██████╗╚██████╔╝██║ ╚████║   ██║   ██║  ██║██║██║ ╚████║███████╗██║  ██║    ██║  ██║╚██████╔╝██║ ╚████║   ██║   ██║██║ ╚═╝ ██║███████╗
#  ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝    ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝   ╚═╝╚═╝     ╚═╝╚══════╝

print_message "Configuring Docker"
mkdir -p "$HOME/.docker/"
cat "$DOCKER_CONFIG_FILE" > "$HOME/.docker/config.json"
brew services start colima



# ███████╗██╗  ██╗███████╗██╗     ██╗
# ██╔════╝██║  ██║██╔════╝██║     ██║
# ███████╗███████║█████╗  ██║     ██║
# ╚════██║██╔══██║██╔══╝  ██║     ██║
# ███████║██║  ██║███████╗███████╗███████╗
# ╚══════╝╚═╝  ╚═╝╚══════╝╚══════╝╚══════╝

start_spinner 'Installing oh my zsh'
{
  print_log_header
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
} &>> "$HOME/.oh_my_zsh_install.log"
stop_spinner

print_message 'Backing up zshrc'
cp "$HOME/.zshrc" "$HOME/.zshrc.bak"

print_loading_message

print_message 'Customising shell'
cat "$ZSHRC_CONFIG_FILE" > "$HOME/.zshrc"

print_loading_message
print_loading_message

print_message 'Creating starship config'
mkdir -p "$HOME/.config/"
cat "$STARSHIP_CONFIG_FILE" > "$HOME/.config/starship.toml"

print_message 'Installing Warp themes'
mkdir -p "$HOME/.warp"
git clone https://github.com/warpdotdev/themes.git "$HOME/.warp/"



#  ██████╗ ██╗████████╗
# ██╔════╝ ██║╚══██╔══╝
# ██║  ███╗██║   ██║
# ██║   ██║██║   ██║
# ╚██████╔╝██║   ██║
#  ╚═════╝ ╚═╝   ╚═╝

print_message 'Configuring git global config'
cat "$GITIGNORE_FILE" > "$HOME/.gitignore"
cat "$GITCONFIG_FILE" > "$HOME/.gitconfig"

print_loading_message
print_loading_message



# ██╗      █████╗ ███╗   ██╗ ██████╗ ██╗   ██╗ █████╗  ██████╗ ███████╗    ██████╗ ██╗   ██╗███╗   ██╗████████╗██╗███╗   ███╗███████╗███████╗
# ██║     ██╔══██╗████╗  ██║██╔════╝ ██║   ██║██╔══██╗██╔════╝ ██╔════╝    ██╔══██╗██║   ██║████╗  ██║╚══██╔══╝██║████╗ ████║██╔════╝██╔════╝
# ██║     ███████║██╔██╗ ██║██║  ███╗██║   ██║███████║██║  ███╗█████╗      ██████╔╝██║   ██║██╔██╗ ██║   ██║   ██║██╔████╔██║█████╗  ███████╗
# ██║     ██╔══██║██║╚██╗██║██║   ██║██║   ██║██╔══██║██║   ██║██╔══╝      ██╔══██╗██║   ██║██║╚██╗██║   ██║   ██║██║╚██╔╝██║██╔══╝  ╚════██║
# ███████╗██║  ██║██║ ╚████║╚██████╔╝╚██████╔╝██║  ██║╚██████╔╝███████╗    ██║  ██║╚██████╔╝██║ ╚████║   ██║   ██║██║ ╚═╝ ██║███████╗███████║
# ╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝    ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝   ╚═╝╚═╝     ╚═╝╚══════╝╚══════╝

print_message 'Configuring asdf'
cat "$ASDF_CONFIG_FILE" > "$HOME/.asdfrc"

print_message 'Install asdf completions'
git clone https://github.com/asdf-vm/asdf.git "$HOME/.asdf"

print_message 'Installing latest version of go'
asdf plugin add golang https://github.com/asdf-community/asdf-golang.git
asdf install golang latest
asdf set golang latest



#  ██████╗██╗  ██╗███████╗███████╗███████╗    ██╗  ██╗██╗███████╗███████╗
# ██╔════╝██║  ██║██╔════╝██╔════╝██╔════╝    ██║ ██╔╝██║██╔════╝██╔════╝
# ██║     ███████║█████╗  █████╗  ███████╗    █████╔╝ ██║███████╗███████╗
# ██║     ██╔══██║██╔══╝  ██╔══╝  ╚════██║    ██╔═██╗ ██║╚════██║╚════██║
# ╚██████╗██║  ██║███████╗██║     ███████║    ██║  ██╗██║███████║███████║
#  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝     ╚══════╝    ╚═╝  ╚═╝╚═╝╚══════╝╚══════╝

print_message 'Installing custom cli tools'
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" GOPROXY="direct" go install github.com/dencoseca/biskit@latest
GOBIN="$HOME/.local/bin" GOPROXY="direct" go install github.com/dencoseca/boxi@latest
GOBIN="$HOME/.local/bin" GOPROXY="direct" go install github.com/dencoseca/jangle@latest



# ██████╗ ██████╗ ██╗███╗   ██╗██╗  ██╗    ██╗     ███████╗███╗   ███╗ ██████╗ ███╗   ██╗ █████╗ ██████╗ ███████╗
# ██╔══██╗██╔══██╗██║████╗  ██║██║ ██╔╝    ██║     ██╔════╝████╗ ████║██╔═══██╗████╗  ██║██╔══██╗██╔══██╗██╔════╝
# ██║  ██║██████╔╝██║██╔██╗ ██║█████╔╝     ██║     █████╗  ██╔████╔██║██║   ██║██╔██╗ ██║███████║██║  ██║█████╗
# ██║  ██║██╔══██╗██║██║╚██╗██║██╔═██╗     ██║     ██╔══╝  ██║╚██╔╝██║██║   ██║██║╚██╗██║██╔══██║██║  ██║██╔══╝
# ██████╔╝██║  ██║██║██║ ╚████║██║  ██╗    ███████╗███████╗██║ ╚═╝ ██║╚██████╔╝██║ ╚████║██║  ██║██████╔╝███████╗
# ╚═════╝ ╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝    ╚══════╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═╝╚═════╝ ╚══════╝

print_message "Praise Poseidon, it's finally over!"
print_message "Let's all relax and drink some lemonade!"
