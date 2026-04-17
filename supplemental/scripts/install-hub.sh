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
#                             Default: app
# vigil_hub_bin (str):       Path to the app binary
#                             Default: /usr/local/sbin/vigil
# vigil_hub_data (str):      Path to the app data directory
#                             Default: /usr/local/etc/vigil/app_data
# vigil_hub_flags (str):     Extra flags passed to app command invocation
#                             Default:

. /etc/rc.subr

name="vigil_hub"
rcvar=vigil_hub_enable

load_rc_config $name
: ${vigil_hub_enable:="YES"}
: ${vigil_hub_port:="8090"}
: ${vigil_hub_user:="app"}
: ${vigil_hub_flags:=""}
: ${vigil_hub_bin:="/usr/local/sbin/vigil"}
: ${vigil_hub_data:="/usr/local/etc/vigil/app_data"}

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

# Define default values
PORT=8090
GITHUB_URL="https://github.com"
AUTO_UPDATE_FLAG="false"
UNINSTALL=false

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
      printf "  -c, --mirror [URL] : Use a GitHub mirror/proxy URL (default: https://gh.Gu1llaum-3.example)\n"
      printf "  --auto-update : Enable automatic daily updates (disabled by default)\n"
      printf "  -h, --help   : Display this help message\n"
      exit 0
      ;;
    -p)
      shift
      PORT="$1"
      shift
      ;;
    -c | --mirror)
      shift
      if [ -n "$1" ] && ! echo "$1" | grep -q '^-'; then
        GITHUB_URL="$(ensure_trailing_slash "$1")https://github.com"
        shift
      else
        GITHUB_URL="https://gh.Gu1llaum-3.example"
      fi
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
  BIN_PATH="/opt/vigil/app"
fi

# Uninstall process
if [ "$UNINSTALL" = true ]; then
  if is_freebsd; then
    echo "Stopping and disabling the Vigil Hub service..."
    service app-hub stop 2>/dev/null
    sysrc vigil_hub_enable="NO" 2>/dev/null

    echo "Removing the FreeBSD service files..."
    rm -f /usr/local/etc/rc.d/app-hub

    echo "Removing the daily update cron job..."
    rm -f /etc/cron.d/app-hub

    echo "Removing log files..."
    rm -f /var/log/vigil_hub.log

    echo "Removing the Vigil Hub binary and data..."
    rm -f "$BIN_PATH"
    rm -rf "$HUB_DIR"

    echo "Removing the dedicated user..."
    pw user del app 2>/dev/null

    echo "The Vigil Hub has been uninstalled successfully!"
    exit 0
  else
    # Stop and disable the Vigil Hub service
    echo "Stopping and disabling the Vigil Hub service..."
    systemctl stop app-hub.service
    systemctl disable app-hub.service

    # Remove the systemd service file
    echo "Removing the systemd service file..."
    rm -f /etc/systemd/system/app-hub.service

    # Remove the update timer and service if they exist
    echo "Removing the daily update service and timer..."
    systemctl stop app-hub-update.timer 2>/dev/null
    systemctl disable app-hub-update.timer 2>/dev/null
    rm -f /etc/systemd/system/app-hub-update.service
    rm -f /etc/systemd/system/app-hub-update.timer

    # Reload the systemd daemon
    echo "Reloading the systemd daemon..."
    systemctl daemon-reload

    # Remove the Vigil Hub binary and data
    echo "Removing the Vigil Hub binary and data..."
    rm -rf "$HUB_DIR"

    # Remove the dedicated user
    echo "Removing the dedicated user..."
    userdel app 2>/dev/null

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
  if ! id -u app >/dev/null 2>&1; then
    pw user add app -d /nonexistent -s /usr/sbin/nologin -c "app user"
  fi
else
  if ! id -u app >/dev/null 2>&1; then
    useradd -M -s /bin/false app
  fi
fi

# Create the directory for the Vigil Hub
echo "Creating the directory for the Vigil Hub..."
mkdir -p "$HUB_DIR/app_data"
chown -R app:app "$HUB_DIR"
chmod 755 "$HUB_DIR"

# Download and install the Vigil Hub
echo "Downloading and installing the Vigil Hub..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(detect_architecture)
FILE_NAME="app_${OS}_${ARCH}.tar.gz"

TEMP_DIR=$(mktemp -d)
ARCHIVE_PATH="$TEMP_DIR/$FILE_NAME"
DOWNLOAD_URL="$GITHUB_URL/Gu1llaum-3/vigil/releases/latest/download/$FILE_NAME"

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

if ! tar -xzf "$ARCHIVE_PATH" -C "$TEMP_DIR" app; then
  echo "Failed to extract app from archive."
  rm -rf "$TEMP_DIR"
  exit 1
fi

if [ ! -s "$TEMP_DIR/app" ]; then
  echo "Downloaded binary is missing or empty."
  rm -rf "$TEMP_DIR"
  exit 1
fi

chmod +x "$TEMP_DIR/app"
mv "$TEMP_DIR/app" "$BIN_PATH"
chown app:app "$BIN_PATH"
rm -rf "$TEMP_DIR"

if is_freebsd; then
  echo "Creating FreeBSD rc service..."

  # Create the rc service file
  generate_freebsd_rc_service > /usr/local/etc/rc.d/app-hub

  # Set proper permissions for the rc script
  chmod 755 /usr/local/etc/rc.d/app-hub

  # Configure the port
  sysrc vigil_hub_port="$PORT"

  # Enable and start the service
  echo "Enabling and starting the Vigil Hub service..."
  sysrc vigil_hub_enable="YES"
  service app-hub restart

  # Check if service started successfully
  sleep 2
  if ! service app-hub status | grep -q "is running"; then
    echo "Error: The Vigil Hub service failed to start. Checking logs..."
    tail -n 20 /var/log/vigil_hub.log
    exit 1
  fi

  # Auto-update service for FreeBSD
  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    echo "Setting up daily automatic updates for app-hub..."

    # Create cron job in /etc/cron.d
    cat >/etc/cron.d/app-hub <<EOF
# Vigil Hub daily update job
12 8 * * * root $BIN_PATH update >/dev/null 2>&1
EOF
    chmod 644 /etc/cron.d/app-hub
    printf "\nDaily updates have been enabled via /etc/cron.d.\n"
  fi

  # Check service status
  if ! service app-hub status >/dev/null 2>&1; then
    echo "Error: The Vigil Hub service is not running."
    service app-hub status
    exit 1
  fi

else
  # Original systemd service installation code
  printf "Creating the systemd service for the Vigil Hub...\n"
  cat >/etc/systemd/system/app-hub.service <<EOF
[Unit]
Description=Vigil Hub Service
After=network.target

[Service]
ExecStart=$BIN_PATH serve --http "0.0.0.0:$PORT"
WorkingDirectory=$HUB_DIR
User=app
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

  # Load and start the service
  printf "Loading and starting the Vigil Hub service...\n"
  systemctl daemon-reload
  systemctl enable --quiet app-hub.service
  systemctl start --quiet app-hub.service

  # Wait for the service to start or fail
  sleep 2

  # Check if the service is running
  if [ "$(systemctl is-active app-hub.service)" != "active" ]; then
    echo "Error: The Vigil Hub service is not running."
    echo "$(systemctl status app-hub.service)"
    exit 1
  fi

  # Enable auto-update if flag is set to true
  if [ "$AUTO_UPDATE_FLAG" = "true" ]; then
    echo "Setting up daily automatic updates for app-hub..."

    # Create systemd service for the daily update
    cat >/etc/systemd/system/app-hub-update.service <<EOF
[Unit]
Description=Update app-hub if needed
Wants=app-hub.service

[Service]
Type=oneshot
ExecStart=$BIN_PATH update
EOF

    # Create systemd timer for the daily update
    cat >/etc/systemd/system/app-hub-update.timer <<EOF
[Unit]
Description=Run app-hub update daily

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=4h

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
    systemctl enable --now app-hub-update.timer

    printf "\nDaily updates have been enabled.\n"
  fi
fi

printf "\n\033[32mVigil Hub has been installed successfully! It is now accessible on port $PORT.\033[0m\n"
