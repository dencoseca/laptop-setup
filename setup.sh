#!/usr/bin/env zsh

set -euo pipefail

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

if [ -f "$HOME/.zprofile" ] && grep -q '/opt/homebrew/bin/brew shellenv' "$HOME/.zprofile"; then
  print_message 'Brew already exists in system PATH'
else
  print_message 'Adding brew to system PATH'
  {
    echo
    echo 'eval "$(/opt/homebrew/bin/brew shellenv)"'
  } >> "$HOME/.zprofile"
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

print_message 'Creating Brewfile'
cat << 'EOF' > "$HOME/Brewfile"
# formulae
brew "bat"
brew "htop"
brew "jq"
brew "watch"
brew "neofetch"
brew "tldr"
brew "tree"
brew "starship"
brew "git"
brew "nvm"
brew "go"
brew "helm"
brew "skaffold"

# casks
cask "alfred"
cask "appcleaner"
cask "bartender"
cask "brave-browser"
cask "docker"
cask "jetbrains-toolbox"
cask "logi-options-plus"
cask "meetingbar"
cask "mos"
cask "rectangle"
cask "slack"
cask "spotify"
cask "warp"

# mac app store
mas "Amphetamine", id: 937984704
mas "Bear", id: 1091189122
mas "Bitwarden", id: 1352778147
mas "NordVPN", id: 905953485
mas "Things", id: 904280696
mas "WhatsApp", id: 1147396723

EOF

print_loading_message
print_loading_message

start_spinner 'Installing bloatware'
{
  print_log_header
  brew bundle install
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

if [ -f "$HOME/.zshrc" ] && grep -q 'kill_it_with_fire_before_it_lays_eggs' "$HOME/.zshrc"; then
  print_message 'Custom shell setup already exists'
else
  print_message 'Adding custom shell setup to zshrc'
  cat << 'EOF' >> "$HOME/.zshrc"

#################
## TOOL CONFIG ##
#################

# starship
eval "$(starship init zsh)"

# nvm
export NVM_DIR="$HOME/.nvm"
  [ -s "/opt/homebrew/opt/nvm/nvm.sh" ] && \. "/opt/homebrew/opt/nvm/nvm.sh"  # This loads nvm
  [ -s "/opt/homebrew/opt/nvm/etc/bash_completion.d/nvm" ] && \. "/opt/homebrew/opt/nvm/etc/bash_completion.d/nvm"  # This loads nvm bash_completion

# bat
export BAT_THEME='zenburn'

################################
## CUSTOM ALIASES / FUNCTIONS ##
################################

# general
alias src="source $HOME/.zshrc"
alias zshc="edit $HOME/.zshrc"
alias zshb="cp $HOME/.zshrc $HOME/.zshrc.bak"
alias github="cd $HOME/Developer/repos/github.com/dencoseca"
alias repos="cd $HOME/Developer/repos"
alias sandbox="cd $HOME/Developer/sandbox"
alias udemy="cd $HOME/Developer/udemy"
alias dl="cd $HOME/Downloads"
alias dt="cd $HOME/Desktop"
alias edit="webstorm -e $1"
alias oif="open -a Finder ./"
alias nq="networkQuality"
alias trc="tree -d -L 3 $HOME/Developer/repos"
alias d="docker"
alias k="kubectl"
alias npmls="npm list -g --depth=0"

print_message() {
  local reset='\x1B[0m'
  local bred='\x1B[1;31m'
  local bgreen='\x1B[1;32m'
  local byellow='\x1B[1;33m'
  local message="$1"
  local msg_type="$2"
  local style

  case $msg_type in
    "danger") style=$bred ;;
    "success") style=$bgreen ;;
    "warning") style=$byellow ;;
    *) style=$reset ;;
  esac

  echo -e "${style}${message}${reset}"
}

update() {
  print_message "Updating brew packages..." "warning"
  brew update && brew upgrade
  print_message "Brew packages updated!" "success"

  print_message "Updating to latest Node..." "warning"
  nvm install --lts --latest-npm
  nvm use --lts
  print_message "Using latest Node version!" "success"

  print_message "Updating global npm packages..." "warning"
  npm update -g
  print_message "Global npm packages all up to date!" "success"
}

# jetbrains http client
alias http-cls="cat .idea/httpRequests/http-client.cookies"
alias http-cc="rm .idea/httpRequests/http-client.cookies"

http-rmc() {
  local column=$1
  local value=$2
  local filepath=.idea/httpRequests/http-client.cookies

  if [ ! -f $filepath ]; then
    echo "There is no http-client.cookies file in this project"
  fi

  if [ -z "$column" ]; then
    echo "Please provide a column"
    return
  fi

  if [ -z "$value" ]; then
    echo "Please provide a value"
    return
  fi

  if [ "$column" = "domain" ]; then
    action=$(awk -v val="$value" 'BEGIN{OFS=FS="\t"} $1!=val' $filepath)
  elif [ "$column" = "name" ]; then
    action=$(awk -v val="$value" 'BEGIN{OFS=FS="\t"} $3!=val' $filepath)
  else
    echo "Invalid column. Please specify either 'domain' or 'name'"
    return
  fi

  if [ -z "$action" ]; then
    echo "No matching lines were found."
  else
    echo "$action" > $filepath
    echo "The matching cookie(s) have been removed."
  fi
}

# java
alias javals="/usr/libexec/java_home -V"

javasw() {
  export JAVA_HOME=$(/usr/libexec/java_home -v "$1")
}

# docker
docker-sc() {
  local container_names
  container_names=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$container_names" ]]; then
    print_message "STOPPING CONTAINERS" "success"
    echo "$container_names" | xargs -r docker stop
  else
    print_message "No CONTAINERS to STOP" "danger"
  fi
}

docker-rc() {
  local container_names
  container_names=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$container_names" ]]; then
    print_message "REMOVING CONTAINERS" "success"
    echo "$container_names" | xargs -r docker rm
  else
    print_message "No CONTAINERS to REMOVE" "danger"
  fi
}

docker-rv() {
  local volume_names
  volume_names=$(docker volume ls -q)
  if [[ -n "$volume_names" ]]; then
    print_message "REMOVING VOLUMES" "success"
    echo "$volume_names" | xargs -r docker volume rm
  else
    print_message "No VOLUMES to REMOVE" "danger"
  fi
}

docker-ri() {
  local image_names=$(docker images -q)
  if [[ -n "$image_names" ]]; then
    print_message "REMOVING IMAGES" "success"
    echo "$image_names" | xargs -r docker rmi
  else
    print_message "No IMAGES to REMOVE" "danger"
  fi
}

alias docker-cc="docker-sc && docker-rc"
alias docker-ca="docker-sc && docker-rc && docker-rv"

kill-it-with-fire-before-it-lays-eggs() {
  docker-sc
  docker-rc
  docker-rv
  docker-ri
  print_message "Pruning SYSTEM" "success"
  docker system prune -f
}

EOF
fi

print_loading_message
print_loading_message

print_message 'Creating starship config'
mkdir -p "$HOME/.config/"
cat << 'EOF' > "$HOME/.config/starship.toml"
[aws]
disabled=true

[gcloud]
disabled=true

[character]
success_symbol = ''
error_symbol = ''

EOF

# '------------------------------------'
# ' Configure Git
# '------------------------------------'

print_message 'Configuring git global config'
cat << 'EOF' > "$HOME/.gitignore_global"
.DS_Store
/.idea

EOF

git config --global user.name 'dencoseca'
git config --global rerere.enabled true
git config --global core.excludesfile "$HOME/.gitignore_global"

print_loading_message

# '------------------------------------'
# ' Tidy up
# '------------------------------------'

print_message 'Cleaning up temporary files'
if [ -f "$HOME/Brewfile" ]; then
  rm "$HOME/Brewfile"
fi
if [ -f "$HOME/Brewfile.lock.json" ]; then
  rm "$HOME/Brewfile.lock.json"
fi

print_loading_message
print_loading_message

print_message "Praise Poseidon, it's finally over!"
print_message "Let's all relax and drink some lemonade!"
