package taskhook

// ManagedLaunchScript is compiled code prepended to the Codex exec in the
// existing fixed tmux command. Account/machine tools are host metadata probes,
// not browser input. Failure is optional registration failure, never task exit.
// Discovery execs the registrar as the exec shell's direct child. Its private
// process group can be stopped without signaling the shell or Codex.
const ManagedLaunchScript = `
unset PANEMUX_TASK_OWNER_GENERATION
pm_old_umask=$(umask); umask 077
if command -v perl >/dev/null 2>&1 && command -v sleep >/dev/null 2>&1; then
  pm_output=$(mktemp "${TMPDIR:-/tmp}/panemux-owner.XXXXXXXX" 2>/dev/null) || pm_output=
  if [ -n "$pm_output" ]; then
    perl -e 'setpgrp(0, 0) or exit 1; exec "/bin/sh", "-s", "--", @ARGV; exit 1' "$2" \
        >"$pm_output" 2>/dev/null <<'PANEMUX_OWNER_DISCOVERY' &
panemux_register_owner() {
  pm_uid=$(id -u 2>/dev/null) || return
  case "$pm_uid" in ''|*[!0-9]*) return;; esac
  pm_os=$(uname -s 2>/dev/null) || return
  case "$pm_os" in
  Linux)
    pm_entry=$(getent passwd "$pm_uid" 2>/dev/null) || return
    case "$pm_entry" in *'
'*) return;; esac
    IFS=: read -r pm_name pm_pw pm_dbuid pm_gid pm_gecos pm_home pm_shell <<EOF
$pm_entry
EOF
    [ "$pm_dbuid" = "$pm_uid" ] || return
    pm_machine=$(cat /etc/machine-id 2>/dev/null) || return
    case "$pm_machine" in ''|*[!0-9a-f]*) return;; esac
    [ "${#pm_machine}" = 32 ] || return
    pm_machine="linux-machine-id:$pm_machine"
    ;;
  Darwin)
    pm_entry=$(dscacheutil -q user -a uid "$pm_uid" 2>/dev/null) || return
    pm_dbuid=; pm_home=
    while IFS= read -r pm_line; do
      case "$pm_line" in
      'uid: '*) [ -z "$pm_dbuid" ] || return; pm_dbuid=${pm_line#uid: };;
      'dir: '*) [ -z "$pm_home" ] || return; pm_home=${pm_line#dir: };;
      esac
    done <<EOF
$pm_entry
EOF
    [ "$pm_dbuid" = "$pm_uid" ] || return
    pm_machine=$(ioreg -rd1 -c IOPlatformExpertDevice 2>/dev/null |
      sed -n 's/.*"IOPlatformUUID" = "\([0-9A-Fa-f-]*\)".*/\1/p' | tr A-F a-f) || return
    case "$pm_machine" in ''|*[!0-9a-f-]*) return;; esac
    [ "${#pm_machine}" = 36 ] || return
    pm_machine="darwin-platform-uuid:$pm_machine"
    ;;
  *) return;;
  esac
  case "$pm_home" in /*) ;; *) return;; esac
  pm_home=$(cd -- "$pm_home" 2>/dev/null && pwd -P) || return
  if [ -n "${CODEX_HOME:-}" ]; then
    pm_codex_home=$CODEX_HOME
  else
    pm_env_home=$(cd -- "${HOME:-/nonexistent}" 2>/dev/null && pwd -P) || return
    [ "$pm_env_home" = "$pm_home" ] || return
    pm_codex_home="$pm_home/.codex"
  fi
  case "$pm_codex_home" in /*) ;; *) return;; esac
  pm_codex_home=$(cd -- "$pm_codex_home" 2>/dev/null && pwd -P) || return
  pm_stat() {
    case "$pm_os" in
    Linux) stat -c '%u:%a:%h' -- "$1" 2>/dev/null;;
    Darwin) stat -f '%u:%Lp:%l' "$1" 2>/dev/null;;
    esac
  }
  pm_dir() {
    [ -d "$1" ] && [ ! -L "$1" ] || return 1
    pm_info=$(pm_stat "$1") || return 1
    pm_owner=${pm_info%%:*}; pm_mode=${pm_info#*:}; pm_mode=${pm_mode%%:*}
    [ "$pm_owner" = "$pm_uid" ] || return 1
    case "$pm_mode" in ''|*[!0-7]*) return 1;; esac
    [ $((0$pm_mode & 022)) = 0 ] || return 1
    [ "$2" != private ] || [ "$pm_mode" = 700 ]
  }
  pm_dir "$pm_home" ordinary && pm_dir "$pm_codex_home" ordinary &&
    pm_dir "$pm_codex_home/sessions" ordinary || return
  if [ "$pm_os" = Linux ]; then
    pm_hash=$(printf '%s\000' 'panemux.task-hooks.context/v1' "$pm_machine" "$pm_uid" "$pm_codex_home" |
      sha256sum 2>/dev/null) || return
  else
    pm_hash=$(printf '%s\000' 'panemux.task-hooks.context/v1' "$pm_machine" "$pm_uid" "$pm_codex_home" |
      shasum -a 256 2>/dev/null) || return
  fi
  pm_hash=${pm_hash%% *}
  case "$pm_hash" in ''|*[!0-9a-f]*) return;; esac
  [ "${#pm_hash}" = 64 ] || return
  pm_parent=$pm_home
  for pm_part in .local state panemux; do
    pm_parent="$pm_parent/$pm_part"; pm_dir "$pm_parent" ordinary || return
  done
  for pm_part in task-hooks contexts "ctx1-$pm_hash"; do
    pm_parent="$pm_parent/$pm_part"; pm_dir "$pm_parent" private || return
  done
  pm_context=$pm_parent; pm_root="$pm_context/state"; pm_helper="$pm_context/bin/panemux-task-hook"
  pm_dir "$pm_context/bin" private && pm_dir "$pm_root" private || return
  [ -f "$pm_helper" ] && [ ! -L "$pm_helper" ] && [ -x "$pm_helper" ] || return
  pm_info=$(pm_stat "$pm_helper") || return
  pm_owner=${pm_info%%:*}; pm_mode=${pm_info#*:}; pm_links=${pm_mode#*:}; pm_mode=${pm_mode%%:*}
  [ "$pm_owner" = "$pm_uid" ] && [ "$pm_links" = 1 ] || return
  case "$pm_mode" in ''|*[!0-7]*) return;; esac
  [ $((0$pm_mode & 07022)) = 0 ] && [ $((0$pm_mode & 0100)) != 0 ] || return
  pm_health=$("$pm_helper" selfcheck --root "$pm_root" </dev/null 2>/dev/null) || return
  [ "$pm_health" = '{"helper_version":"0.1.0","process_probe":"available","protocol_version":1}' ] || return
  exec "$pm_helper" register-managed-launch --root "$pm_root" --account-home "$pm_home" \
      --codex-home "$pm_codex_home" --machine-identity "$pm_machine" --uid "$pm_uid" \
      --executable "$1"
}
panemux_register_owner "$1"
PANEMUX_OWNER_DISCOVERY
    pm_pid=$!; pm_ticks=0
    while kill -0 "$pm_pid" 2>/dev/null && [ "$pm_ticks" -lt 20 ]; do
      sleep 0.05 || break
      pm_ticks=$((pm_ticks + 1))
    done
    if ! kill -0 "$pm_pid" 2>/dev/null; then
      if wait "$pm_pid"; then
        IFS= read -r pm_generation <"$pm_output" || :
        case "$pm_generation" in g1-*)
          pm_hex=${pm_generation#g1-}
          case "$pm_hex" in *[!0-9a-f]*) ;; *)
            if [ "${#pm_hex}" = 32 ]; then
              PANEMUX_TASK_OWNER_GENERATION=$pm_generation; export PANEMUX_TASK_OWNER_GENERATION
            fi;; esac;;
        esac
      fi
    else
      kill -KILL "-$pm_pid" 2>/dev/null || :
      kill -KILL "$pm_pid" 2>/dev/null || :
    fi
    rm -f -- "$pm_output"
  fi
fi
umask "$pm_old_umask"
`
