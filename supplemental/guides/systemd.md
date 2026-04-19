# Installing as a Linux systemd service

This is useful if you want to run the hub or agent in the background continuously, including after a reboot.

## Install script (recommended)

There are two scripts, one for the hub and one for the agent. You can run either one, or both.

The install script creates a dedicated user for the service (`app`), downloads the latest release, and installs the service.

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

```bash
./install-hub.sh
```

#### Uninstall

```bash
./install-hub.sh -u
```

#### Update

```bash
sudo /opt/vigil/vigil update && sudo systemctl restart app-hub
```

### Agent

Download the script:

```bash
curl -sL https://raw.githubusercontent.com/Gu1llaum-3/vigil/main/supplemental/scripts/install-agent.sh -o install-agent.sh && chmod +x install-agent.sh
```

#### Install

The agent install script is currently intended for Linux release targets published by `.goreleaser.yml`: `amd64`, `arm64`, and `arm` (`armv7`).

You may optionally include the hub public key, token, and hub URL as arguments. Run `./install-agent.sh -h` for more info.

If you want to test a beta or another pre-release, pass it explicitly with `--version` because GitHub's `latest` endpoint only returns stable releases.

If specifying your key with `-k`, please make sure to enclose it in quotes.

```bash
./install-agent.sh
```

Example for a beta:

```bash
./install-agent.sh --version v0.1.0-beta.5
```

#### Uninstall

```bash
./install-agent.sh -u
```

#### Update

`vigil-agent update` is not available yet.

To upgrade an existing agent installation, re-run the install script and optionally pin a release version:

```bash
./install-agent.sh --version v0.1.0
```

## Manual install

### Hub

1. Create the system service at `/etc/systemd/system/app.service`

```bash
[Unit]
Description=App Hub Service
After=network.target

[Service]
# update the values in the curly braces below (remove the braces)
ExecStart={/path/to/working/directory}/app serve
WorkingDirectory={/path/to/working/directory}
User={YOUR_USERNAME}
Restart=always

[Install]
WantedBy=multi-user.target
```

2. Start and enable the service to let it run after system boot

```bash
sudo systemctl daemon-reload
sudo systemctl enable app.service
sudo systemctl start app.service
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
