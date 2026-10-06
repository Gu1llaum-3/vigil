#!/bin/sh
set -e

[ "$1" = "configure" ] || exit 0

CONFIG_FILE=/etc/vigil-agent.conf
STATE_DIR=/var/lib/vigil-agent
SERVICE=vigil-agent
# Dedicated account: not "vigil", which is the native hub's user (install-hub.sh)
# and owns the hub's data.
SERVICE_USER=vigil-agent
# Packages up to 0.2.x ran the agent as "vigil".
LEGACY_USER=vigil

. /usr/share/debconf/confmodule

in_group() {
	id -nG "$1" 2>/dev/null | tr ' ' '\n' | grep -qx "$2"
}

owned_by() {
	[ -e "$1" ] && [ "$(stat -c %U "$1")" = "$2" ]
}

# An install made by an older package left its config and state owned by the
# legacy user. Detect it before anything is changed.
LEGACY_INSTALL=false
if id -u "$LEGACY_USER" >/dev/null 2>&1; then
	if owned_by "$CONFIG_FILE" "$LEGACY_USER" || owned_by "$STATE_DIR" "$LEGACY_USER"; then
		LEGACY_INSTALL=true
	fi
fi

# Create group and user
if ! getent group "$SERVICE_USER" >/dev/null; then
	echo "Creating $SERVICE_USER group"
	addgroup --quiet --system "$SERVICE_USER"
fi

if ! getent passwd "$SERVICE_USER" >/dev/null; then
	echo "Creating $SERVICE_USER user"
	adduser --quiet --system "$SERVICE_USER" \
		--ingroup "$SERVICE_USER" \
		--no-create-home \
		--home /nonexistent \
		--gecos "System user for $SERVICE"
fi

if [ "$LEGACY_INSTALL" = true ]; then
	echo "Migrating $SERVICE from the $LEGACY_USER user to $SERVICE_USER"
	# Keep the persisted fingerprint, so the hub sees the same host.
	if [ -d "$STATE_DIR" ]; then
		chown -R "$SERVICE_USER":"$SERVICE_USER" "$STATE_DIR"
	fi
	# Keep Docker monitoring working: older packages put the legacy user in the
	# docker group (0.1.x unconditionally), so carry that over, unless the admin
	# explicitly answered the question (an answer is authoritative).
	if in_group "$LEGACY_USER" docker; then
		db_fget vigil-agent/docker_access seen || RET=false
		if [ "$RET" != "true" ]; then
			db_set vigil-agent/docker_access true
			db_fset vigil-agent/docker_access seen true
		fi
	fi
fi

# Docker socket access is opt-in: membership in the docker group is equivalent
# to root and bypasses the service sandboxing. The debconf answer is
# authoritative, so `dpkg-reconfigure vigil-agent` grants or revokes it.
db_get vigil-agent/docker_access || RET=false
if [ "$RET" = "true" ]; then
	if getent group docker >/dev/null 2>&1; then
		if ! in_group "$SERVICE_USER" docker; then
			echo "Adding $SERVICE_USER to docker group (grants Docker socket access)"
			usermod -aG docker "$SERVICE_USER"
		fi
	else
		echo "Docker monitoring requested but no 'docker' group exists; skipping."
	fi
elif in_group "$SERVICE_USER" docker; then
	echo "Removing $SERVICE_USER from docker group (Docker monitoring disabled)"
	gpasswd -d "$SERVICE_USER" docker >/dev/null
fi

# The config holds the token and is read by systemd as root (EnvironmentFile),
# so the agent itself never needs to read or rewrite it.
if [ ! -f "$CONFIG_FILE" ]; then
	touch "$CONFIG_FILE"
fi
chown root:root "$CONFIG_FILE"
chmod 0600 "$CONFIG_FILE"

# Append a KEY=value line to the config only if that key is not already present,
# so reconfigure/upgrade never clobbers manually edited values. The config file
# is a systemd EnvironmentFile (read by systemd, never shell-evaluated).
add_config_value() {
	_k="$1"
	_v="$2"
	[ -n "$_v" ] || return 0
	grep -q "^${_k}=" "$CONFIG_FILE" && return 0
	printf '%s=%s\n' "$_k" "$_v" >> "$CONFIG_FILE"
}

db_get vigil-agent/key || RET=""
add_config_value KEY "$RET"
db_get vigil-agent/hub_url || RET=""
add_config_value HUB_URL "$RET"
db_get vigil-agent/token || RET=""
add_config_value TOKEN "$RET"

deb-systemd-helper enable "$SERVICE".service
systemctl daemon-reload

# Only start automatically once a hub URL and the hub public key are configured;
# without the URL the agent has nothing to connect to, and without the key it
# exits at startup (and systemd would keep restarting it).
if grep -q "^HUB_URL=." "$CONFIG_FILE" && grep -q "^KEY=." "$CONFIG_FILE"; then
	deb-systemd-invoke start "$SERVICE".service || echo "could not start $SERVICE.service!"
else
	echo "HUB_URL or KEY is not set in $CONFIG_FILE; not starting $SERVICE.service yet."
	echo "Set HUB_URL, KEY and TOKEN there, then run: systemctl start $SERVICE.service"
fi

if [ "$LEGACY_INSTALL" = true ]; then
	echo
	echo "Note: the $LEGACY_USER user is no longer used by $SERVICE and was left untouched"
	echo "(groups: $(id -nG "$LEGACY_USER" 2>/dev/null)). If the Vigil hub is not installed"
	echo "natively on this host, it is unused and can be removed with: deluser --system $LEGACY_USER"
	if in_group "$LEGACY_USER" docker; then
		echo "Warning: $LEGACY_USER is still in the docker group, which is root-equivalent; the hub"
		echo "does not need it. Remove it with: gpasswd -d $LEGACY_USER docker"
	fi
fi
