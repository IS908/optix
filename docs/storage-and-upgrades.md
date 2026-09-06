# Persistent storage and migration

All CLI commands, the server and the Skill resolve the database in this order:

1. Explicit `--db` (empty is an error).
2. `OPTIX_DB_PATH`.
3. YAML `database.path` (existing values remain supported).
4. `$XDG_DATA_HOME/optix/optix.db` when XDG_DATA_HOME is absolute; otherwise
   macOS `~/Library/Application Support/optix/optix.db`, Linux `~/.local/share/optix/optix.db`.

Relative explicit paths retain their existing working-directory semantics. The
Skill runs in its runtime directory, so use absolute overrides for shared data.
The shipped YAML leaves database.path empty. Tests should pass an isolated `--db`.

`optix data status` shows the resolved path, its source, binary version, physical
runtime and a legacy-path warning without opening or creating a database.

Without an explicit selection, an existing `data/optix.db` in the working
folder or beside the executable's `bin/` directory stops normal commands. This
also applies if both old and new databases exist: select deliberately rather
than silently choosing possibly divergent records.

## Migrate

Stop the server, scheduled jobs and other writers before the final cutover.
Use the old database path reported by `data status`, then:

```sh
optix data migrate --from /absolute/old/data/optix.db --to /absolute/user/data/optix.db
optix --db /absolute/user/data/optix.db watch list
export OPTIX_DB_PATH=/absolute/user/data/optix.db
```

Migration exports a consistent SQLite `VACUUM INTO` snapshot, including committed
WAL records, validates `integrity_check`, and creates a checksum-identical copy.
The destination is published atomically without overwrite. An existing target
(including a symlink) or SQLite sidecar is an error. The source remains untouched;
a separate recovery snapshot is kept in the private `.optix-migration-*` directory
next to the destination, and its absolute path is printed on success. Copies
have mode 0600. Failure before publication cleans temporary artifacts. Interruption
by SIGKILL may leave a private staging directory, but cannot replace an old target.

The snapshot permits concurrent source activity without corrupting the backup,
but does not replicate commits after its snapshot. Stop all writers and repeat
to a *new* target if they were active during the final migration. Configuration
is never switched automatically. Repeated migration to the same target refuses
overwrite. Keep the old database and snapshot until business records have been
checked. To recover, stop writers and select the original source, or migrate the
printed backup to a new destination; do not overwrite a live SQLite file.

## Install, upgrade and roll back the Skill

`install.sh --agent claude` now installs an independent runtime even when run
from a source checkout. A source install compiles that checkout; a release install
copies the bundled version. Use a published tarball for a fixed release, or
`install.sh --dev --agent claude` to explicitly link a built checkout. Moving the
source after a standalone installation does not affect the installed runtime.

Each standalone install owns its Python venv and engine under
`~/.agents/skills/optix/.runtimes/runtime-*`. Validation happens before activation;
`.runtime` is replaced atomically and `.previous-runtime` retains the previous
version. Venv directories are never relocated. To switch back between compatible
standalone versions:

```sh
bash ~/.agents/skills/optix/install.sh --rollback
```

Rollback refuses legacy versions and dev targets that lack the storage-layout
marker: old orchestration scripts can force a runtime-local DB and old venvs are
not relocatable. Reinstall a compatible version explicitly instead. An existing
legacy real `.runtime` is retained as an archive when first converted to the new
layout; this first conversion requires a directory move before publishing the
symlink. Stop old processes before this one-time conversion. Subsequent version
switches use atomic symlink replacement.

If a source/old runtime has a database, a layout switch refuses to proceed until
you migrate and explicitly supply an absolute `OPTIX_DB_PATH` outside both the
old runtime and the canonical skill directory. This does not auto-migrate or
validate your business records. Retained old locations are propagated into the
new runtime's `legacy-databases` file, so a future process without your environment
override fails visibly instead of silently creating an empty database. Keep the
chosen database override in the environment/config used by your agent or scheduler.

`--uninstall` removes agent symlinks and preserves the bundle. Real legacy agent
directories are refused rather than recursively deleted. `--uninstall --purge`
refuses a bundle with possible database files or SQLite headers, including custom
filenames. External user data is preserved. Review and archive old data explicitly
before purging; runtime archives are not automatically garbage-collected.

An installation lock prevents simultaneous upgrades/uninstalls. An interrupted
installation may leave `.install-lock` or an unactivated runtime; verify no
installer is running before removing a stale lock. Failed preparation leaves the
active runtime unchanged. `optix data status` reports `dev` versus `standalone`.
