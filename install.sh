#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INSTALL_DIR=${INSTALL_DIR:-${CREW_INSTALL_DIR:-$HOME/.local/bin}}
GO_BIN=${GO_BIN:-go}
BIN_NAME=crew
CONFIG_HOME=${XDG_CONFIG_HOME:-$HOME/.config}
DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}
STATE_HOME=${XDG_STATE_HOME:-$HOME/.local/state}
CREW_CONFIG_DIR=${CREW_CONFIG_DIR:-$CONFIG_HOME/crew}
CREW_DATA_DIR=${CREW_DATA_DIR:-$DATA_HOME/crew}
CREW_STATE_DIR=${CREW_STATE_DIR:-$STATE_HOME/crew}
CREW_RUNTIME_BIN=$CREW_DATA_DIR/bin/$BIN_NAME
CREW_WRAPPER_BIN=$INSTALL_DIR/$BIN_NAME
CREW_CONFIG_PATH=$CREW_CONFIG_DIR/crew.yaml
CREW_AGENTS_DIR=$CREW_DATA_DIR/crew_agents
CREW_SANDBOX_ROOT=$CREW_STATE_DIR/sandboxes
CREW_DB_PATH=$CREW_STATE_DIR/crew.db
CREW_RUNTIME_STATE_PATH=$CREW_STATE_DIR/crew-runtime.json
PATH_MARKER_BEGIN="# >>> crew PATH >>>"
PATH_MARKER_END="# <<< crew PATH <<<"
PATH_SETUP_TARGET=${PATH_SETUP_TARGET:-auto}
PATH_SETUP_UPDATED=0
PATH_SETUP_FILE=""
PATH_SETUP_RELOAD_CMD=""

if ! command -v "$GO_BIN" >/dev/null 2>&1; then
	echo "missing Go toolchain: expected '$GO_BIN' on PATH" >&2
	exit 1
fi

yaml_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

shell_escape() {
	printf '%s' "$1" | sed 's/[\\"]/\\&/g; s/\$/\\$/g; s/`/\\`/g'
}

install_dir_on_path() {
	case ":$PATH:" in
		*":$INSTALL_DIR:"*) return 0 ;;
		*) return 1 ;;
	esac
}

append_sh_path_block() {
	rc_file=$1
	escaped_install_dir=$(shell_escape "$INSTALL_DIR")

	mkdir -p "$(dirname "$rc_file")"
	if [ -f "$rc_file" ] && grep -F "$PATH_MARKER_BEGIN" "$rc_file" >/dev/null 2>&1; then
		printf 'kept existing managed PATH setup in %s\n' "$rc_file"
		PATH_SETUP_FILE=$rc_file
		PATH_SETUP_RELOAD_CMD=". $rc_file"
		return
	fi
	if [ -f "$rc_file" ] && grep -F "$INSTALL_DIR" "$rc_file" >/dev/null 2>&1; then
		printf 'kept existing PATH entry in %s\n' "$rc_file"
		PATH_SETUP_FILE=$rc_file
		PATH_SETUP_RELOAD_CMD=". $rc_file"
		return
	fi

	umask 077
	cat >>"$rc_file" <<EOF

$PATH_MARKER_BEGIN
case ":\$PATH:" in
  *:"$escaped_install_dir":*) ;;
  *) export PATH="$escaped_install_dir:\$PATH" ;;
esac
$PATH_MARKER_END
EOF
	printf 'updated PATH setup in %s\n' "$rc_file"
	PATH_SETUP_UPDATED=1
	PATH_SETUP_FILE=$rc_file
	PATH_SETUP_RELOAD_CMD=". $rc_file"
}

append_fish_path_block() {
	rc_file=$1
	escaped_install_dir=$(shell_escape "$INSTALL_DIR")

	mkdir -p "$(dirname "$rc_file")"
	if [ -f "$rc_file" ] && grep -F "$PATH_MARKER_BEGIN" "$rc_file" >/dev/null 2>&1; then
		printf 'kept existing managed PATH setup in %s\n' "$rc_file"
		PATH_SETUP_FILE=$rc_file
		PATH_SETUP_RELOAD_CMD="source $rc_file"
		return
	fi
	if [ -f "$rc_file" ] && grep -F "$INSTALL_DIR" "$rc_file" >/dev/null 2>&1; then
		printf 'kept existing PATH entry in %s\n' "$rc_file"
		PATH_SETUP_FILE=$rc_file
		PATH_SETUP_RELOAD_CMD="source $rc_file"
		return
	fi

	umask 077
	cat >>"$rc_file" <<EOF

$PATH_MARKER_BEGIN
if not contains "$escaped_install_dir" \$PATH
    set -gx PATH "$escaped_install_dir" \$PATH
end
$PATH_MARKER_END
EOF
	printf 'updated PATH setup in %s\n' "$rc_file"
	PATH_SETUP_UPDATED=1
	PATH_SETUP_FILE=$rc_file
	PATH_SETUP_RELOAD_CMD="source $rc_file"
}

configure_path_setup() {
	if install_dir_on_path; then
		return
	fi

	case "$PATH_SETUP_TARGET" in
		none)
			return
			;;
		zsh)
			append_sh_path_block "$HOME/.zprofile"
			append_sh_path_block "$HOME/.zshrc"
			PATH_SETUP_RELOAD_CMD="exec zsh"
			return
			;;
		zshrc)
			append_sh_path_block "$HOME/.zshrc"
			return
			;;
		zprofile)
			append_sh_path_block "$HOME/.zprofile"
			return
			;;
		bash)
			append_sh_path_block "$HOME/.bash_profile"
			append_sh_path_block "$HOME/.bashrc"
			PATH_SETUP_RELOAD_CMD="exec bash"
			return
			;;
		bashrc)
			append_sh_path_block "$HOME/.bashrc"
			return
			;;
		bash_profile)
			append_sh_path_block "$HOME/.bash_profile"
			return
			;;
		profile)
			append_sh_path_block "$HOME/.profile"
			return
			;;
		fish)
			append_fish_path_block "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
			return
			;;
		auto)
			;;
		*)
			printf 'warning: unsupported PATH_SETUP_TARGET=%s; skipping shell profile changes\n' "$PATH_SETUP_TARGET" >&2
			return
			;;
	esac

	shell_name=$(basename "${SHELL:-}")
	case "$shell_name" in
		zsh)
			append_sh_path_block "$HOME/.zprofile"
			append_sh_path_block "$HOME/.zshrc"
			PATH_SETUP_RELOAD_CMD="exec zsh"
			;;
		bash)
			append_sh_path_block "$HOME/.bash_profile"
			append_sh_path_block "$HOME/.bashrc"
			PATH_SETUP_RELOAD_CMD="exec bash"
			;;
		fish)
			append_fish_path_block "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
			;;
		sh|dash|ash|ksh)
			append_sh_path_block "$HOME/.profile"
			;;
		*)
			if [ -f "$HOME/.zshrc" ]; then
				append_sh_path_block "$HOME/.zshrc"
			elif [ -f "$HOME/.bash_profile" ]; then
				append_sh_path_block "$HOME/.bash_profile"
			elif [ -f "$HOME/.bashrc" ]; then
				append_sh_path_block "$HOME/.bashrc"
			elif [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish" ]; then
				append_fish_path_block "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
			else
				append_sh_path_block "$HOME/.profile"
			fi
			;;
	esac
}

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/crew-install.XXXXXX")
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

mkdir -p "$INSTALL_DIR" \
	"$CREW_CONFIG_DIR" \
	"$CREW_DATA_DIR/bin" \
	"$CREW_STATE_DIR" \
	"$CREW_SANDBOX_ROOT"

cd "$ROOT_DIR"
"$GO_BIN" build -o "$TMP_DIR/$BIN_NAME" ./cmd/crew
install -m 0755 "$TMP_DIR/$BIN_NAME" "$CREW_RUNTIME_BIN"

if [ ! -e "$CREW_AGENTS_DIR/AGENTS.MD" ]; then
	mkdir -p "$CREW_AGENTS_DIR"
	cp -R "$ROOT_DIR/crew_agents/." "$CREW_AGENTS_DIR"
	printf 'seeded agent catalog at %s\n' "$CREW_AGENTS_DIR"
else
	rm -rf "$CREW_AGENTS_DIR"
	mkdir -p "$CREW_AGENTS_DIR"
	cp -R "$ROOT_DIR/crew_agents/." "$CREW_AGENTS_DIR"
	printf 'refreshed agent catalog at %s\n' "$CREW_AGENTS_DIR"
fi

if [ ! -f "$CREW_CONFIG_PATH" ]; then
	CREW_DB_PATH_YAML=$(yaml_escape "$CREW_DB_PATH")
	CREW_SANDBOX_ROOT_YAML=$(yaml_escape "$CREW_SANDBOX_ROOT")
	CREW_RUNTIME_STATE_PATH_YAML=$(yaml_escape "$CREW_RUNTIME_STATE_PATH")
	umask 077
	cat >"$CREW_CONFIG_PATH" <<EOF
# crew home config written by install.sh
# Edit this file for local provider credentials and runtime settings.

storage:
  path: "$CREW_DB_PATH_YAML"

providers:
  codex:
    binary: codex
    working_directory: .
    timeout_millis: 30000

sandbox:
  default_provider: disabled
  source_workspace_root: .
  permission_profile: patch
  providers:
    codex:
      binary: codex
      model: ""
      workspace_root: "$CREW_SANDBOX_ROOT_YAML"
      timeout_millis: 300000
      additional_write: []

runtime:
  state_path: "$CREW_RUNTIME_STATE_PATH_YAML"
EOF
	printf 'wrote default config to %s\n' "$CREW_CONFIG_PATH"
else
	printf 'kept existing config at %s\n' "$CREW_CONFIG_PATH"
fi

cat >"$TMP_DIR/$BIN_NAME.wrapper" <<EOF
#!/usr/bin/env sh
set -eu

DEFAULT_CONFIG_PATH="$CREW_CONFIG_PATH"
CREW_BIN="$CREW_RUNTIME_BIN"

CONFIG_PATH=\${CREW_CONFIG_PATH:-\$DEFAULT_CONFIG_PATH}
exec "\$CREW_BIN" --config "\$CONFIG_PATH" "\$@"
EOF
install -m 0755 "$TMP_DIR/$BIN_NAME.wrapper" "$CREW_WRAPPER_BIN"

CREW_AGENTS_DIR=$CREW_AGENTS_DIR "$CREW_WRAPPER_BIN" config show >/dev/null
CREW_AGENTS_DIR=$CREW_AGENTS_DIR "$CREW_WRAPPER_BIN" agents validate >/dev/null
configure_path_setup

printf 'installed wrapper to %s\n' "$CREW_WRAPPER_BIN"
printf 'installed runtime binary to %s\n' "$CREW_RUNTIME_BIN"
printf 'config path: %s\n' "$CREW_CONFIG_PATH"
printf 'agent catalog: %s\n' "$CREW_AGENTS_DIR"
printf 'state directory: %s\n' "$CREW_STATE_DIR"

if ! command -v codex >/dev/null 2>&1; then
	printf 'warning: codex CLI not found on PATH; sandbox tasks and provider=codex agents will not run until it is installed\n' >&2
fi

if [ -z "${XAI_API_KEY:-}" ]; then
	printf 'warning: bundled Grok-backed agents require XAI_API_KEY for live text generation\n' >&2
fi

if install_dir_on_path; then
	printf '%s is already on PATH in the current shell\n' "$INSTALL_DIR"
elif [ "$PATH_SETUP_UPDATED" -eq 1 ] || [ -n "$PATH_SETUP_FILE" ]; then
	printf '%s is not on PATH in the current shell yet\n' "$INSTALL_DIR"
	printf 'reload your shell config with: %s\n' "$PATH_SETUP_RELOAD_CMD"
else
	printf '%s is not on PATH and no shell profile was updated\n' "$INSTALL_DIR" >&2
	printf 'set PATH_SETUP_TARGET=zshrc|bashrc|bash_profile|profile|fish to force a target, or add %s manually\n' "$INSTALL_DIR" >&2
fi
