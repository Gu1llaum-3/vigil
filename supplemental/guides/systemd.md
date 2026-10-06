# Installing as a Linux systemd service

This is useful if you want to run the hub or agent in the background continuously, including after a reboot.

## Install script (recommended)

There are two scripts, one for the hub and one for the agent. You can run either one, or both.

The install scripts create a dedicated system user for the service (`vigil` for the hub, `vigil-agent` for the agent), download the release, and install the service.

If you need to edit the service -- for instance, to change an environment variable -- you can edit the file(s) in `/etc/systemd/system/`. Then reload the systemd daemon and restart the service.

> [!NOTE]
> You need system administrator privileges to run the install script. If you encounter a problem, please [open an issue](https://github.com/Gu1llaum-3/vigil/issues/new).

### Hub

Download the script:

```bash
curl -sL https://raw.githubusercontent.com/Gu1llaum-3/vigil/main/supplemental/scripts/install-hub.sh -o install-hub.sh && chmod +x install-hub.sh
```

#### Install

You may specify a port number with the `-p` flag. The default port is `8090`.

Without `-v`, the script installs the latest **stable** release. Pre-releases (such as `-beta` versions) must be passed explicitly with `-v`; pick the tag from the [releases page](https://github.com/Gu1llaum-3/vigil/releases).

```bash
./install-hub.sh
```

Example for a beta:

```bash
./install-hub.sh -v vX.Y.Z-beta
```

#### Uninstall

```bash
./install-hub.sh -u
```

#### Update

```bash
sudo /opt/vigil/vigil update && sudo systemctl restart vigil-hub
```

### Agent

Download the script:

```bash
curl -sL https://raw.githubusercontent.com/Gu1llaum-3/vigil/main/supplemental/scripts/install-agent.sh -o install-agent.sh && chmod +x install-agent.sh
```

#### Install

The easiest way to install an agent is the command copied from the hub's **Add agent** dialog (*Copy install script*, or *Copy install script with Docker monitoring* from its menu): it already contains the hub URL, public key and token, and it is pinned to the hub's own version, so the agent always matches the hub.

To run the script by hand instead: the agent install script is currently intended for Linux release targets published by `.goreleaser.yml`: `amd64`, `arm64`, and `arm` (`armv7`).

You may optionally include the hub public key, token, and hub URL as arguments. Run `./install-agent.sh -h` for more info.

Without `--version`, the script installs the latest **stable** release. To install a beta or another pre-release (for example to match a beta hub), pass it explicitly with `--version`, because GitHub's `latest` endpoint only returns stable releases. Use the same version as your hub.

If specifying your key with `-k`, please make sure to enclose it in quotes.

```bash
./install-agent.sh
```

Example for a beta:

```bash
./install-agent.sh --version vX.Y.Z-beta
```

The agent runs as the unprivileged `vigil-agent` user and does not get Docker access by default. To inventory and monitor Docker containers, add `--docker`: it puts `vigil-agent` in the `docker` group, which is equivalent to root access on the host. Upgrades keep the current setting; `--no-docker` revokes it.

```bash
./install-agent.sh --docker
```

#### Uninstall

```bash
./install-agent.sh -u
```

#### Update

`vigil-agent update` is not available yet.

To upgrade an existing agent installation, re-run the install script and pin the release version (usually your hub's version):

```bash
./install-agent.sh --version vX.Y.Z
```

## Manual install

### Hub

1. Create the system service at `/etc/systemd/system/vigil-hub.service`

```bash
[Unit]
Description=Vigil Hub Service
After=network.target

[Service]
# update the values in the curly braces below (remove the braces)
ExecStart={/path/to/working/directory}/vigil serve
WorkingDirectory={/path/to/working/directory}
User={YOUR_USERNAME}
Restart=always

[Install]
WantedBy=multi-user.target
```

2. Start and enable the service to let it run after system boot

```bash
sudo systemctl daemon-reload
sudo systemctl enable vigil-hub.service
sudo systemctl start vigil-hub.service
```

### Agent

1. Create the system service at `/etc/systemd/system/vigil-agent.service`

```bash
[Unit]
Description=App Agent Service
After=network.target

[Service]
# update the values in curly braces below (remove the braces)
Environment="KEY={PASTE_YOUR_KEY_HERE}"
ExecStart={/path/to/directory}/vigil-agent
User={YOUR_USERNAME}
Restart=always

[Install]
WantedBy=multi-user.target
```

2. Start and enable the service to let it run after system boot

```bash
sudo systemctl daemon-reload
sudo systemctl enable vigil-agent.service
sudo systemctl start vigil-agent.service
```
