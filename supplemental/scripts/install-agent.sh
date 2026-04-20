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
  echo "Example: ./install-agent.sh --version v0.1.0-beta.5 ..."
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
#                               Default: app
# vigil_agent_bin (str):       Path to the vigil-agent binary
#                               Default: /usr/local/sbin/vigil-agent
# vigil_agent_flags (str):     Extra flags passed to vigil-agent command invocation
#                               Default:

. /etc/rc.subr

name="vigil_agent"
rcvar=vigil_agent_enable

load_rc_config $name
: ${vigil_agent_enable:="YES"}
: ${vigil_agent_user:="app"}
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
KEY=""
TOKEN=""
HUB_URL=""
AUTO_UPDATE_FLAG="" # empty string means unused, "true" warns and skips, "false" means skip
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
  printf "  --auto-update [VALUE] : Reserved for future use (currently ignored)\n"
  printf "                          VALUE can be true or false; the flag is accepted for compatibility.\n"
  printf "  --mirror [URL]        : Use GitHub proxy to resolve network timeout issues in mainland China\n"
  printf "                          URL: optional custom proxy URL (default: https://gh.github.com)\n"
  print_supported_targets
  printf "  -h, --help            : Display this help message\n"
  exit 0
  ;;
esac

# Build sudo args by properly quoting everything
build_sudo_args() {
  QUOTED_ARGS=""
  while [ $# -gt 0 ]; do
    if [ -n "$QUOTED_ARGS" ]; then
      QUOTED_ARGS="$QUOTED_ARGS "
    fi
    QUOTED_ARGS="$QUOTED_ARGS'$(echo "$1" | sed "s/'/'\\\\''/g")'"
    shift
  done
  echo "$QUOTED_ARGS"
}

# Check if running as root and re-execute with sudo if needed
if [ "$(id -u)" != "0" ]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO_ARGS=$(build_sudo_args "$@")
    eval "exec sudo $0 $SUDO_ARGS"
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
    rm /etc/systemd/system/vigil-agent.service

    # Remove the update timer and service if they exist
    echo "Removing the daily update service and timer..."
    systemctl stop vigil-agent-update.timer 2>/dev/null
    systemctl disable vigil-agent-update.timer >/dev/null 2>&1
    rm -f /etc/systemd/system/vigil-agent-update.service
    rm -f /etc/systemd/system/vigil-agent-update.timer

    systemctl daemon-reload
  fi

  echo "Removing the Vigil Agent directory..."
  rm -rf "$AGENT_DIR"

  echo "Removing the dedicated user for the agent service..."
  killall vigil-agent 2>/dev/null
  if is_alpine || is_openwrt; then
    deluser app 2>/dev/null
  elif is_freebsd; then
    pw user del app 2>/dev/null
  else
    userdel app 2>/dev/null
  fi

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
  fi
fi

# Remove newlines from KEY
KEY=$(echo "$KEY" | tr -d '\n')

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

# Create a dedicated user for the service if it doesn't exist
AGENT_USER="app"
echo "Configuring the dedicated user for the Vigil Agent service..."
if is_alpine; then
  if ! id -u app >/dev/null 2>&1; then
    addgroup app
    adduser -S -D -H -s /sbin/nologin -G app app
  fi
  # Add the user to the docker group to allow access to the Docker socket if group docker exists
  if getent group docker >/dev/null 2>&1; then
    echo "Adding app to docker group"
    addgroup app docker
  fi
  
elif is_openwrt; then
  # Create app group first if it doesn't exist (check /etc/group directly)
  if ! grep -q "^app:" /etc/group >/dev/null 2>&1; then
    echo "app:x:999:" >> /etc/group
  fi
  
  # Create app user if it doesn't exist (double-check to prevent duplicates)
  if ! id -u app >/dev/null 2>&1 && ! grep -q "^app:" /etc/passwd >/dev/null 2>&1; then
    echo "app:x:999:999::/nonexistent:/bin/false" >> /etc/passwd
  fi
  
  # Add the user to the docker group if docker group exists and user is not already in it
  if grep -q "^docker:" /etc/group >/dev/null 2>&1; then
    echo "Adding app to docker group"
    # Check if app is already in docker group
    if ! grep "^docker:" /etc/group | grep -q "app"; then
      # Add app to docker group by modifying /etc/group
      # Handle both cases: group with existing members and group without members
      if grep "^docker:" /etc/group | grep -q ":.*:.*$"; then
        # Group has existing members, append with comma
        sed -i 's/^docker:\([^:]*:[^:]*:\)\(.*\)$/docker:\1\2,app/' /etc/group
      else
        # Group has no members, just append
        sed -i 's/^docker:\([^:]*:[^:]*:\)$/docker:\1app/' /etc/group
      fi
    fi
  fi

elif is_freebsd; then
  if is_opnsense; then
    echo "OPNsense detected: skipping user creation (using daemon user instead)"
    AGENT_USER="daemon"
  else
    if ! id -u app >/dev/null 2>&1; then
      pw user add app -d /nonexistent -s /usr/sbin/nologin -c "app user"
    fi
    # Add the user to the wheel group to allow self-updates
    if pw group show wheel >/dev/null 2>&1; then
      echo "Adding app to wheel group for self-updates"
      pw group mod wheel -m app
    fi
  fi

else
  if ! id -u app >/dev/null 2>&1; then
    useradd --system --home-dir /nonexistent --shell /bin/false app
  fi
  # Add the user to the docker group to allow access to the Docker socket if group docker exists
  if getent group docker >/dev/null 2>&1; then
    echo "Adding app to docker group"
    usermod -aG docker app
  fi
  # Add the user to the disk group to allow access to disk devices if group disk exists
  if getent group disk >/dev/null 2>&1; then
    echo "Adding app to disk group"
    usermod -aG disk app
  fi
fi

# Create the directory for the Vigil Agent

if [ ! -d "$AGENT_DIR" ]; then
  echo "Creating the directory for the Vigil Agent..."
  mkdir -p "$AGENT_DIR"
  chown "${AGENT_USER}:${AGENT_USER}" "$AGENT_DIR"
  chmod 755 "$AGENT_DIR"
fi

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
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR" || exit 1
CHECKSUM=$(curl -fsSL "$GITHUB_URL/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/vigil_${INSTALL_VERSION}_checksums.txt" | grep "$FILE_NAME" | cut -d' ' -f1)
if [ -z "$CHECKSUM" ] || ! echo "$CHECKSUM" | grep -qE "^[a-fA-F0-9]{64}$"; then
  echo "Failed to get checksum or invalid checksum format"
  echo "Try again with --mirror (or --mirror <url>) if GitHub is not reachable."
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
chown "${AGENT_USER}:${AGENT_USER}" "$BIN_PATH"
chmod 755 "$BIN_PATH"

# Set SELinux context if needed
set_selinux_context

# Cleanup
rm -rf "$TEMP_DIR"

# Make sure /etc/machine-id exists for persistent fingerprint
if [ ! -f /etc/machine-id ]; then
  cat /proc/sys/kernel/random/uuid | tr -d '-' > /etc/machine-id
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
command_user="app"
command_background="yes"
pidfile="/run/\${RC_SVCNAME}.pid"
output_log="/var/log/vigil-agent.log"
error_log="/var/log/vigil-agent.err"

start_pre() {
    checkpath -f -m 0644 -o app:app "\$output_log" "\$error_log"
}

export KEY="$KEY"
export TOKEN="$TOKEN"
export HUB_URL="$HUB_URL"

depend() {
    need net
    after firewall
}
EOF
    chmod +x /etc/init.d/vigil-agent
    rc-update add vigil-agent default
  else
    echo "Alpine OpenRC service file already exists. Skipping creation."
  fi

  # Create log files with proper permissions
  touch /var/log/vigil-agent.log /var/log/vigil-agent.err
  chown app:app /var/log/vigil-agent.log /var/log/vigil-agent.err

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
    procd_open_instance
    procd_set_param command $BIN_PATH
    procd_set_param user app
    procd_set_param pidfile /var/run/vigil-agent.pid
    procd_set_param env KEY="$KEY" TOKEN="$TOKEN" HUB_URL="$HUB_URL"
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}

EOF
    # Enable the service
    chmod +x /etc/init.d/vigil-agent
    /etc/init.d/vigil-agent enable
  else
    echo "OpenWRT init script already exists. Skipping creation."
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
    cat >"$AGENT_DIR/env" <<EOF
KEY="$KEY"
TOKEN=$TOKEN
HUB_URL=$HUB_URL
EOF
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
  # Original systemd service installation code
  if [ ! -f /etc/systemd/system/vigil-agent.service ]; then
    echo "Creating the systemd service for the agent..."

    cat >/etc/systemd/system/vigil-agent.service <<EOF
[Unit]
Description=Vigil Agent Service
Wants=network-online.target
After=network-online.target

[Service]
Environment="KEY=$KEY"
Environment="TOKEN=$TOKEN"
Environment="HUB_URL=$HUB_URL"
ExecStart=$BIN_PATH
User=app
Restart=on-failure
RestartSec=5
StateDirectory=vigil-agent

# Security/sandboxing settings
KeyringMode=private
LockPersonality=yes
ProtectClock=yes
ProtectHome=read-only
ProtectHostname=yes
ProtectKernelLogs=yes
ProtectSystem=strict
RemoveIPC=yes
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
EOF
  else
    echo "Systemd service file already exists. Skipping creation."
  fi

  # Always update environment variables in the service file if new values were provided.
  # This ensures that upgrades passing a new -t / -k / -url actually take effect,
  # since the service file is not recreated when it already exists.
  SERVICE_FILE="/etc/systemd/system/vigil-agent.service"
  if [ -n "$TOKEN" ]; then
    sed -i "s|Environment=\"TOKEN=.*\"|Environment=\"TOKEN=$TOKEN\"|" "$SERVICE_FILE"
  fi
  if [ -n "$KEY" ]; then
    sed -i "s|Environment=\"KEY=.*\"|Environment=\"KEY=$KEY\"|" "$SERVICE_FILE"
  fi
  if [ -n "$HUB_URL" ]; then
    sed -i "s|Environment=\"HUB_URL=.*\"|Environment=\"HUB_URL=$HUB_URL\"|" "$SERVICE_FILE"
  fi

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

printf "\n\033[32mVigil Agent has been installed successfully!\033[0m\n"
