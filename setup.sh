#!/usr/bin/env zsh

set -euo pipefail

GIT_USERNAME='username'
SIMS_LOADING_MESSAGES=()

while IFS= read -r LINE; do
  if [ -n "$LINE" ]; then
    SIMS_LOADING_MESSAGES+=("$LINE")
  fi
done <'./sims-loading-messages.txt'

NUM_LINES=${#SIMS_LOADING_MESSAGES[@]}

print_loading_message() {
  local RANDOM_INDEX=$((RANDOM % NUM_LINES))
  echo "${SIMS_LOADING_MESSAGES[RANDOM_INDEX]}..."
  sleep "$((RANDOM % 3))"
}

cd $HOME || exit 1

echo 'installing homebrew...'
NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" &>~/.output_homebrew_install.log && echo 'homebrew installed!'

echo 'creating Brewfile...'
cat <<EOM >~/Brewfile
# formulae
brew "git"
brew "tree"
brew "jq"
brew "neofetch"
brew "tldr"
brew "cmatrix"
brew "nvm"
brew "starship"

# casks
cask "rectangle"
cask "mos"
cask "appcleaner"
cask "docker"
cask "meetingbar"
cask "warp"
cask "slack"
cask "alfred"
cask "bartender"
cask "jetbrains-toolbox"
cask "spotify"
cask "brave-browser"

# mac app store
mas "Bitwarden", id: 1352778147
mas "Bear", id: 1091189122
mas "Things", id: 904280696
mas "WhatsApp", id: 1147396723
mas "NordVPN", id: 905953485
EOM

print_loading_message
print_loading_message

echo 'installing apps...'
brew bundle install &>~/.output_brew_bundle_install.log && echo 'brew install complete!'

print_loading_message

echo 'installing ohmyzsh...'
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" &>~/.output_ohmyzsh_install.log

echo 'setting ohmyzsh to update automatically...'
sed -i '' 's/# zstyle \x27:omz:update\x27 mode auto/zstyle \x27:omz:update\x27 mode auto/' ~/.zshrc

print_loading_message

echo 'adding custom shell setup to .zshrc...'
cat <<EOM >>~/.zshrc

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
function cjq() {
    curl $1 | jq
}

# zsh
alias src="source ~/.zshrc"
alias zshc="edit ~/.zshrc"
alias zshb="cp ~/.zshrc ~/.zshrc.backup"

# java
alias javals="/usr/libexec/java_home -V"
function javasw() {
  export JAVA_HOME=$(/usr/libexec/java_home -v "$1")
}

# docker
alias docker-sc="echo 'stopping containers' && docker ps -a -q | xargs -r docker stop"
alias docker-rc="echo 'removing containers' && docker ps -a -q | xargs -r docker rm"
alias docker-rv="echo 'removing volumes' && docker volume rm \$(docker volume ls -q)"
alias docker-cc="docker-sc && docker-rc"
alias docker-ca="docker-sc && docker-rc && docker-rv"

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
git config --global user.name $GIT_USERNAME
git config --global rerere.enabled true

print_loading_message

echo 'cleaning up temp files...'
rm ~/Brewfile

print_loading_message
print_loading_message

echo 'finished setup!'
exit 0
