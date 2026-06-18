#!/bin/sh
set -e

. /usr/share/debconf/confmodule
db_version 2.0

db_input high vigil-agent/hub_url || true
db_input high vigil-agent/token || true
db_input high vigil-agent/key || true
db_input medium vigil-agent/docker_access || true
db_go
