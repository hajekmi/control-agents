#!/bin/sh
set -eu

REPO="${REPO:-hajekmi/control-agents}"
VERSION="${VERSION:-latest}"
PREFIX="${PREFIX:-"$HOME/.local"}"
BIN_DIR="${BIN_DIR:-"$PREFIX/bin"}"
XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-"$HOME/.config"}"
CONFIG_DIR="${CONFIG_DIR:-"$XDG_CONFIG_HOME/control-agents"}"
SYSTEMD_USER_DIR="${SYSTEMD_USER_DIR:-"$XDG_CONFIG_HOME/systemd/user"}"
ENV_FILE="${ENV_FILE:-"$CONFIG_DIR/env"}"
SERVICE_FILE="${SERVICE_FILE:-"$SYSTEMD_USER_DIR/control-agents.service"}"
ORIGINAL_PATH="$PATH"
LANG="C.UTF-8"
LC_ALL="C.UTF-8"
PATH="$BIN_DIR:$PATH"
export LANG LC_ALL PATH

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

detect_platform() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux) ;;
    *) die "unsupported OS: $os" ;;
  esac

  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "unsupported architecture: $arch" ;;
  esac

  printf '%s-%s' "$os" "$arch"
}

download() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
  else
    die "curl or wget is required"
  fi
}

generate_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
    return
  fi
  if command -v od >/dev/null 2>&1; then
    od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
    printf '\n'
    return
  fi
  die "openssl or od is required to generate CONTROL_AGENTS_PASSWORD"
}

make_tmp_dir() {
  for base in "${TMPDIR:-}" "${XDG_RUNTIME_DIR:-}" "$HOME/.cache" "/tmp"; do
    [ -n "$base" ] || continue
    mkdir -p "$base" 2>/dev/null || continue
    [ -d "$base" ] && [ -w "$base" ] || continue
    mktemp -d "$base/control-agents-install.XXXXXX" && return
  done
  die "could not create a temporary directory"
}

write_env_file() {
  if [ -f "$ENV_FILE" ]; then
    return
  fi

  mkdir -p "$CONFIG_DIR"
  password="$(generate_password)"
  (
    umask 077
    {
      printf 'CONTROL_AGENTS_PASSWORD=%s\n' "$password"
      printf 'CONTROL_AGENTS_BIND_ADDR=0.0.0.0\n'
      printf 'CONTROL_AGENTS_PORT=8080\n'
    } > "$ENV_FILE"
  )
}

write_service_file() {
  mkdir -p "$SYSTEMD_USER_DIR"
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Control Agents
After=network.target

[Service]
Type=simple
UMask=0077
Environment=CONTROL_AGENTS_BIND_ADDR=0.0.0.0
Environment=CONTROL_AGENTS_PORT=8080
Environment=CONTROL_AGENTS_STATE_DIR=$HOME/.local/state/control-agents
EnvironmentFile=$ENV_FILE
ExecStart=/usr/bin/env PATH=$BIN_DIR:/home/linuxbrew/.linuxbrew/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin LANG=C.UTF-8 LC_ALL=C.UTF-8 $BIN_DIR/control-agents-server
StandardOutput=journal
StandardError=journal
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=false
ProtectSystem=full
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LimitCORE=0

[Install]
WantedBy=default.target
EOF
  chmod 0644 "$SERVICE_FILE"
}

install_binaries() {
  platform="$(detect_platform)"
  tmp_dir="$(make_tmp_dir)"
  trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM
  server_file="$tmp_dir/control-agents-server"
  client_file="$tmp_dir/control-agents"

  if [ -n "${LOCAL_BIN_DIR:-}" ]; then
    cp "$LOCAL_BIN_DIR/control-agents-server" "$server_file"
    cp "$LOCAL_BIN_DIR/control-agents" "$client_file"
  else
    server_asset="control-agents-server-$platform"
    client_asset="control-agents-$platform"
    server_file="$tmp_dir/$server_asset"
    client_file="$tmp_dir/$client_asset"

    if [ "$VERSION" = "latest" ]; then
      base_url="https://github.com/$REPO/releases/latest/download"
    else
      case "$VERSION" in
        v*) tag="$VERSION" ;;
        *) tag="v$VERSION" ;;
      esac
      base_url="https://github.com/$REPO/releases/download/$tag"
    fi

    download "$base_url/$server_asset" "$server_file"
    download "$base_url/$client_asset" "$client_file"
    need_cmd sha256sum
    download "$base_url/sha256sums.txt" "$tmp_dir/sha256sums.txt"
    server_sum="$(sed -n "s/^\\([0-9a-fA-F]\\{64\\}\\)  $server_asset$/\\1/p" "$tmp_dir/sha256sums.txt")"
    client_sum="$(sed -n "s/^\\([0-9a-fA-F]\\{64\\}\\)  $client_asset$/\\1/p" "$tmp_dir/sha256sums.txt")"
    [ -n "$server_sum" ] || die "checksum manifest does not contain $server_asset"
    [ -n "$client_sum" ] || die "checksum manifest does not contain $client_asset"
    (
      cd "$tmp_dir"
      printf '%s  %s\n%s  %s\n' "$server_sum" "$server_asset" "$client_sum" "$client_asset" | sha256sum -c -
    )
  fi

  mkdir -p "$BIN_DIR"
  install -m 0755 "$server_file" "$BIN_DIR/control-agents-server"
  install -m 0755 "$client_file" "$BIN_DIR/control-agents"
}

verify_tmux() {
  selected_tmux="$BIN_DIR/tmux"
  [ -x "$selected_tmux" ] || \
    die "tmux 3.7b is required at $selected_tmux; run install-tmux.sh with the same BIN_DIR"
  selected_version="$("$selected_tmux" -V)"
  [ "$selected_version" = "tmux 3.7b" ] || \
    die "tmux 3.7b is required; selected $selected_tmux reports $selected_version"
}

check_ttyd() {
  if ! command -v ttyd >/dev/null 2>&1; then
    warn "ttyd is missing; install it before starting Control Agents"
  fi
}

reload_systemd() {
  if command -v systemctl >/dev/null 2>&1; then
    if systemctl --user daemon-reload >/dev/null 2>&1; then
      return
    fi
    warn "systemctl --user daemon-reload failed; run it manually after login"
  else
    warn "systemctl is unavailable; service file was installed but not reloaded"
  fi
}

case "$BIN_DIR" in
  /*) ;;
  *) die "BIN_DIR must be an absolute path" ;;
esac

need_cmd uname
need_cmd tr
need_cmd mktemp
need_cmd install

verify_tmux
install_binaries
write_env_file
write_service_file
reload_systemd
check_ttyd

case ":$ORIGINAL_PATH:" in
  *":$BIN_DIR:"*) ;;
  *) warn "$BIN_DIR is not in your shell PATH; run: export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

port="$(sed -n 's/^CONTROL_AGENTS_PORT=//p' "$ENV_FILE" | tail -n 1)"
[ -n "$port" ] || port="8080"

printf '\nInstalled Control Agents.\n'
printf 'Binaries: %s/control-agents-server, %s/control-agents\n' "$BIN_DIR" "$BIN_DIR"
printf 'Config:   %s\n' "$ENV_FILE"
printf 'Service:  %s\n\n' "$SERVICE_FILE"
printf 'Run the service and client as this same Unix user. Next commands:\n'
printf '  systemctl --user enable control-agents.service\n'
printf '  systemctl --user restart control-agents.service\n'
printf '  control-agents\n\n'
printf 'Then open:\n'
printf '  https://<host>:%s\n' "$port"
