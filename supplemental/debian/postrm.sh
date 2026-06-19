#!/bin/sh
set -e

if [ "$1" = "purge" ]; then
	if [ -f /usr/share/debconf/confmodule ]; then
		. /usr/share/debconf/confmodule
		db_purge
	fi

	# Remove configuration and persisted agent state (fingerprint, etc.).
	rm -f /etc/vigil-agent.conf
	rm -rf /var/lib/vigil-agent

	# Remove the dedicated system user/group created in postinst. Kept tolerant
	# (|| true) so a partially-removed package still purges cleanly.
	if command -v deluser >/dev/null 2>&1; then
		deluser --quiet --system vigil >/dev/null 2>&1 || true
	fi
	if command -v delgroup >/dev/null 2>&1; then
		delgroup --quiet --system --only-if-empty vigil >/dev/null 2>&1 || true
	fi
fi
