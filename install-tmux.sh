#!/bin/sh
set -eu

TMUX_VERSION="3.7b"
TMUX_SHA256="87f2e99e3b685973f2ca002ffd6ed7e51a5744f7009daae5a15670b6d532db96"
BIN_DIR="${BIN_DIR:-"$HOME/.local/bin"}"
MAKE_JOBS="${MAKE_JOBS:-2}"
LANG="C.UTF-8"
LC_ALL="C.UTF-8"
export LANG LC_ALL

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

download() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --proto '=https' --tlsv1.2 --fail --location --show-error --silent \
      --output "$output" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only -qO "$output" "$url"
  else
    die "curl or wget is required"
  fi
}

make_tmp_dir() {
  for base in "${TMPDIR:-}" "${XDG_RUNTIME_DIR:-}" "$HOME/.cache" "/tmp"; do
    [ -n "$base" ] || continue
    mkdir -p "$base" 2>/dev/null || continue
    [ -d "$base" ] && [ -w "$base" ] || continue
    mktemp -d "$base/control-agents-tmux.XXXXXX" && return
  done
  die "could not create a temporary directory"
}

for command in sha256sum tar make pkg-config bison cc mktemp install mv; do
  need_cmd "$command"
done

case "$BIN_DIR" in
  /*) ;;
  *) die "BIN_DIR must be an absolute path" ;;
esac

tmp_dir="$(make_tmp_dir)"
destination_tmp=""
cleanup() {
  if [ -n "$destination_tmp" ]; then
    rm -f "$destination_tmp"
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
archive="$tmp_dir/tmux-$TMUX_VERSION.tar.gz"
source_dir="$tmp_dir/tmux-$TMUX_VERSION"
stage_dir="$tmp_dir/stage"
download \
  "https://github.com/tmux/tmux/releases/download/$TMUX_VERSION/tmux-$TMUX_VERSION.tar.gz" \
  "$archive"
printf '%s  %s\n' "$TMUX_SHA256" "$archive" | sha256sum --check
tar -xzf "$archive" -C "$tmp_dir"
[ -x "$source_dir/configure" ] || die "tmux source archive is incomplete"

(
  cd "$source_dir"
  ./configure --prefix="$stage_dir"
  make -j "$MAKE_JOBS"
  make install
)

built_tmux="$stage_dir/bin/tmux"
[ -x "$built_tmux" ] || die "tmux build did not produce $built_tmux"
built_version="$("$built_tmux" -V)"
[ "$built_version" = "tmux $TMUX_VERSION" ] || \
  die "built tmux version is $built_version, expected tmux $TMUX_VERSION"

mkdir -p "$BIN_DIR"
destination_tmp="$(mktemp "$BIN_DIR/.tmux.XXXXXX")"
install -m 0755 "$built_tmux" "$destination_tmp"
staged_version="$("$destination_tmp" -V)"
[ "$staged_version" = "tmux $TMUX_VERSION" ] || \
  die "staged tmux version is $staged_version, expected tmux $TMUX_VERSION"
mv -f "$destination_tmp" "$BIN_DIR/tmux"
destination_tmp=""

PATH="$BIN_DIR:$PATH"
export PATH
selected_tmux="$(command -v tmux)"
[ "$selected_tmux" = "$BIN_DIR/tmux" ] || \
  die "selected tmux executable is $selected_tmux, expected $BIN_DIR/tmux"
installed_version="$("$selected_tmux" -V)"
[ "$installed_version" = "tmux $TMUX_VERSION" ] || die "selected tmux version verification failed"

printf 'Installed and verified %s at %s.\n' "$installed_version" "$selected_tmux"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) die "internal PATH verification failed" ;;
esac
printf 'Add %s to your shell PATH before running Control Agents.\n' "$BIN_DIR"
