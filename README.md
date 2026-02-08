# laptop-setup

| Flags | Valid Values |
|-------|--------------|
| -e    | home, work   |
| -y    | (auto-yes)   |

```shell
curl -fsSL https://raw.githubusercontent.com/dencoseca/laptop-setup/main/bootstrap.sh -o bootstrap.sh
zsh bootstrap.sh -e work
```

For non-interactive use (auto-yes):
```shell
zsh bootstrap.sh -e work -y
```
