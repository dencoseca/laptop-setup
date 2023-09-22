#!/usr/bin/env zsh

set -euo pipefail

trap "echo \"I'm sorry Dave... I'm afraid I can't do that\"" ERR

print_log_header() {
  echo
  echo "##------------------------------------------------##"
  echo "##--------  $(date)  --------##"
  echo "##------------------------------------------------##"
}

# rosebud!;!;!;!;!;!;!;!;!;!;!;!;!;!;
loading_messages=()
while IFS= read -r line; do
  if [ -n "$line" ]; then
    loading_messages+=("$line")
  fi
done < './loading-messages.txt'
num_lines=${#loading_messages[@]}

print_loading_message() {
  sleep "$((RANDOM % 3))"
  local random_index=$((RANDOM % num_lines))
  echo "${loading_messages[random_index]}..."
  sleep "$((RANDOM % 3))"
}

# Pong for the boring parts
spinner_pid=
start_spinner() {
  set +m
  echo -n "$1         "
  { while :; do for X in '  •     ' '   •    ' '    •   ' '     •  ' '      • ' '     •  ' '    •   ' '   •    ' '  •     ' ' •      '; do
    echo -en "\b\b\b\b\b\b\b\b$X"
    sleep 0.1
  done; done & } 2> /dev/null
  spinner_pid=$!
}

stop_spinner() {
  { kill -9 $spinner_pid && wait; } 2> /dev/null
  set -m
  echo -en "\033[2K\r"
}

trap stop_spinner EXIT

cd ~ || exit 1

# Install all the things
start_spinner 'Installing homebrew...'
{
  print_log_header
  NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
} &>> ~/.output_homebrew_install.log
stop_spinner
echo 'Installing homebrew... done!'

echo 'Adding brew to path...'
(
  echo
  echo 'eval "$(/opt/homebrew/bin/brew shellenv)"'
) >> ~/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"

echo 'Creating Brewfile...'
cat << 'EOM' > ~/Brewfile
# formulae
brew "git"
brew "bash"
brew "cmatrix"
brew "jq"
brew "neofetch"
brew "tldr"
brew "tree"
brew "starship"
brew "nvm"

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

start_spinner 'Installing apps...'
{
  print_log_header
  brew bundle install
} &>> ~/.output_brew_bundle_install.log
stop_spinner
echo 'Installing apps... done!'

print_loading_message
print_loading_message

# I literally only want this for like, six, maybe seven git aliases
start_spinner 'Installing ohmyzsh...'
{
  print_log_header
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended
} &>> ~/.output_ohmyzsh_install.log
stop_spinner
echo 'Installing ohmyzsh... done!'

echo 'Configuring ohmyzsh to update automatically...'
sed -i '' "s/# zstyle ':omz:update' mode auto/zstyle ':omz:update' mode auto/" ~/.zshrc

print_loading_message

# Squirtle evolved into Wartortle!
if grep -q 'neofetch' ~/.zshrc; then
  echo 'Custom shell setup already exists...'
else
  echo 'Adding custom shell setup to .zshrc...'
  cat << 'EOM' >> ~/.zshrc

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
    print_message "REMOVING CONTAINERS" "$success"
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
EOM
fi

print_loading_message
print_loading_message

# I don't need coloured arrows to highlight my failure thank you very much
echo 'Creating starship config...'
mkdir -p ~/.config/
cat << 'EOM' > ~/.config/starship.toml
[aws]
disabled=true

[gcloud]
disabled=true

[character]
success_symbol = ''
error_symbol = ''
EOM

# GO AWAY DS_Store! NOBODY INVITED YOU!
echo 'Configuring git global config...'
git config --global user.name 'dencoseca'
git config --global rerere.enabled true
echo '.DS_Store' > ~/.gitignore_global
git config --global core.excludesfile ~/.gitignore_global

print_loading_message

echo 'Cleaning up temporary brew files...'
if [ -f ~/Brewfile ]; then
  rm ~/Brewfile
fi
if [ -f ~/Brewfile.lock.json ]; then
  rm ~/Brewfile.lock.json
fi

print_loading_message
print_loading_message

echo 'Finished setup!'
exit 0
