#!/usr/bin/env zsh

set -euo pipefail

print_usage() {
  cat <<- EOF
Your new laptop setup is about to happen. Try not to blink.

Usage:
  zsh -s -- -e <home|work>

Flags:
  -e   Required. Specify the environment ('home' or 'work')
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

print_message "Checking Command Line Tools for Xcode"
if ! xcode-select -p &> /dev/null; then
  print_message "Command Line Tools for Xcode not found. Installing from software update utility"
  {
    touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
    version=$(softwareupdate -l | grep "\*.*Command Line" | tail -n 1 | sed 's/^[^C]* //')
    softwareupdate -i "$version" --verbose
  } &>> "$HOME/.xcode-select-install.log"
else
  print_message "Command Line Tools for Xcode are already installed."
fi

TARGET_DIR="$HOME/Developer/repos/github.com/dencoseca/laptop-setup"
print_message "Summoning the repo into $TARGET_DIR"
mkdir -p "$HOME/Developer/repos/github.com/dencoseca"
if [ -d "$TARGET_DIR/.git" ]; then
  print_message "Repo already exists. Skipping clone so you don't summon duplicates."
else
  git clone https://github.com/dencoseca/laptop-setup.git "$TARGET_DIR"
fi

print_message "Handing off to setup.sh. The Sims loading screen starts now."
cd "$TARGET_DIR"
chmod +x ./setup.sh
if [ -t 0 ]; then
  ./setup.sh -e "$ENV_FLAG"
else
  ./setup.sh -e "$ENV_FLAG" -y
fi
