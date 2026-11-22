# Complete state recovery

Build the offline tool with `go build -o yunqi-state ./cmd/yunqi-state` from `backend-go/`.

```sh
./yunqi-state backup --offline --config /srv/yunqi/config/config.json --statedir /srv/yunqi/state --secrets-file /srv/yunqi/service.env --output /srv/backups/yunqi-snapshot
./yunqi-state verify --bundle /srv/backups/yunqi-snapshot
./yunqi-state restore --bundle /srv/backups/yunqi-snapshot --output /srv/recovery/yunqi-snapshot
```

The tool never stops, starts or reloads a service. All writers must already be stopped. Updated service instances and the tool share exclusive OS locks; older binaries and external writers do not participate. `--offline` confirms the operator has quiesced them. Use storage-level consistent snapshots when zero-downtime backups are required.

Bundles include the specified configuration, its sibling `entry-budget.db`, and regular files in the state directory, including metrics, autopilot, caches, session/recovery state and preset price-review history. An explicitly supplied secret environment file is included verbatim as `secrets.env`; injected environment variables and external secret stores are not discovered. Logs, old backups, locks and raw SQLite WAL/SHM files are excluded. SQLite snapshots use `VACUUM INTO` and integrity checks so committed WAL data is preserved.

Both backup and restore require a new output directory. Backups cannot be inside their sources. Restore verifies exact inventory, safe paths, SHA-256, sizes and SQLite integrity before creating a destination. It rejects symlinks, special files, extra files, duplicate paths and corrupt bundles. Limits are 10,000 files and 16 GiB. Files use 0600 and directories 0700. Checksums detect corruption; they are not signatures. Protect backups as credentials.

Recovered startup paths are `--config OUTPUT/config/config.json --statedir OUTPUT/state`. Restore never activates credentials or overwrites a deployment. Rehearse in isolation and verify budget spent/reserved amounts, cost history, approved prices, roles and revocations before an operator switches traffic. Incomplete recovery markers prevent the service opening partial state.

Redis, IdP and other external services are not included. Quiesce all nodes and take coordinated external snapshots; `--external-snapshot ID` records an operator-supplied reference only. Restore matching epochs and complete external state; never clear or reinitialize namespaces to bypass a mismatch. Older revocation snapshots can resurrect sessions revoked since that snapshot: when completeness cannot be proven, rotate the identity/session trust domain and require fresh login. Separate local ledgers are not a shared team budget. Retain the snapshot references, software version and rehearsal results.
