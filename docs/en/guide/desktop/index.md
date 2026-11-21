# Web console and migration

The Wails desktop shell has been removed. Run the Yunqi service binary or Docker image and open its configured address in a browser. Set PROXY_ACCESS_KEY before starting; optionally set a separate ADMIN_ACCESS_KEY. Sign in, add and test upstream channels, then use **Service & setup** to generate client configuration.

## Existing desktop installations

Configuration JSON and database formats remain unchanged. Preserve the old installation and back up its environment, configuration and state directories before migration. Typical legacy locations are macOS `~/Library/Application Support/ccx-desktop`, Windows `%APPDATA%\ccx-desktop` (Store installations can differ), and Linux `${XDG_STATE_HOME:-$HOME/.local/state}/ccx`. Confirm the actual location in the previous installation.

Stop writes before copying state, or use a consistent SQLite backup including committed WAL data. Copy to a new empty location; never overwrite existing data. Start with the existing `--config`, `--statedir`, `--logdir` and `--backupdir` options. These options do not relocate `.env`; explicitly preserve the original environment and keys. Verify channel counts, model mappings, subscriptions and historical statistics before retiring the old installation. Do not run both versions against the same database.

## Operations

Use Docker Compose, systemd, launchd or a Windows service manager for lifecycle management. Channel settings support hot reload; environment changes require an administrator-managed restart. The console shows runtime status, health diagnostics and a key-free diagnostic export. Read service logs on the deployment host. Client configuration is copied or downloaded and merged manually; the browser does not edit client-machine files.

Release binaries retain the existing asset names and SHA256 sidecars. Service binaries retain macOS codesign and Windows SignPath signing; desktop installers and in-app updates are removed. Follow your organization's software distribution policy.
