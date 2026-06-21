#!/bin/sh

is_freebsd() {
  [ "$(uname -s)" = "FreeBSD" ]
}

# Function to ensure the proxy URL ends with a /
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

# Generate FreeBSD rc service content
generate_freebsd_rc_service() {
  cat <<'EOF'
#!/bin/sh

# PROVIDE: vigil_hub
# REQUIRE: DAEMON NETWORKING
# BEFORE: LOGIN
# KEYWORD: shutdown

# Add the following lines to /etc/rc.conf to configure Vigil Hub:
#
# vigil_hub_enable (bool):   Set to YES to enable Vigil Hub
#                             Default: YES
# vigil_hub_port (str):      Port to listen on
#                             Default: 8090
# vigil_hub_user (str):      Vigil Hub daemon user
#                             Default: vigil
# vigil_hub_bin (str):       Path to the vigil binary
#                             Default: /usr/local/sbin/vigil
# vigil_hub_data (str):      Path to the data directory
#                             Default: /usr/local/etc/vigil/vigil_data
# vigil_hub_flags (str):     Extra flags passed to the vigil command invocation
#                             Default:

. /etc/rc.subr

name="vigil_hub"
rcvar=vigil_hub_enable

load_rc_config $name
: ${vigil_hub_enable:="YES"}
: ${vigil_hub_port:="8090"}
: ${vigil_hub_user:="vigil"}
: ${vigil_hub_flags:=""}
: ${vigil_hub_bin:="/usr/local/sbin/vigil"}
: ${vigil_hub_data:="/usr/local/etc/vigil/vigil_data"}

logfile="/var/log/${name}.log"
pidfile="/var/run/${name}.pid"

procname="/usr/sbin/daemon"
start_precmd="${name}_prestart"
start_cmd="${name}_start"
stop_cmd="${name}_stop"

extra_commands="upgrade"
upgrade_cmd="vigil_hub_upgrade"

vigil_hub_prestart()
{
    if [ ! -d "${vigil_hub_data}" ]; then
        echo "Creating data directory ${vigil_hub_data}"
        mkdir -p "${vigil_hub_data}"
        chown "${vigil_hub_user}:${vigil_hub_user}" "${vigil_hub_data}"
    fi
}

vigil_hub_start()
{
    echo "Starting ${name}"
    cd "$(dirname "${vigil_hub_data}")" || exit 1
    /usr/sbin/daemon -f \
            -P "${pidfile}" \
            -o "${logfile}" \
            -u "${vigil_hub_user}" \
            "${vigil_hub_bin}" serve --http "0.0.0.0:${vigil_hub_port}" ${vigil_hub_flags}
}

vigil_hub_stop()
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

vigil_hub_upgrade()
{
    echo "Upgrading ${name}"
    if command -v sudo >/dev/null; then
        sudo -u "${vigil_hub_user}" -- "${vigil_hub_bin}" update
    else
        su -m "${vigil_hub_user}" -c "${vigil_hub_bin} update"
    fi
}

run_rc_command "$1"
EOF
}

# Detect system architecture
detect_architecture() {
  arch=$(uname -m)
  case "$arch" in
    x86_64)
      arch="amd64"
      ;;
    armv7l)
      arch="arm"
      ;;
    aarch64)
      arch="arm64"
      ;;
  esac
  echo "$arch"
}

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

# Define default values
PORT=8090
GITHUB_URL="https://github.com"
GITHUB_PROXY_URL=""
INSECURE_MIRROR=false
AUTO_UPDATE_FLAG="false"
UNINSTALL=false
VERSION="latest"

# Parse command line arguments
while [ $# -gt 0 ]; do
  case "$1" in
    -u)
      UNINSTALL=true
      shift
      ;;
    -h|--help)
      printf "Vigil Hub installation script\n\n"
      printf "Usage: ./install-hub.sh [options]\n\n"
      printf "Options: \n"
      printf "  -u           : Uninstall the Vigil Hub\n"
      printf "  -p <port>    : Specify a port number (default: 8090)\n"
      printf "  -v <version> : Install a specific version (default: latest)\n"
      printf "  -c, --mirror [URL] : Use a GitHub mirror/proxy URL (default: https://gh.github.com)\n"
      printf "  --insecure-mirror : With --mirror, allow the checksum to come from the mirror when\n"
      printf "                      github.com is unreachable (reduced integrity). Use only if you\n"
      printf "                      trust the mirror and github.com is fully blocked.\n"
      printf "  --auto-update : Enable automatic daily updates (disabled by default)\n"
      printf "  -h, --help   : Display this help message\n"
      exit 0
      ;;
    -p)
      shift
      PORT="$1"
      shift
      ;;
    -v | --version)
      shift
      VERSION="$1"
      shift
      ;;
    -c | --mirror)
      shift
      if [ -n "$1" ] && ! echo "$1" | grep -q '^-'; then
        GITHUB_PROXY_URL="$(ensure_trailing_slash "$1")https://github.com"
        GITHUB_URL="$GITHUB_PROXY_URL"
        shift
      else
        GITHUB_PROXY_URL="https://gh.github.com"
        GITHUB_URL="$GITHUB_PROXY_URL"
      fi
      ;;
    --insecure-mirror)
      INSECURE_MIRROR=true
      shift
      ;;
    --auto-update)
      AUTO_UPDATE_FLAG="true"
      shift
      ;;
    *)
      echo "Invalid option: $1" >&2
      exit 1
      ;;
  esac
done

# Set paths based on operating system
if is_freebsd; then
  HUB_DIR="/usr/local/etc/vigil"
  BIN_PATH="/usr/local/sbin/vigil"
else
  HUB_DIR="/opt/vigil"
  BIN_PATH="/opt/vigil/vigil"
fi

# Uninstall process
if [ "$UNINSTALL" = true ]; then
  if is_freebsd; then
    echo "Stopping and disabling the Vigil Hub service..."
    service vigil-hub stop 2>/dev/null
    sysrc vigil_hub_enable="NO" 2>/dev/null

    echo "Removing the FreeBSD service files..."
    rm -f /usr/local/etc/rc.d/vigil-hub

    echo "Removing the daily update cron job..."
    rm -f /etc/cron.d/vigil-hub

    echo "Removing log files..."
    rm -f /var/log/vigil_hub.log

    echo "Removing the Vigil Hub binary and data..."
    rm -f "$BIN_PATH"
    if [ -n "$HUB_DIR" ] && [ "$HUB_DIR" != "/" ]; then
      rm -rf "$HUB_DIR"
    fi

    echo "Removing the dedicated user..."
    pw user del vigil 2>/dev/null

    echo "The Vigil Hub has been uninstalled successfully!"
    exit 0
  else
    # Stop and disable the Vigil Hub service
    echo "Stopping and disabling the Vigil Hub service..."
    systemctl stop vigil-hub.service
    systemctl disable vigil-hub.service

    # Remove the systemd service file
    echo "Removing the systemd service file..."
    rm -f /etc/systemd/system/vigil-hub.service

    # Remove the update timer and service if they exist
    echo "Removing the daily update service and timer..."
    systemctl stop vigil-hub-update.timer 2>/dev/null
    systemctl disable vigil-hub-update.timer 2>/dev/null
    rm -f /etc/systemd/system/vigil-hub-update.service
    rm -f /etc/systemd/system/vigil-hub-update.timer

    # Reload the systemd daemon
    echo "Reloading the systemd daemon..."
    systemctl daemon-reload

    # Remove the Vigil Hub binary and data
    echo "Removing the Vigil Hub binary and data..."
    if [ -n "$HUB_DIR" ] && [ "$HUB_DIR" != "/" ]; then
      rm -rf "$HUB_DIR"
    fi

    # Remove the dedicated user
    echo "Removing the dedicated user..."
    userdel vigil 2>/dev/null

    echo "The Vigil Hub has been uninstalled successfully!"
    exit 0
  fi
fi

# Function to check if a package is installed
package_installed() {
  command -v "$1" >/dev/null 2>&1
}

# Check for package manager and install necessary packages if not installed
if package_installed pkg && is_freebsd; then
  if ! package_installed tar || ! package_installed curl; then
    pkg update
    pkg install -y gtar curl
  fi
elif package_installed apt-get; then
  if ! package_installed tar || ! package_installed curl; then
    apt-get update
    apt-get install -y tar curl
  fi
elif package_installed yum; then
  if ! package_installed tar || ! package_installed curl; then
    yum install -y tar curl
  fi
elif package_installed pacman; then
  if ! package_installed tar || ! package_installed curl; then
    pacman -Sy --noconfirm tar curl
  fi
else
  echo "Warning: Please ensure 'tar' and 'curl' are installed."
fi

# Create a dedicated user for the service if it doesn't exist
echo "Creating a dedicated user for the Vigil Hub service..."
if is_freebsd; then
  if ! id -u vigil >/dev/null 2>&1; then
    pw user add vigil -d /nonexistent -s /usr/sbin/nologin -c "vigil user"
  fi
else
  if ! id -u vigil >/dev/null 2>&1; then
    useradd -M -s /bin/false vigil
  fi
fi

# Create the directory for the Vigil Hub
echo "Creating the directory for the Vigil Hub..."
mkdir -p "$HUB_DIR/vigil_data"
chown -R vigil:vigil "$HUB_DIR"
# Lock down the hub directory so local users cannot traverse into vigil_data, which holds
# the ED25519 hub private key (id_ed25519) and the AES credentials key (credentials.key).
chmod 750 "$HUB_DIR"
chmod 700 "$HUB_DIR/vigil_data"

# Download and install the Vigil Hub
echo "Downloading and installing the Vigil Hub..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(detect_architecture)
FILE_NAME="vigil_${OS}_${ARCH}.tar.gz"

# Select the checksum tool for this platform
if is_freebsd; then
  CHECK_CMD="sha256 -q"
else
  CHECK_CMD="sha256sum"
fi

# Resolve the version to install (the checksums file name embeds the version,
# so we must know it before downloading; make_latest may be unset on releases).
if [ "$VERSION" = "latest" ]; then
  API_RELEASE_URL="https://api.github.com/repos/Gu1llaum-3/vigil/releases/latest"
  INSTALL_VERSION=$(curl -fsSL "$API_RELEASE_URL" | grep -o '"tag_name": "v[^"]*"' | cut -d'"' -f4 | tr -d 'v')
  if [ -z "$INSTALL_VERSION" ]; then
    echo "Failed to get the latest stable version from GitHub."
    echo "Specify a version explicitly with -v <version>, or try --mirror <url> if GitHub is not reachable."
    exit 1
  fi
else
  INSTALL_VERSION=$(echo "$VERSION" | sed 's/^v//')
fi

echo "Installing vigil hub v${INSTALL_VERSION}..."

TEMP_DIR=$(mktemp -d)
ARCHIVE_PATH="$TEMP_DIR/$FILE_NAME"
RELEASE_BASE="$GITHUB_URL/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}"
DOWNLOAD_URL="$RELEASE_BASE/$FILE_NAME"

# Fetch and validate the published SHA-256 checksum for this artifact.
#
# Security: the checksum is the only integrity control on the downloaded binary, so it must
# come from a trusted source. When --mirror is used the binary is fetched from an arbitrary
# third-party host; fetching the checksum from that same host would let a malicious mirror
# serve a backdoored binary together with a matching checksum and defeat verification. We
# therefore always fetch the checksum from the canonical GitHub host (a tiny file), even
# under --mirror — only the larger binary goes through the mirror below.
CHECKSUM_NAME="vigil_${INSTALL_VERSION}_checksums.txt"
CANONICAL_CHECKSUM_URL="https://github.com/Gu1llaum-3/vigil/releases/download/v${INSTALL_VERSION}/${CHECKSUM_NAME}"
CHECKSUM=$(curl -fsSL "$CANONICAL_CHECKSUM_URL" | grep "$FILE_NAME" | cut -d' ' -f1)

# Fall back to the mirror's checksum only when github.com is unreachable AND the operator
# explicitly accepted the reduced integrity guarantee via --insecure-mirror.
if { [ -z "$CHECKSUM" ] || ! echo "$CHECKSUM" | grep -qE "^[a-fA-F0-9]{64}$"; } && [ -n "$GITHUB_PROXY_URL" ] && [ "$INSECURE_MIRROR" = "true" ]; then
  echo "WARNING: could not fetch the checksum from the canonical GitHub host (github.com)." >&2
  echo "WARNING: --insecure-mirror is set, falling back to the mirror's checksum." >&2
  echo "WARNING: integrity is NOT guaranteed — the mirror provides both the binary and its checksum." >&2
  CHECKSUM=$(curl -fsSL "$RELEASE_BASE/${CHECKSUM_NAME}" | grep "$FILE_NAME" | cut -d' ' -f1)
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

if ! curl -fL# --retry 3 --retry-delay 2 --connect-timeout 10 "$DOWNLOAD_URL" -o "$ARCHIVE_PATH"; then
  echo "Failed to download the Vigil Hub from:"
  echo "$DOWNLOAD_URL"
  echo "Try again with --mirror (or --mirror <url>) if GitHub is not reachable."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if ! tar -tzf "$ARCHIVE_PATH" >/dev/null 2>&1; then
  echo "Downloaded archive is invalid or incomplete (possible network/proxy issue)."
  echo "Try again with --mirror (or --mirror <url>) if the download path is unstable."
  rm -rf "$TEMP_DIR"
  exit 1
fi

# Verify integrity before trusting the archive contents
if [ "$($CHECK_CMD "$ARCHIVE_PATH" | cut -d' ' -f1)" != "$CHECKSUM" ]; then
  echo "Checksum verification failed for $FILE_NAME."
  echo "Expected: $CHECKSUM"
  echo "Got:      $($CHECK_CMD "$ARCHIVE_PATH" | cut -d' ' -f1)"
  rm -rf "$TEMP_DIR"
  exit 1
fi

if ! tar -xzf "$ARCHIVE_PATH" -C "$TEMP_DIR" vigil; then
  echo "Failed to extract the vigil binary from the archive."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if [ ! -s "$TEMP_DIR/vigil" ]; then
  echo "Downloaded binary is missing or empty."
  rm -rf "$TEMP_DIR"
  exit 1
fi

chmod +x "$TEMP_DIR/vigil"
mv "$TEMP_DIR/vigil" "$BIN_PATH"
chown vigil:vigil "$BIN_PATH"
rm -rf "$TEMP_DIR"

if is_freebsd; then
  echo "Creating FreeBSD rc service..."

  # Create the rc service file
  generate_freebsd_rc_service > /usr/local/etc/rc.d/vigil-hub

  # Set proper permissions for the rc script
  chmod 755 /usr/local/etc/rc.d/vigil-hub

  # Configure the port
  sysrc vigil_hub_port="$PORT"

  # Enable and start the service
  echo "Enabling and starting the Vigil Hub service..."
  sysrc vigil_hub_enable="YES"
  service vigil-hub restart

  # Check if service started successfully
  sleep 2
  if ! service vigil-hub status | grep -q "is running"; then
    echo "Error: The Vigil Hub service failed to start. Checking logs..."
    tail -n 20 /var/log/vigil_hub.log
    exit 1
  fi

  # Auto-update service for FreeBSD
  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    echo "Setting up daily automatic updates for vigil-hub..."

    # Create cron job in /etc/cron.d
    cat >/etc/cron.d/vigil-hub <<EOF
# Vigil Hub daily update job
12 8 * * * root $BIN_PATH update >/dev/null 2>&1
EOF
    chmod 644 /etc/cron.d/vigil-hub
    printf "\nDaily updates have been enabled via /etc/cron.d.\n"
  fi

  # Check service status
  if ! service vigil-hub status >/dev/null 2>&1; then
    echo "Error: The Vigil Hub service is not running."
    service vigil-hub status
    exit 1
  fi

else
  # Original systemd service installation code
  printf "Creating the systemd service for the Vigil Hub...\n"
  cat >/etc/systemd/system/vigil-hub.service <<EOF
[Unit]
Description=Vigil Hub Service
After=network.target

[Service]
ExecStart=$BIN_PATH serve --http "0.0.0.0:$PORT"
WorkingDirectory=$HUB_DIR
User=vigil
Group=vigil
Restart=always
RestartSec=5

# Security/sandboxing settings (the hub is the internet-facing component and holds the
# private keys, so it gets at least the same hardening as the agent unit). ProtectSystem=strict
# makes the filesystem read-only except ReadWritePaths, which must include the data dir.
NoNewPrivileges=yes
PrivateTmp=yes
LockPersonality=yes
ProtectClock=yes
ProtectHome=read-only
ProtectHostname=yes
ProtectKernelLogs=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectSystem=strict
ReadWritePaths=$HUB_DIR
RemoveIPC=yes
RestrictSUIDSGID=true

[Install]
WantedBy=multi-user.target
EOF

  # Load and start the service
  printf "Loading and starting the Vigil Hub service...\n"
  systemctl daemon-reload
  systemctl enable --quiet vigil-hub.service
  systemctl start --quiet vigil-hub.service

  # Wait for the service to start or fail
  sleep 2

  # Check if the service is running
  if [ "$(systemctl is-active vigil-hub.service)" != "active" ]; then
    echo "Error: The Vigil Hub service is not running."
    echo "$(systemctl status vigil-hub.service)"
    exit 1
  fi

  # Enable auto-update if flag is set to true
  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    echo "Setting up daily automatic updates for vigil-hub..."

    # Create systemd service for the daily update
    cat >/etc/systemd/system/vigil-hub-update.service <<EOF
[Unit]
Description=Update vigil-hub if needed
Wants=vigil-hub.service

[Service]
Type=oneshot
# Run the self-updater as the unprivileged service account, not root. The binary lives in
# $HUB_DIR (owned by vigil), so vigil can replace it; ReadWritePaths grants exactly that.
User=vigil
Group=vigil
ExecStart=$BIN_PATH update
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths=$HUB_DIR
EOF

    # Create systemd timer for the daily update
    cat >/etc/systemd/system/vigil-hub-update.timer <<EOF
[Unit]
Description=Run vigil-hub update daily

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=4h

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
    systemctl enable --now vigil-hub-update.timer

    printf "\nDaily updates have been enabled.\n"
  fi
fi

printf "\n\033[32mVigil Hub has been installed successfully! It is now accessible on port $PORT.\033[0m\n"
