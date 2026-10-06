#!/bin/sh

is_alpine() {
  [ -f /etc/alpine-release ]
}

is_openwrt() {
  grep -qi "OpenWrt" /etc/os-release
}

is_freebsd() {
  [ "$(uname -s)" = "FreeBSD" ]
}

is_opnsense() {
  [ -f /usr/local/sbin/opnsense-version ] || [ -f /usr/local/etc/opnsense-version ] || [ -f /etc/opnsense-release ]
}

# If SELinux is enabled, set the context of the binary
set_selinux_context() {
  # Check if SELinux is enabled and in enforcing or permissive mode
  if command -v getenforce >/dev/null 2>&1; then
    SELINUX_MODE=$(getenforce)
    if [ "$SELINUX_MODE" != "Disabled" ]; then
      echo "SELinux is enabled (${SELINUX_MODE} mode). Setting appropriate context..."

      # First try to set persistent context if semanage is available
      if command -v semanage >/dev/null 2>&1; then
        echo "Attempting to set persistent SELinux context..."
        if semanage fcontext -a -t bin_t "$BIN_PATH" >/dev/null 2>&1; then
          restorecon -v "$BIN_PATH" >/dev/null 2>&1
        else
          echo "Warning: Failed to set persistent context, falling back to temporary context."
        fi
      fi

      # Fall back to chcon if semanage failed or isn't available
      if command -v chcon >/dev/null 2>&1; then
        # Set context for both the directory and binary
        chcon -t bin_t "$BIN_PATH" || echo "Warning: Failed to set SELinux context for binary."
        chcon -R -t bin_t "$AGENT_DIR" || echo "Warning: Failed to set SELinux context for directory."
      else
        if [ "$SELINUX_MODE" = "Enforcing" ]; then
          echo "Warning: SELinux is in enforcing mode but chcon command not found. The service may fail to start."
          echo "Consider installing the policycoreutils package or temporarily setting SELinux to permissive mode."
        else
          echo "Warning: SELinux is in permissive mode but chcon command not found."
        fi
      fi
    fi
  fi
}

# Clean up SELinux contexts if they were set
cleanup_selinux_context() {
  if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" != "Disabled" ]; then
    echo "Cleaning up SELinux contexts..."
    # Remove persistent context if semanage is available
    if command -v semanage >/dev/null 2>&1; then
      semanage fcontext -d "$BIN_PATH" 2>/dev/null || true
    fi
  fi
}

# Ensure the proxy URL ends with a /
ensure_trailing_slash() {
  if [ -n "$1" ]; then
    case "$1" in
    */) echo "$1" ;;
    *) echo "$1/" ;;
    esac
  else
    echo "$1"
  fi
}

print_supported_targets() {
  echo "Supported release targets: linux/amd64, linux/arm64, linux/arm (armv7)."
}

print_prerelease_hint() {
  echo "If you want to install a beta or other pre-release, pass it explicitly with --version or -v."
  echo "Example: ./install-agent.sh --version vX.Y.Z-beta ..."
}

require_supported_release_target() {
  if [ "$1" != "linux" ]; then
    echo "Error: install-agent.sh currently supports only Linux release targets."
    print_supported_targets
    exit 1
  fi

  case "$2" in
    amd64|arm64|arm)
      ;;
    *)
      echo "Error: Unsupported architecture '$2' for Vigil Agent release artifacts."
      print_supported_targets
      exit 1
      ;;
  esac
}

warn_auto_update_unavailable() {
  echo "Warning: automatic updates are not available for Vigil Agent yet. Skipping auto-update setup."
}

# Succeeds when user $1 is a member of group $2.
user_in_group() {
  id -nG "$1" 2>/dev/null | tr ' ' '\n' | grep -qx "$2"
}

group_exists() {
  if is_openwrt; then
    grep -q "^$1:" /etc/group 2>/dev/null
  else
    getent group "$1" >/dev/null 2>&1
  fi
}

add_user_to_group() {
  if is_alpine; then
    addgroup "$1" "$2"
  elif is_openwrt; then
    # No usermod on OpenWrt: append the user to the group's member list in /etc/group.
    if grep -q "^$2:[^:]*:[^:]*:..*$" /etc/group; then
      sed -i "s/^\($2:[^:]*:[^:]*:.*\)$/\1,$1/" /etc/group
    else
      sed -i "s/^\($2:[^:]*:[^:]*:\)$/\1$1/" /etc/group
    fi
  elif is_freebsd; then
    pw group mod "$2" -m "$1"
  else
    usermod -aG "$2" "$1"
  fi
}

remove_user_from_group() {
  if is_alpine; then
    delgroup "$1" "$2"
  elif is_openwrt; then
    sed -i -e "/^$2:/s/:$1\$/:/" -e "/^$2:/s/:$1,/:/" -e "/^$2:/s/,$1\$//" -e "/^$2:/s/,$1,/,/" /etc/group
  elif is_freebsd; then
    pw group mod "$2" -d "$1"
  else
    gpasswd -d "$1" "$2" >/dev/null
  fi
}

# Docker socket access is opt-in: docker group membership is equivalent to root on the
# host and bypasses the service sandboxing. --docker grants it, --no-docker revokes it,
# and without either flag the current membership is left as is.
apply_docker_access() {
  if [ "$DOCKER_ACCESS" = "true" ]; then
    if ! group_exists docker; then
      echo "Docker access requested but no 'docker' group exists on this host; skipping."
    elif user_in_group "$AGENT_USER" docker; then
      echo "Docker socket access already granted to $AGENT_USER."
    else
      echo "Adding $AGENT_USER to the docker group (Docker socket access, equivalent to root on this host)..."
      add_user_to_group "$AGENT_USER" docker
    fi
  elif [ "$DOCKER_ACCESS" = "false" ]; then
    if user_in_group "$AGENT_USER" docker; then
      echo "Removing $AGENT_USER from the docker group..."
      remove_user_from_group "$AGENT_USER" docker
    fi
  elif user_in_group "$AGENT_USER" docker; then
    echo "Keeping Docker socket access for $AGENT_USER (pass --no-docker to revoke it)."
  elif group_exists docker; then
    echo "Docker container inventory is disabled: the agent has no access to the Docker socket."
    echo "To enable it, re-run this script with --docker (grants root-equivalent docker group membership)."
  fi
}

# Installs made before the dedicated vigil-agent user ran as a generic "app" account; the
# service definition tells them apart from an unrelated "app" user.
legacy_service_user_in_use() {
  if is_alpine || is_openwrt; then
    [ -f /etc/init.d/vigil-agent ] && grep -Eq "command_user=\"$LEGACY_AGENT_USER\"|procd_set_param user $LEGACY_AGENT_USER\$" /etc/init.d/vigil-agent
  elif is_freebsd; then
    [ "$(sysrc -n vigil_agent_user 2>/dev/null)" = "$LEGACY_AGENT_USER" ]
  else
    [ -f /etc/systemd/system/vigil-agent.service ] && grep -qx "User=$LEGACY_AGENT_USER" /etc/systemd/system/vigil-agent.service
  fi
}

print_legacy_user_notice() {
  if id -u "$LEGACY_AGENT_USER" >/dev/null 2>&1; then
    echo ""
    echo "Note: the Vigil Agent no longer uses the '$LEGACY_AGENT_USER' account, which was left untouched"
    echo "because it may belong to something else (groups: $(id -nG "$LEGACY_AGENT_USER" 2>/dev/null))."
    if is_alpine; then
      _remove_cmd="deluser $LEGACY_AGENT_USER"
    elif is_openwrt; then
      _remove_cmd="sed -i '/^$LEGACY_AGENT_USER:/d' /etc/passwd /etc/group /etc/shadow"
    else
      _remove_cmd="userdel $LEGACY_AGENT_USER"
    fi
    echo "If an earlier Vigil Agent install created it and nothing else uses it, remove it with: $_remove_cmd"
  fi
}

# Generate FreeBSD rc service content
generate_freebsd_rc_service() {
  cat <<'EOF'
#!/bin/sh

# PROVIDE: vigil_agent
# REQUIRE: DAEMON NETWORKING
# BEFORE: LOGIN
# KEYWORD: shutdown

# Add the following lines to /etc/rc.conf to configure Vigil Agent:
#
# vigil_agent_enable (bool):   Set to YES to enable Vigil Agent
#                               Default: YES
# vigil_agent_env_file (str):  Vigil Agent env configuration file
#                               Default: /usr/local/etc/vigil-agent/env
# vigil_agent_user (str):      Vigil Agent daemon user
#                               Default: vigil-agent
# vigil_agent_bin (str):       Path to the vigil-agent binary
#                               Default: /usr/local/sbin/vigil-agent
# vigil_agent_flags (str):     Extra flags passed to vigil-agent command invocation
#                               Default:

. /etc/rc.subr

name="vigil_agent"
rcvar=vigil_agent_enable

load_rc_config $name
: ${vigil_agent_enable:="YES"}
: ${vigil_agent_user:="vigil-agent"}
: ${vigil_agent_flags:=""}
: ${vigil_agent_env_file:="/usr/local/etc/vigil-agent/env"}
: ${vigil_agent_bin:="/usr/local/sbin/vigil-agent"}

logfile="/var/log/${name}.log"
pidfile="/var/run/${name}.pid"

procname="/usr/sbin/daemon"
start_precmd="${name}_prestart"
start_cmd="${name}_start"
stop_cmd="${name}_stop"

vigil_agent_prestart()
{
    if [ ! -f "${vigil_agent_env_file}" ]; then
        echo WARNING: missing "${vigil_agent_env_file}" env file. Start aborted.
        exit 1
    fi
}

vigil_agent_start()
{
    echo "Starting ${name}"
    /usr/sbin/daemon -fc \
            -P "${pidfile}" \
            -o "${logfile}" \
            -u "${vigil_agent_user}" \
            "${vigil_agent_bin}" ${vigil_agent_flags}
}

vigil_agent_stop()
{
    pid="$(check_pidfile "${pidfile}" "${procname}")"
    if [ -n "${pid}" ]; then
        echo "Stopping ${name} (pid=${pid})"
        kill -- "-${pid}"
        wait_for_pids "${pid}"
    else
        echo "${name} isn't running"
    fi
}

run_rc_command "$1"
EOF
}

# Detect system architecture
detect_architecture() {
  local arch=$(uname -m)

  if [ "$arch" = "mips" ]; then
    detect_mips_endianness
    return $?
  fi

  case "$arch" in
    x86_64)
      arch="amd64"
      ;;
    armv6l|armv7l|armv8l)
      arch="arm"
      ;;
    aarch64)
      arch="arm64"
      ;;
  esac

  echo "$arch"
}

# Detect MIPS endianness using ELF header
detect_mips_endianness() {
  local bins="/bin/sh /bin/ls /usr/bin/env"
  local bin_to_check endian
  
  for bin_to_check in $bins; do
    if [ -f "$bin_to_check" ]; then
      # The 6th byte in ELF header: 01 = little, 02 = big
      endian=$(hexdump -n 1 -s 5 -e '1/1 "%02x"' "$bin_to_check" 2>/dev/null)
      if [ "$endian" = "01" ]; then
        echo "mipsle"
        return
      elif [ "$endian" = "02" ]; then
        echo "mips" 
        return
      fi
    fi
  done
  
  # Final fallback
  echo "mips"
}

# Default values
UNINSTALL=false
GITHUB_URL="https://github.com"
GITHUB_PROXY_URL=""
INSECURE_MIRROR=false
KEY=""
TOKEN=""
HUB_URL=""
AUTO_UPDATE_FLAG="" # empty string means unused, "true" warns and skips, "false" means skip
DOCKER_ACCESS="" # "true" grants docker group membership, "false" revokes it, empty keeps the current state
VERSION="latest"

# Check for help flag
case "$1" in
-h | --help)
  printf "Vigil Agent installation script\n\n"
  printf "Usage: ./install-agent.sh [options]\n\n"
  printf "Options: \n"
  printf "  -k                    : SSH key (required, or interactive if not provided)\n"
  printf "  -t                    : Token (optional for backwards compatibility)\n"
  printf "  -url                  : Hub URL (optional for backwards compatibility)\n"
  printf "  -v, --version         : Version to install (default: latest)\n"
  printf "  -u                    : Uninstall Vigil Agent\n"
  printf "  --docker              : Grant the agent Docker socket access (docker group) to inventory\n"
  printf "                          containers. Equivalent to root on the host: only use it if you\n"
  printf "                          want container monitoring. Kept on upgrades once granted.\n"
  printf "  --no-docker           : Revoke Docker socket access granted earlier\n"
  printf "  --auto-update [VALUE] : Reserved for future use (currently ignored)\n"
  printf "                          VALUE can be true or false; the flag is accepted for compatibility.\n"
  printf "  --mirror [URL]        : Use GitHub proxy to resolve network timeout issues in mainland China\n"
  printf "                          URL: optional custom proxy URL (default: https://gh.github.com)\n"
  printf "  --insecure-mirror     : With --mirror, allow the checksum to come from the mirror when\n"
  printf "                          github.com is unreachable. Reduces integrity (the mirror then\n"
  printf "                          provides both the binary and its checksum). Use only if you trust\n"
  printf "                          the mirror and github.com is fully blocked.\n"
  print_supported_targets
  printf "  -h, --help            : Display this help message\n"
  exit 0
  ;;
esac

# Check if running as root and re-execute with sudo if needed
if [ "$(id -u)" != "0" ]; then
  if command -v sudo >/dev/null 2>&1; then
    # Re-exec under sudo. "$@" is still the original, unparsed argument list here, so the
    # shell preserves each argument's quoting exactly — no eval, no word-splitting, and no
    # risk from a script path ($0) that contains spaces or shell metacharacters.
    exec sudo "$0" "$@"
  else
    echo "This script must be run as root. Please either:"
    echo "1. Run this script as root (su root)"
    echo "2. Install sudo and run with sudo"
    exit 1
  fi
fi

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
  -k)
    shift
    KEY="$1"
    ;;
  -t)
    shift
    TOKEN="$1"
    ;;
  -url)
    shift
    HUB_URL="$1"
    ;;
  -v | --version)
    shift
    VERSION="$1"
    ;;
  -u)
    UNINSTALL=true
    ;;
  --docker)
    DOCKER_ACCESS=true
    ;;
  --no-docker)
    DOCKER_ACCESS=false
    ;;
  --mirror* | --china-mirrors*)
    # Check if there's a value after the = sign
    if echo "$1" | grep -q "="; then
      # Extract the value after =
      CUSTOM_PROXY=$(echo "$1" | cut -d'=' -f2)
      if [ -n "$CUSTOM_PROXY" ]; then
        GITHUB_PROXY_URL="$CUSTOM_PROXY"
        GITHUB_URL="$(ensure_trailing_slash "$CUSTOM_PROXY")https://github.com"
      else
        GITHUB_PROXY_URL="https://gh.github.com"
        GITHUB_URL="$GITHUB_PROXY_URL"
      fi
    elif [ "$2" != "" ] && ! echo "$2" | grep -q '^-'; then
      # use custom proxy URL provided as next argument
      GITHUB_PROXY_URL="$2"
      GITHUB_URL="$(ensure_trailing_slash "$2")https://github.com"
      shift
    else
      # No value specified, use default
      GITHUB_PROXY_URL="https://gh.github.com"
      GITHUB_URL="$GITHUB_PROXY_URL"
    fi
    ;;
  --insecure-mirror)
    INSECURE_MIRROR=true
    ;;
  --auto-update*)
    # Check if there's a value after the = sign
    if echo "$1" | grep -q "="; then
      # Extract the value after =
      AUTO_UPDATE_VALUE=$(echo "$1" | cut -d'=' -f2)
      if [ "$AUTO_UPDATE_VALUE" = "true" ]; then
        AUTO_UPDATE_FLAG="true"
      elif [ "$AUTO_UPDATE_VALUE" = "false" ]; then
        AUTO_UPDATE_FLAG="false"
      else
        echo "Invalid value for --auto-update flag: $AUTO_UPDATE_VALUE. Ignoring the flag."
      fi
    elif [ "$2" = "true" ] || [ "$2" = "false" ]; then
      # Value provided as next argument
      AUTO_UPDATE_FLAG="$2"
      shift
    else
      # No value specified, use true
      AUTO_UPDATE_FLAG="true"
    fi
    ;;
  *)
    echo "Invalid option: $1" >&2
    exit 1
    ;;
  esac
  shift
done

# Set paths based on operating system
if is_freebsd; then
  AGENT_DIR="/usr/local/etc/vigil-agent"
  BIN_DIR="/usr/local/sbin"
  BIN_PATH="/usr/local/sbin/vigil-agent"
else
  AGENT_DIR="/opt/vigil-agent"
  BIN_DIR="/opt/vigil-agent"
  BIN_PATH="/opt/vigil-agent/vigil-agent"
fi

# The agent runs as a dedicated, unprivileged system user. Not "vigil": that is the native
# hub's user (install-hub.sh), and an agent on the hub's host must not own the hub's data.
# Installs made before this change used a generic "app" user (LEGACY_AGENT_USER), which
# upgrades migrate away from without deleting it (the name is common, it may belong to
# something else).
AGENT_USER="vigil-agent"
LEGACY_AGENT_USER="app"
AGENT_STATE_DIR="/var/lib/vigil-agent"

# Stop existing service if it exists (for upgrades)
if [ "$UNINSTALL" != true ] && [ -f "$BIN_PATH" ]; then
  echo "Existing installation detected. Stopping service for upgrade..."
  if is_alpine; then
    rc-service vigil-agent stop 2>/dev/null || true
  elif is_openwrt; then
    /etc/init.d/vigil-agent stop 2>/dev/null || true
  elif is_freebsd; then
    service vigil-agent stop 2>/dev/null || true
  else
    systemctl stop vigil-agent.service 2>/dev/null || true
  fi
fi

# Detect an install that still runs as the legacy "app" user, before the uninstall or the
# upgrade rewrites its service definition (see AGENT_USER above).
LEGACY_INSTALL=false
if legacy_service_user_in_use; then
  LEGACY_INSTALL=true
fi

# Uninstall process
if [ "$UNINSTALL" = true ]; then
  # Clean up SELinux contexts before removing files
  cleanup_selinux_context

  if is_alpine; then
    echo "Stopping and disabling the agent service..."
    rc-service vigil-agent stop
    rc-update del vigil-agent default

    echo "Removing the OpenRC service files..."
    rm -f /etc/init.d/vigil-agent

    # Remove the daily update cron job if it exists
    echo "Removing the daily update cron job..."
    if crontab -u root -l 2>/dev/null | grep -q "vigil-agent.*update"; then
      crontab -u root -l 2>/dev/null | grep -v "vigil-agent.*update" | crontab -u root -
    fi

    # Remove log files
    echo "Removing log files..."
    rm -f /var/log/vigil-agent.log /var/log/vigil-agent.err
  elif is_openwrt; then
    echo "Stopping and disabling the agent service..."
    /etc/init.d/vigil-agent stop
    /etc/init.d/vigil-agent disable

    echo "Removing the OpenWRT service files..."
    rm -f /etc/init.d/vigil-agent

    # Remove the update service if it exists
    echo "Removing the daily update service..."
    # Remove legacy app account based crontab file
    rm -f /etc/crontabs/app
    # Install root crontab job
    if crontab -u root -l 2>/dev/null | grep -q "vigil-agent.*update"; then
      crontab -u root -l 2>/dev/null | grep -v "vigil-agent.*update" | crontab -u root -
    fi

  elif is_freebsd; then
    echo "Stopping and disabling the agent service..."
    service vigil-agent stop
    sysrc vigil_agent_enable="NO"

    echo "Removing the FreeBSD service files..."
    rm -f /usr/local/etc/rc.d/vigil-agent

    # Remove the daily update cron job if it exists
    echo "Removing the daily update cron job..."
    rm -f /etc/cron.d/vigil-agent

    # Remove log files
    echo "Removing log files..."
    rm -f /var/log/vigil-agent.log

    # Remove env file and directories
    echo "Removing environment configuration file..."
    rm -f "$AGENT_DIR/env"
    rm -f "$BIN_PATH"
    rmdir "$AGENT_DIR" 2>/dev/null || true

  else
    echo "Stopping and disabling the agent service..."
    systemctl stop vigil-agent.service
    systemctl disable vigil-agent.service >/dev/null 2>&1

    echo "Removing the systemd service file..."
    rm -f /etc/systemd/system/vigil-agent.service

    # Remove the update timer and service if they exist
    echo "Removing the daily update service and timer..."
    systemctl stop vigil-agent-update.timer 2>/dev/null
    systemctl disable vigil-agent-update.timer >/dev/null 2>&1
    rm -f /etc/systemd/system/vigil-agent-update.service
    rm -f /etc/systemd/system/vigil-agent-update.timer

    systemctl daemon-reload
  fi

  echo "Removing the Vigil Agent directory..."
  if [ -n "$AGENT_DIR" ] && [ "$AGENT_DIR" != "/" ]; then
    rm -rf "$AGENT_DIR"
  fi

  echo "Removing the dedicated user for the agent service..."
  killall vigil-agent 2>/dev/null
  if is_openwrt; then
    # No deluser on OpenWrt: drop the group membership and the account lines directly.
    remove_user_from_group "$AGENT_USER" docker
    sed -i "/^$AGENT_USER:/d" /etc/passwd /etc/group
    [ -f /etc/shadow ] && sed -i "/^$AGENT_USER:/d" /etc/shadow
  elif is_alpine; then
    deluser "$AGENT_USER" 2>/dev/null
  elif is_freebsd; then
    pw user del "$AGENT_USER" 2>/dev/null
  else
    userdel "$AGENT_USER" 2>/dev/null
  fi
  if [ "$LEGACY_INSTALL" = true ]; then
    print_legacy_user_notice
  fi
  # The state dir (/var/lib/vigil-agent, holding the fingerprint) is kept on purpose, so a
  # reinstall comes back as the same host on the hub.

  echo "Vigil Agent has been uninstalled successfully!"
  exit 0
fi

TARGET_OS=$(uname -s | sed -e 'y/ABCDEFGHIJKLMNOPQRSTUVWXYZ/abcdefghijklmnopqrstuvwxyz/')
TARGET_ARCH=$(detect_architecture)
require_supported_release_target "$TARGET_OS" "$TARGET_ARCH"

# Check if a package is installed
package_installed() {
  command -v "$1" >/dev/null 2>&1
}

# Check for package manager and install necessary packages if not installed
if package_installed apk; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    apk update
    apk add tar curl coreutils shadow
  fi
elif package_installed opkg; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    opkg update
    opkg install tar curl coreutils
  fi
elif package_installed pkg && is_freebsd; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    pkg update
    pkg install -y gtar curl coreutils
  fi
elif package_installed apt-get; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    apt-get update
    apt-get install -y tar curl coreutils
  fi
elif package_installed yum; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    yum install -y tar curl coreutils
  fi
elif package_installed pacman; then
  if ! package_installed tar || ! package_installed curl || ! package_installed sha256sum; then
    pacman -Sy --noconfirm tar curl coreutils
  fi
else
  echo "Warning: Please ensure 'tar' and 'curl' and 'sha256sum (coreutils)' are installed."
fi

# If no SSH key is provided, ask for the SSH key interactively (skip if upgrading)
if [ -z "$KEY" ]; then
  if [ -f "$BIN_PATH" ]; then
    echo "Upgrading existing installation. Using existing service configuration."
  else
    printf "Enter your SSH key: "
    read KEY
    if [ -z "$KEY" ]; then
      echo "Error: the hub public key is required (copy it from the hub's Add agent dialog, or pass it with -k)."
      exit 1
    fi
  fi
fi

# --- secret / env handling ------------------------------------------------
# Strip CR/LF so KEY/TOKEN/HUB_URL cannot break or inject into the generated
# service definitions and env files.
KEY=$(printf '%s' "$KEY" | tr -d '\r\n')
TOKEN=$(printf '%s' "$TOKEN" | tr -d '\r\n')
HUB_URL=$(printf '%s' "$HUB_URL" | tr -d '\r\n')

# Single-quote-escape a value so it is safe inside a shell-sourced env file.
sq() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

# Create a root-only env file that is safe to `.`-source from an init script
# (used by the OpenRC, procd and FreeBSD service paths).
write_shell_env_file() {
  _ef="$1"
  (
    umask 077
    {
      printf 'KEY=%s\n' "$(sq "$KEY")"
      printf 'TOKEN=%s\n' "$(sq "$TOKEN")"
      printf 'HUB_URL=%s\n' "$(sq "$HUB_URL")"
    } >"$_ef"
  )
  chmod 600 "$_ef" 2>/dev/null || true
  chown root:root "$_ef" 2>/dev/null || true
}

# Upsert a KEY=value line into a systemd EnvironmentFile, only when the value is
# non-empty (so upgrades that omit -t/-k/-url preserve the existing secret).
# systemd never runs a shell on these values, and avoiding sed keeps |, & and \
# safe.
upsert_systemd_env() {
  _ef="$1"
  _k="$2"
  _v="$3"
  [ -z "$_v" ] && return 0
  (
    umask 077
    touch "$_ef"
  )
  _tmp="${_ef}.tmp.$$"
  grep -v "^${_k}=" "$_ef" >"$_tmp" 2>/dev/null || true
  printf '%s=%s\n' "$_k" "$_v" >>"$_tmp"
  mv "$_tmp" "$_ef"
  chmod 600 "$_ef" 2>/dev/null || true
  chown root:root "$_ef" 2>/dev/null || true
}

# TOKEN and HUB_URL are optional for backwards compatibility - no interactive prompts
# They will be set as empty environment variables if not provided

# Verify checksum
if command -v sha256sum >/dev/null; then
  CHECK_CMD="sha256sum"
elif command -v sha256 >/dev/null; then
  # FreeBSD uses 'sha256' instead of 'sha256sum', with different output format
  CHECK_CMD="sha256 -q"
else
  echo "No SHA256 checksum utility found"
  exit 1
fi

# Create the directory for the Vigil Agent

# The install dir holds the binary and the root-only env file: owned by root so the
# service user can neither replace the binary nor read the secrets.
if [ ! -d "$AGENT_DIR" ]; then
  echo "Creating the directory for the Vigil Agent..."
  mkdir -p "$AGENT_DIR"
fi
chown 0:0 "$AGENT_DIR"
chmod 755 "$AGENT_DIR"

if [ ! -d "$BIN_DIR" ]; then
  mkdir -p "$BIN_DIR"
fi

# Download and install the Vigil Agent

FILE_NAME="vigil-agent_${TARGET_OS}_${TARGET_ARCH}.tar.gz"

# Determine version to install
if [ "$VERSION" = "latest" ]; then
  API_RELEASE_URL="https://api.github.com/repos/Gu1llaum-3/vigil/releases/latest"
  INSTALL_VERSION=$(curl -fsSL "$API_RELEASE_URL" | grep -o '"tag_name": "v[^"]*"' | cut -d'"' -f4 | tr -d 'v')
  if [ -z "$INSTALL_VERSION" ]; then
    echo "Failed to get latest stable version from GitHub."
    print_prerelease_hint
    exit 1
  fi
else
  INSTALL_VERSION="$VERSION"
  # Remove 'v' prefix if present
  INSTALL_VERSION=$(echo "$INSTALL_VERSION" | sed 's/^v//')
fi

echo "Downloading vigil-agent v${INSTALL_VERSION}..."

# Download checksums file
#
# Security: the checksum is the only integrity control on the downloaded binary, so it must
# come from a trusted source. When --mirror is used the binary is fetched from an arbitrary
# third-party host; fetching the checksum from that same host would let a malicious mirror
# serve a backdoored binary together with a matching checksum and defeat verification. We
# therefore always fetch the checksum from the canonical GitHub host (a tiny file), even
# under --mirror — only the larger binary goes through the mirror below.
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR" || exit 1
CHECKSUM_NAME="vigil_${INSTALL_VERSION}_checksums.txt"
CANONICAL_CHECKSUM_URL="https://github.com/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/${CHECKSUM_NAME}"
CHECKSUM=$(curl -fsSL "$CANONICAL_CHECKSUM_URL" | grep "$FILE_NAME" | cut -d' ' -f1)

# Fall back to the mirror's checksum only when github.com is unreachable AND the operator
# explicitly accepted the reduced integrity guarantee via --insecure-mirror.
if { [ -z "$CHECKSUM" ] || ! echo "$CHECKSUM" | grep -qE "^[a-fA-F0-9]{64}$"; } && [ -n "$GITHUB_PROXY_URL" ] && [ "$INSECURE_MIRROR" = "true" ]; then
  echo "WARNING: could not fetch the checksum from the canonical GitHub host (github.com)." >&2
  echo "WARNING: --insecure-mirror is set, falling back to the mirror's checksum." >&2
  echo "WARNING: integrity is NOT guaranteed — the mirror provides both the binary and its checksum." >&2
  CHECKSUM=$(curl -fsSL "$GITHUB_URL/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/${CHECKSUM_NAME}" | grep "$FILE_NAME" | cut -d' ' -f1)
fi

if [ -z "$CHECKSUM" ] || ! echo "$CHECKSUM" | grep -qE "^[a-fA-F0-9]{64}$"; then
  echo "Failed to get a valid checksum from the canonical GitHub host (github.com)."
  if [ -n "$GITHUB_PROXY_URL" ]; then
    echo "github.com must be reachable for the checksum even when --mirror is used (only the larger binary goes through the mirror)."
    echo "If github.com is fully blocked and you trust the mirror, re-run with --insecure-mirror to accept the mirror's checksum (reduced integrity)."
  else
    echo "Try again with --mirror (or --mirror <url>) if GitHub is not reachable."
  fi
  rm -rf "$TEMP_DIR"
  exit 1
fi

if ! curl -fL# --retry 3 --retry-delay 2 --connect-timeout 10 "$GITHUB_URL/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/$FILE_NAME" -o "$FILE_NAME"; then
  echo "Failed to download the agent from $GITHUB_URL/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/$FILE_NAME"
  echo "Try again with --mirror (or --mirror <url>) if GitHub is not reachable."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if ! tar -tzf "$FILE_NAME" >/dev/null 2>&1; then
  echo "Downloaded archive is invalid or incomplete (possible network/proxy issue)."
  echo "Try again with --mirror (or --mirror <url>) if the download path is unstable."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if [ "$($CHECK_CMD "$FILE_NAME" | cut -d' ' -f1)" != "$CHECKSUM" ]; then
  echo "Checksum verification failed: $($CHECK_CMD "$FILE_NAME" | cut -d' ' -f1) & $CHECKSUM"
  rm -rf "$TEMP_DIR"
  exit 1
fi

if ! tar -xzf "$FILE_NAME" vigil-agent; then
  echo "Failed to extract the agent"
  rm -rf "$TEMP_DIR"
  exit 1
fi

if [ ! -s "$TEMP_DIR/vigil-agent" ]; then
  echo "Downloaded binary is missing or empty."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if [ -f "$BIN_PATH" ]; then
  echo "Backing up existing binary..."
  cp "$BIN_PATH" "$BIN_PATH.bak"
fi

mv vigil-agent "$BIN_PATH"
chown 0:0 "$BIN_PATH"
chmod 755 "$BIN_PATH"

# Set SELinux context if needed
set_selinux_context

# Cleanup
rm -rf "$TEMP_DIR"

# Migrate installs that ran as the legacy "app" user (detected above).
if [ "$LEGACY_INSTALL" = true ]; then
  echo "Migrating the agent service from the '$LEGACY_AGENT_USER' user to '$AGENT_USER'..."
  # Keep Docker inventory working across the migration unless told otherwise: the legacy
  # installer granted docker access unconditionally.
  if [ -z "$DOCKER_ACCESS" ] && user_in_group "$LEGACY_AGENT_USER" docker; then
    echo "The previous service user had Docker socket access; carrying it over (pass --no-docker to drop it)."
    DOCKER_ACCESS=true
  fi
fi

# Create the dedicated, unprivileged user for the service if it doesn't exist. This runs once
# the new binary is verified, so a failed download leaves an existing install untouched.
echo "Configuring the dedicated user for the Vigil Agent service..."
if is_alpine; then
  if ! id -u "$AGENT_USER" >/dev/null 2>&1; then
    addgroup -S "$AGENT_USER"
    adduser -S -D -H -h /nonexistent -s /sbin/nologin -G "$AGENT_USER" "$AGENT_USER"
  fi

elif is_openwrt; then
  # No useradd on OpenWrt: pick the first free system id below 1000 and edit the files directly.
  if ! id -u "$AGENT_USER" >/dev/null 2>&1 && ! grep -q "^$AGENT_USER:" /etc/passwd 2>/dev/null; then
    AGENT_ID=999
    while cut -d: -f3 /etc/passwd /etc/group | grep -qx "$AGENT_ID"; do
      AGENT_ID=$((AGENT_ID - 1))
    done
    grep -q "^$AGENT_USER:" /etc/group || echo "$AGENT_USER:x:$AGENT_ID:" >> /etc/group
    echo "$AGENT_USER:x:$AGENT_ID:$AGENT_ID::/nonexistent:/bin/false" >> /etc/passwd
  fi

elif is_freebsd; then
  if is_opnsense; then
    echo "OPNsense detected: skipping user creation (using daemon user instead)"
    AGENT_USER="daemon"
  elif ! id -u "$AGENT_USER" >/dev/null 2>&1; then
    pw user add "$AGENT_USER" -d /nonexistent -s /usr/sbin/nologin -c "Vigil Agent"
  fi

else
  if ! id -u "$AGENT_USER" >/dev/null 2>&1; then
    # Reuse a leftover group of the same name: useradd --user-group refuses to create it twice.
    if getent group "$AGENT_USER" >/dev/null 2>&1; then
      AGENT_GROUP_OPT="-g $AGENT_USER"
    else
      AGENT_GROUP_OPT="--user-group"
    fi
    useradd --system $AGENT_GROUP_OPT --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin \
      --comment "Vigil Agent" "$AGENT_USER"
  fi
fi

apply_docker_access

# The agent persists its fingerprint (its identity on the hub) here. systemd's
# StateDirectory= also manages it, but OpenRC needs it created for the service user, and a
# migration from the legacy user must hand the existing files over. Only that migration
# recurses: the agent writes into this dir, and root should not chown its content blindly.
mkdir -p "$AGENT_STATE_DIR"
if [ "$LEGACY_INSTALL" = true ]; then
  chown -R "${AGENT_USER}:${AGENT_USER}" "$AGENT_STATE_DIR"
else
  chown "${AGENT_USER}:${AGENT_USER}" "$AGENT_STATE_DIR"
fi

# Modify service installation part, add Alpine check before systemd service creation
if is_alpine; then
  if [ ! -f /etc/init.d/vigil-agent ]; then
    echo "Creating OpenRC service for Alpine Linux..."
    cat >/etc/init.d/vigil-agent <<EOF
#!/sbin/openrc-run

name="vigil-agent"
description="Vigil Agent Service"
command="$BIN_PATH"
command_user="$AGENT_USER"
command_background="yes"
pidfile="/run/\${RC_SVCNAME}.pid"
output_log="/var/log/vigil-agent.log"
error_log="/var/log/vigil-agent.err"

start_pre() {
    checkpath -f -m 0644 -o $AGENT_USER:$AGENT_USER "\$output_log" "\$error_log"
}

# Load KEY/TOKEN/HUB_URL from the root-only env file written at install time.
# Values are single-quote-escaped there, so sourcing cannot execute injected code.
if [ -r "$AGENT_DIR/agent.env" ]; then
    . "$AGENT_DIR/agent.env"
    export KEY TOKEN HUB_URL
fi

depend() {
    need net
    after firewall
}
EOF
    chmod +x /etc/init.d/vigil-agent
    write_shell_env_file "$AGENT_DIR/agent.env"
    rc-update add vigil-agent default
  else
    if [ "$LEGACY_INSTALL" = true ]; then
      echo "Switching the OpenRC service to the $AGENT_USER user..."
      sed -i -e "s/^command_user=\"$LEGACY_AGENT_USER\"\$/command_user=\"$AGENT_USER\"/" \
        -e "s/-o $LEGACY_AGENT_USER:$LEGACY_AGENT_USER /-o $AGENT_USER:$AGENT_USER /" /etc/init.d/vigil-agent
    else
      echo "Alpine OpenRC service file already exists. Skipping creation."
    fi
  fi

  # Create log files with proper permissions
  touch /var/log/vigil-agent.log /var/log/vigil-agent.err
  chown "$AGENT_USER:$AGENT_USER" /var/log/vigil-agent.log /var/log/vigil-agent.err

  # Start the service
  rc-service vigil-agent restart

  # Check if service started successfully
  sleep 2
  if ! rc-service vigil-agent status | grep -q "started"; then
    echo "Error: The Vigil Agent service failed to start. Checking logs..."
    tail -n 20 /var/log/vigil-agent.err
    exit 1
  fi

  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    warn_auto_update_unavailable
  fi

  # Check service status
  if ! rc-service vigil-agent status >/dev/null 2>&1; then
    echo "Error: The Vigil Agent service is not running."
    rc-service vigil-agent status
    exit 1
  fi

elif is_openwrt; then
  if [ ! -f /etc/init.d/vigil-agent ]; then
    echo "Creating procd init script service for OpenWRT..."
    cat >/etc/init.d/vigil-agent <<EOF
#!/bin/sh /etc/rc.common

USE_PROCD=1
START=99

start_service() {
    # Load KEY/TOKEN/HUB_URL from the root-only env file (values are
    # single-quote-escaped there, so sourcing cannot execute injected code).
    [ -r "$AGENT_DIR/agent.env" ] && . "$AGENT_DIR/agent.env"
    # /var is a tmpfs on OpenWrt: recreate the state dir for the unprivileged user on each boot.
    mkdir -p $AGENT_STATE_DIR
    chown $AGENT_USER:$AGENT_USER $AGENT_STATE_DIR
    procd_open_instance
    procd_set_param command $BIN_PATH
    procd_set_param user $AGENT_USER
    procd_set_param pidfile /var/run/vigil-agent.pid
    procd_set_param env KEY="\$KEY" TOKEN="\$TOKEN" HUB_URL="\$HUB_URL"
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}

EOF
    # Enable the service
    chmod +x /etc/init.d/vigil-agent
    write_shell_env_file "$AGENT_DIR/agent.env"
    /etc/init.d/vigil-agent enable
  else
    if [ "$LEGACY_INSTALL" = true ]; then
      echo "Switching the procd service to the $AGENT_USER user..."
      sed -i "s/procd_set_param user $LEGACY_AGENT_USER\$/procd_set_param user $AGENT_USER/" /etc/init.d/vigil-agent
    else
      echo "OpenWRT init script already exists. Skipping creation."
    fi
  fi

  # Start the service
  /etc/init.d/vigil-agent restart

  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    warn_auto_update_unavailable
  fi

  # Check service status
  if ! /etc/init.d/vigil-agent running >/dev/null 2>&1; then
    echo "Error: The Vigil Agent service is not running."
    /etc/init.d/vigil-agent status
    exit 1
  fi

elif is_freebsd; then
  echo "Checking for existing FreeBSD service configuration..."
  # Ensure rc.d directory exists on minimal FreeBSD installs
  mkdir -p /usr/local/etc/rc.d
  
  # Create environment configuration file with proper permissions if it doesn't exist
  if [ ! -f "$AGENT_DIR/env" ]; then
    echo "Creating environment configuration file..."
    (
      umask 077
      {
        printf 'KEY=%s\n' "$(sq "$KEY")"
        printf 'TOKEN=%s\n' "$(sq "$TOKEN")"
        printf 'HUB_URL=%s\n' "$(sq "$HUB_URL")"
      } >"$AGENT_DIR/env"
    )
    chmod 640 "$AGENT_DIR/env"
    chown "root:${AGENT_USER}" "$AGENT_DIR/env"
  else
    echo "FreeBSD environment file already exists. Skipping creation."
  fi
  
  # Create the rc service file if it doesn't exist
  if [ ! -f /usr/local/etc/rc.d/vigil-agent ]; then
    echo "Creating FreeBSD rc service..."
    generate_freebsd_rc_service > /usr/local/etc/rc.d/vigil-agent
    # Set proper permissions for the rc script
    chmod 755 /usr/local/etc/rc.d/vigil-agent
  else
    echo "FreeBSD rc service file already exists. Skipping creation."
  fi

  # Enable and start the service
  echo "Enabling and starting the agent service..."
  sysrc vigil_agent_enable="YES"
  sysrc vigil_agent_user="${AGENT_USER}"
  service vigil-agent restart
  
  # Check if service started successfully
  sleep 2
  if ! service vigil-agent status | grep -q "is running"; then
    echo "Error: The Vigil Agent service failed to start. Checking logs..."
    tail -n 20 /var/log/vigil_agent.log
    exit 1
  fi

  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    warn_auto_update_unavailable
  fi

  # Check service status
  if ! service vigil-agent status >/dev/null 2>&1; then
    echo "Error: The Vigil Agent service is not running."
    service vigil-agent status
    exit 1
  fi

else
  # systemd service installation
  echo "Configuring the systemd service for the agent..."

  SERVICE_FILE="/etc/systemd/system/vigil-agent.service"
  ENV_FILE="$AGENT_DIR/agent.env"

  # Migrate legacy units that inlined the secrets via Environment="VAR=..." into
  # the root-only EnvironmentFile, so an upgrade does not lose KEY/TOKEN/HUB_URL.
  # (We only read the unit we previously generated here, never user-controlled
  # input re-executed as code.)
  if [ ! -f "$ENV_FILE" ] && [ -f "$SERVICE_FILE" ] && grep -q '^Environment="' "$SERVICE_FILE"; then
    echo "Migrating inline service secrets to $ENV_FILE..."
    ( umask 077; : >"$ENV_FILE" )
    for _k in KEY TOKEN HUB_URL; do
      _val=$(sed -n "s/^Environment=\"${_k}=\(.*\)\"\$/\1/p" "$SERVICE_FILE" | head -n1)
      [ -n "$_val" ] && printf '%s=%s\n' "$_k" "$_val" >>"$ENV_FILE"
    done
    chmod 600 "$ENV_FILE" 2>/dev/null || true
    chown root:root "$ENV_FILE" 2>/dev/null || true
  fi

  # Apply any newly provided values. Only non-empty values are written, so an
  # upgrade that omits -t/-k/-url preserves whatever is already there. systemd
  # reads this file as root and never runs a shell on it, so crafted values
  # cannot inject commands (unlike the previous inline Environment=/sed path).
  upsert_systemd_env "$ENV_FILE" KEY "$KEY"
  upsert_systemd_env "$ENV_FILE" TOKEN "$TOKEN"
  upsert_systemd_env "$ENV_FILE" HUB_URL "$HUB_URL"

  # The unit is fully managed by this script: (re)write it every run so the
  # EnvironmentFile directive and sandboxing stay canonical.
  cat >"$SERVICE_FILE" <<EOF
[Unit]
Description=Vigil Agent Service
Wants=network-online.target
After=network-online.target

[Service]
EnvironmentFile=-$AGENT_DIR/agent.env
ExecStart=$BIN_PATH
User=$AGENT_USER
Group=$AGENT_USER
Restart=on-failure
RestartSec=5
StateDirectory=vigil-agent

# Security/sandboxing settings (keep in sync with supplemental/debian/vigil-agent.service)
NoNewPrivileges=yes
PrivateTmp=yes
KeyringMode=private
LockPersonality=yes
ProtectClock=yes
ProtectHome=read-only
ProtectHostname=yes
ProtectKernelLogs=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectSystem=strict
RemoveIPC=yes
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
EOF

  # Load and start the service
  printf "\nLoading and starting the agent service...\n"
  systemctl daemon-reload
  systemctl enable vigil-agent.service >/dev/null 2>&1
  systemctl restart vigil-agent.service
  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    warn_auto_update_unavailable
  fi

  # Wait for the service to start or fail
  if [ "$(systemctl is-active vigil-agent.service)" != "active" ]; then
    echo "Error: The Vigil Agent service is not running."
    echo "$(systemctl status vigil-agent.service)"
    exit 1
  fi
fi

if [ "$LEGACY_INSTALL" = true ]; then
  print_legacy_user_notice
fi

printf "\n\033[32mVigil Agent has been installed successfully!\033[0m\n"
