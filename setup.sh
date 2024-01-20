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

cd ~ || exit 1

# '------------------------------------'
# ' Set MacOS defaults
# '------------------------------------'

print_message 'Setting MacOS defaults'
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
} &>> ~/.setting_macos_defaults.log

# '------------------------------------'
# ' Install bloatware
# '------------------------------------'

start_spinner 'Installing homebrew'
{
  print_log_header
  NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
} &>> ~/.homebrew_install.log
stop_spinner

if [ -f ~/.zprofile ] && grep -q '/opt/homebrew/bin/brew shellenv' ~/.zprofile; then
  print_message 'Brew already exists in system PATH'
else
  print_message 'Adding brew to system PATH'
  {
    echo
    echo 'eval "$(/opt/homebrew/bin/brew shellenv)"'
  } >> ~/.zprofile
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

print_message 'Creating Brewfile'
cat << 'EOF' > ~/Brewfile
# formulae
brew "bat"
brew "bash"
brew "cmatrix"
brew "git"
brew "jq"
brew "neofetch"
brew "nvm"
brew "starship"
brew "tldr"
brew "tree"

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
} &>> ~/.brew_bundle_install.log
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
} &>> ~/.oh_my_zsh_install.log
stop_spinner

print_message 'Configuring oh my zsh to update automatically'
sed -i '.bak' "s/# zstyle ':omz:update' mode auto/zstyle ':omz:update' mode auto/" ~/.zshrc

print_loading_message

if [ -f ~/.zshrc ] && grep -q 'neofetch' ~/.zshrc; then
  print_message 'Custom shell setup already exists'
else
  print_message 'Adding custom shell setup to zshrc'
  cat << 'EOF' >> ~/.zshrc

################
## TOOL SETUP ##
################

# starship prompt
eval "$(starship init zsh)"

# nvm
export NVM_DIR="$HOME/.nvm"
  [ -s "/opt/homebrew/opt/nvm/nvm.sh" ] && \. "/opt/homebrew/opt/nvm/nvm.sh"  # This loads nvm
  [ -s "/opt/homebrew/opt/nvm/etc/bash_completion.d/nvm" ] && \. "/opt/homebrew/opt/nvm/etc/bash_completion.d/nvm"  # This loads nvm bash_completion

################################
## CUSTOM ALIASES / FUNCTIONS ##
################################

# general
alias repos="cd ~/Developer/repos"
alias sandbox="cd ~/Developer/sandbox"
alias down="cd ~/Downloads"
alias edit="webstorm -e $1"
alias oif="open -a Finder ./"
alias nq="networkQuality"
alias trc="tree -d -L 3 ~/Developer/repos"

cjq() {
  curl "$@" | jq
}

print_message() {
  local reset='\033[0m'
  local bred='\033[1;31m'
  local bgreen='\033[1;32m'
  local message="$1"
  local msg_type="$2"
  local style

  case $msg_type in
    "danger") style=$bred ;;
    "success") style=$bgreen ;;
    *) style=$reset ;;
  esac

  echo -e "${style}${message}${reset}"
}

update() {
  print_message "Updating brew packages" "success"
  brew update && brew upgrade
  
  print_message "Updating global npm packages" "success"
  npm update -g
}

# zsh
alias src="source ~/.zshrc"
alias zshc="edit ~/.zshrc"
alias zshb="cp ~/.zshrc ~/.zshrc.bak"

# java
alias javals="/usr/libexec/java_home -V"
javasw() {
  export JAVA_HOME=$(/usr/libexec/java_home -v "$1")
}

# docker
docker_sc() {
  local container_names
  container_names=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$container_names" ]]; then
    print_message "STOPPING CONTAINERS" "success"
    echo "$container_names" | xargs -r docker stop
  else
    print_message "No CONTAINERS to STOP" "danger"
  fi
}

docker_rc() {
  local container_names
  container_names=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$container_names" ]]; then
    print_message "REMOVING CONTAINERS" "success"
    echo "$container_names" | xargs -r docker rm
  else
    print_message "No CONTAINERS to REMOVE" "danger"
  fi
}

docker_rv() {
  local volume_names
  volume_names=$(docker volume ls -q)
  if [[ -n "$volume_names" ]]; then
    print_message "REMOVING VOLUMES" "success"
    echo "$volume_names" | xargs -r docker volume rm
  else
    print_message "No VOLUMES to REMOVE" "danger"
  fi
}

docker_ri() {
  local image_names=$(docker images -q)
  if [[ -n "$image_names" ]]; then
    print_message "REMOVING IMAGES" "success"
    echo "$image_names" | xargs -r docker rmi
  else
    print_message "No IMAGES to REMOVE" "danger"
  fi
}

alias docker-cc="docker_sc && docker_rc"
alias docker-ca="docker_sc && docker_rc && docker_rv"

kill_it_with_fire_before_it_lays_eggs() {
  docker_sc
  docker_rc
  docker_rv
  docker_ri
  print_message "Pruning SYSTEM" "success"
  docker system prune -f
}

neofetch
EOF
fi

print_loading_message
print_loading_message

print_message 'Creating starship config'
mkdir -p ~/.config/
cat << 'EOF' > ~/.config/starship.toml
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
git config --global user.name 'dencoseca'
git config --global rerere.enabled true

cat << 'EOF' > ~/.gitignore_global
.DS_Store
/.idea
EOF

git config --global core.excludesfile ~/.gitignore_global

print_loading_message

# '------------------------------------'
# ' Tidy up
# '------------------------------------'

print_message 'Cleaning up temporary files'
if [ -f ~/Brewfile ]; then
  rm ~/Brewfile
fi
if [ -f ~/Brewfile.lock.json ]; then
  rm ~/Brewfile.lock.json
fi

print_loading_message
print_loading_message

print_message "Praise Poseidon, it's finally over!"
print_message "Let's all relax and drink some lemonade!"
