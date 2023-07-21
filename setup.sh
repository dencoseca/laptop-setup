#!/usr/bin/env zsh

set -euo pipefail

# Setup loading messages
LOADING_MESSAGES=()
while IFS= read -r LINE; do
  if [ -n "$LINE" ]; then
    LOADING_MESSAGES+=("$LINE")
  fi
done <'./loading-messages.txt'
NUM_LINES=${#LOADING_MESSAGES[@]}

print_loading_message() {
  local RANDOM_INDEX=$((RANDOM % NUM_LINES))
  echo "${LOADING_MESSAGES[RANDOM_INDEX]}..."
  sleep "$((RANDOM % 3))"
}

# Setup spinner
SPINNER_PID=
start_spinner() {
  set +m
  echo -n "$1         "
  { while :; do for X in '  •     ' '   •    ' '    •   ' '     •  ' '      • ' '     •  ' '    •   ' '   •    ' '  •     ' ' •      '; do
    echo -en "\b\b\b\b\b\b\b\b$X"
    sleep 0.1
  done; done & } 2>/dev/null
  SPINNER_PID=$!
}

stop_spinner() {
  { kill -9 $SPINNER_PID && wait; } 2>/dev/null
  set -m
  echo -en "\033[2K\r"
}

trap stop_spinner EXIT

# Do all the stuffs
echo 'creating styles source file...'
cp ./styles.sh ~/.styles.sh

cd ~ || exit 1

start_spinner 'installing homebrew...'
NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" &>~/.output_homebrew_install.log && echo 'homebrew installed!'
stop_spinner

echo 'creating Brewfile...'
cat <<EOM >~/Brewfile
# formulae
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
cask "meetingbar"
cask "mos"
cask "rectangle"
cask "slack"
cask "spotify"
cask "warp"

# mac app store
mas "Bear", id: 1091189122
mas "Bitwarden", id: 1352778147
mas "NordVPN", id: 905953485
mas "Things", id: 904280696
mas "WhatsApp", id: 1147396723
EOM

print_loading_message
print_loading_message

start_spinner 'installing apps...'
brew bundle install &>~/.output_brew_bundle_install.log && echo 'brew install complete!'
stop_spinner

print_loading_message

start_spinner 'installing ohmyzsh...'
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" &>~/.output_ohmyzsh_install.log
stop_spinner

echo 'setting ohmyzsh to update automatically...'
sed -i '' 's/# zstyle \x27:omz:update\x27 mode auto/zstyle \x27:omz:update\x27 mode auto/' ~/.zshrc

print_loading_message

echo 'adding custom shell setup to .zshrc...'
cat <<EOM >>~/.zshrc

###########
## STYLE ##
###########

source ~/.styles.sh

print_message() {
  local STRING="$1"
  local STYLE="$2"

  echo -e "${STYLE}${STRING}${RESET}"
}

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
alias edit="webstorm -e $1"
alias oif="open -a Finder ./"
alias nq="networkQuality"
alias trc="tree -d -L 3 ~/Developer/repos"
cjq() {
  curl $1 | jq
}

# zsh
alias src="source ~/.zshrc"
alias zshc="edit ~/.zshrc"
alias zshb="cp ~/.zshrc ~/.zshrc.backup"

# java
alias javals="/usr/libexec/java_home -V"
javasw() {
  export JAVA_HOME=$(/usr/libexec/java_home -v "$1")
}

# docker
docker_sc() {
  local CONTAINER_NAMES
  CONTAINER_NAMES=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$CONTAINER_NAMES" ]]; then
    print_message "STOPPING CONTAINERS" "$BGreen"
    echo "$CONTAINER_NAMES" | xargs -r docker stop
  else
    print_message "No CONTAINERS to STOP" "$BRed"
  fi
}

docker_rc() {
  local CONTAINER_NAMES
  CONTAINER_NAMES=$(docker ps -a --format "{{.Names}}")
  if [[ -n "$CONTAINER_NAMES" ]]; then
    print_message "REMOVING CONTAINERS" "$BGreen"
    echo "$CONTAINER_NAMES" | xargs -r docker rm
  else
    print_message "No CONTAINERS to REMOVE" "$BRed"
  fi
}

docker_rv() {
  local VOLUME_NAMES
  VOLUME_NAMES=$(docker volume ls -q)
  if [[ -n "$VOLUME_NAMES" ]]; then
    print_message "REMOVING VOLUMES" "$BGreen"
    echo "$VOLUME_NAMES" | xargs -r docker volume rm
  else
    print_message "No VOLUMES to REMOVE" "$BRed"
  fi
}

alias docker-cc="docker_sc && docker_rc"
alias docker-ca="docker_sc && docker_rc && docker_rv"

neofetch
EOM

print_loading_message
print_loading_message

echo 'creating starship config...'
mkdir -p ~/.config/
cat <<EOM >~/.config/starship.toml
[aws]
disabled=true

[gcloud]
disabled=true

[character]
success_symbol = ''
error_symbol = ''
EOM

echo 'setting up git global config...'
git config --global user.name 'dencoSeca'
git config --global rerere.enabled true

print_loading_message

echo 'cleaning up temp files...'
rm ~/Brewfile

print_loading_message
print_loading_message

echo 'finished setup!'
exit 0
