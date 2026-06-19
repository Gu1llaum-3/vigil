#!/bin/sh
set -e

[ "$1" = "configure" ] || exit 0

CONFIG_FILE=/etc/vigil-agent.conf
SERVICE=vigil-agent
SERVICE_USER=vigil

. /usr/share/debconf/confmodule

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

# Docker socket access is opt-in: membership in the docker group is equivalent
# to root and bypasses the service sandboxing, so only add it when requested.
db_get vigil-agent/docker_access || RET=false
if [ "$RET" = "true" ]; then
	if getent group docker >/dev/null 2>&1; then
		if ! id -nG "$SERVICE_USER" 2>/dev/null | tr ' ' '\n' | grep -qx docker; then
			echo "Adding $SERVICE_USER to docker group (grants Docker socket access)"
			usermod -aG docker "$SERVICE_USER"
		fi
	else
		echo "Docker monitoring requested but no 'docker' group exists; skipping."
	fi
fi

# Create config file if it doesn't already exist
if [ ! -f "$CONFIG_FILE" ]; then
	touch "$CONFIG_FILE"
	chmod 0600 "$CONFIG_FILE"
	chown "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_FILE"
fi;

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

# Only start automatically once a hub URL is configured; without it the agent
# has nothing to connect to and would just idle (and log warnings).
if grep -q "^HUB_URL=." "$CONFIG_FILE"; then
	deb-systemd-invoke start "$SERVICE".service || echo "could not start $SERVICE.service!"
else
	echo "HUB_URL is not set in $CONFIG_FILE; not starting $SERVICE.service yet."
	echo "Set HUB_URL (and TOKEN) there, then run: systemctl start $SERVICE.service"
fi
