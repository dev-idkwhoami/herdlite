# Herdlite

Herdlite is a personal Laravel development runtime for Linux, currently aimed at CachyOS and other Arch-like systems.

The goal is to provide the parts of Laravel Herd that are useful on a daily workstation: local `.test` domains, HTTPS, PHP version management, Nginx/PHP-FPM hosting, PostgreSQL, Composer, shell shims, mail capture, logs, and early debug tooling.

This is not designed as a broad compatibility product. It is intentionally optimized for one developer machine and one preferred stack.

## What It Does

Herdlite currently manages:

- Herdlite-built PHP runtimes installed under the user data directory
- PHP-FPM config generation per installed PHP runtime
- Nginx config generation for Laravel projects
- `.test` wildcard DNS setup
- local CA and site certificates
- project-aware shell shims for `php`, `composer`, `node`, `npm`, and `npx`
- native PostgreSQL data directory and lifecycle
- per-project PostgreSQL database creation on link
- Laravel `.env` updates for mail and database settings
- local SMTP mail catching
- browser mail viewer with attachment serving
- daemon log viewer
- experimental Symfony VarDumper capture via PHP `auto_prepend_file`
- GitHub release packaging for prebuilt CLI binaries

> [!IMPORTANT]
> This project is developed with AI assistance. Treat the code as personal-tool quality, review changes before running privileged commands, and do not assume the defaults are appropriate for shared, production, or security-sensitive systems.

## Platform

Primary target:

```text
Linux + Arch/CachyOS-style system packages
```

The installer expects `pacman` for system dependencies. Other distributions are not a current goal.

## Install

The root `install.sh` is only a bootstrapper. It downloads a prebuilt Herdlite binary from GitHub Releases and places it in `~/.local/share/herdlite/bin` and adds one managed zsh source line.

Install from the default GitHub repository:

```sh
curl -fsSL https://raw.githubusercontent.com/dev-idkwhoami/herdlite/master/install.sh | sh
```

After the binary is installed, run the actual system setup:

```sh
herdlite install
```

`herdlite install` performs the heavy work:

- installs required official packages through `pacman`
- initializes Herdlite user directories
- creates/trusts the local development CA
- configures `.test` DNS
- prepares Nginx
- builds the default PHP runtime
- installs Composer
- initializes PostgreSQL
- writes shell integration

## Build From Source

Development requirements:

- Go 1.22+
- C compiler/toolchain
- SQLite development headers
- `pkg-config`

Build:

```sh
make build
```

Run tests:

```sh
make test
```

Run vet:

```sh
make vet
```

Create a local release archive:

```sh
VERSION=v0.1.0 make release
```

Release output is written to `dist/`.

## Common Usage

Install Herdlite runtime pieces:

```sh
herdlite install
```

### PHP extensions

`herdlite php install <version>` also installs the extensions listed in
`~/.config/herdlite/extensions.toml` (or `$XDG_CONFIG_HOME/herdlite/extensions.toml`).
Each exact PHP version has its own section:

```toml
[php."8.5.6"]
[[php."8.5.6".extensions]]
name = "grpc"
package = "grpc/grpc-php-ext"
version = "1.84.0"

[php."8.5.11"]
[[php."8.5.11".extensions]]
name = "grpc"
package = "grpc/grpc-php-ext"
version = "1.84.0"
```

Add extension blocks under the desired PHP version. `name` is the PHP module name
used to verify loading; `package` is its PIE package name. `version` accepts an
exact extension version or a Composer-style constraint. Different PHP sections
can pin different versions of the same extension. Unknown keys and duplicates
within one PHP section are rejected.

Missing sections inherit from the newest installed runtime in the same minor
series. A new minor series starts empty. Existing sections are never overwritten;
use `extensions = []` inside a PHP section to explicitly configure no extensions.
Removing an entry does not uninstall an existing extension. An entirely empty
manifest initializes empty sections for installed runtimes.

The old shared `[[extensions]]` format is migrated automatically on open or sync:
each installed runtime receives its own copy, and the original file is backed up
as `extensions.toml.legacy-*.bak`. Dry runs preview the configuration without
writing or migrating it. Normal additions preserve existing sections and comments;
the one-time legacy conversion rewrites the TOML structure.

Open the manifest in the desktop's default application for TOML files, or list
the extensions actually loaded by every installed PHP runtime:

```sh
herdlite php extensions open
herdlite php extensions list
```

`open` creates the per-version file if it is missing. `list` reads each PHP CLI runtime
and includes bundled, PIE-installed, and manually enabled extensions, independent
of the manifest. Disabled extensions and already-running FPM processes are not
included in this view.

Apply the configuration to an already installed runtime without rebuilding PHP:

```sh
herdlite php extensions sync 8.5 --dry-run
herdlite php extensions sync 8.5
```

The install command also syncs extensions when it reuses an existing valid PHP
runtime. PIE handles already installed versions; a failed sync can be retried.
Restart running PHP-FPM and queue workers after changing extensions.

Herdlite downloads the official PIE 1.5.0 PHAR and verifies its pinned SHA-256
before execution. PIE builds against the selected runtime's `php-config` and
`phpize`, enables extensions in its `etc/conf.d`, and keeps its records under
Herdlite's data directory. PIE requires PHP 8.1+ to run. Build tools and any
extension-specific system libraries must already be installed; the automatic sync
does not install system packages interactively. A module-load failure fails the sync.

Link the current Laravel project:

```sh
herdlite link
```

Link a specific project:

```sh
herdlite link --p /path/to/project
```

Link with a selected PHP version:

```sh
herdlite link --p /path/to/project --php 8.5
```

Override the detected project name or domain:

```sh
herdlite link --p /path/to/project --n customer-portal --d customer.test
```

Enable a websocket subdomain for a project:

```sh
herdlite link --p /path/to/project --ws --ws-port 8080
```

Start hosting:

```sh
herdlite start
```

Open a linked project:

```sh
herdlite open
```

Open by path/name/domain:

```sh
herdlite open --p /path/to/project
herdlite open --p my-app
herdlite open --p my-app.test
```

Unlink a project:

```sh
herdlite unlink --p my-app
```

Remove unused Herdlite-managed site certificates and recorded project databases without touching project folders:

```sh
herdlite purge --dry-run
herdlite purge
```

## PHP Management

List installed PHP runtimes:

```sh
herdlite php list
```

Install a PHP runtime:

```sh
herdlite php install 8.5
```

Update installed minor versions to their latest stable patch:

```sh
herdlite php update --dry-run  # Show exact version mappings, pins, and locked skips
herdlite php update 8.5       # Update only the installed 8.5 series
herdlite php update           # Update each installed minor sequentially
herdlite php lock 8.5.11      # Keep this exact installed patch
herdlite php unlock 8.5.11
```

Locks persist and appear in `php list`. Locked patches, their configuration, and
project pins are excluded from updates, including `--force`. Other unlocked
patches in the same minor can still update alongside them. If the latest patch
is itself locked, it can be reused without rebuilding or changing its files.

The updater previews the replacements and asks for confirmation in a terminal.
It then stops running nginx, the Herdlite daemon, PHP-FPM, and managed queue
workers. It builds and validates each replacement, preserves PHP configuration
and configured extensions, migrates affected project/global pins, and updates
site configuration. Conflicting configurations within a minor must be resolved
or kept separate with locks before updating. PostgreSQL stays running.

Only services and workers that were running beforehand are restarted. Old
runtime directories remain available until restoration succeeds. On a failed
minor, the previous installation is recovered and you can choose whether to
continue with the remaining minors; the command still exits nonzero. Ctrl+C
also restores service state. If service restoration fails, the updater attempts
to roll back the completed replacements and reports any recovery failures.

Already-current versions are skipped unless `--force` is supplied. Older
unlocked patches are consolidated into an already-installed latest patch.
`--dry-run` makes no runtime or service changes; `--verbose` streams build
output, and `--keep-build` retains successful build sources. Build logs have
separate filenames so continuing after a failure does not overwrite its log.

Remove an exact installed patch:

```sh
herdlite php remove 8.5.6
```

The command lists affected projects and the other installed PHP versions, then
asks for one replacement. That version is applied to every affected project and
the global PHP selection if it uses the removed patch. Enter a listed number or
exact version, or `q` to cancel. The replacement may belong to another minor.
Locked patches must be unlocked before removal. An in-use runtime cannot be
removed without an installed replacement.

After validating replacements and confirming the final mapping, removal pauses
hosting and managed PHP workers, migrates references and site configurations,
and restores the previous running state. The old directory is deleted only
after restoration succeeds; failures attempt rollback. Existing replacement
configurations are used unchanged, and the removed version's extension manifest
section is retained for future reinstalls. This command requires a terminal.

Pin the current project to a PHP runtime:

```sh
herdlite php use 8.5
```

Set the global fallback PHP runtime used outside linked projects:

```sh
herdlite php global 8.5
```

## PostgreSQL

Initialize PostgreSQL:

```sh
herdlite postgres init
```

Start, stop, and inspect it:

```sh
herdlite postgres start
herdlite postgres status
herdlite postgres logs
herdlite postgres stop
```

For this personal development setup, Herdlite creates a passwordless PostgreSQL superuser role named `root`.

## Mail

Herdlite includes a local SMTP catcher. Linked Laravel projects are configured to send mail to it.

List captured mail:

```sh
herdlite mail list
```

Show a message:

```sh
herdlite mail show <id>
```

Open the browser mail viewer:

```sh
herdlite mail open <id>
```

Clear captured mail:

```sh
herdlite mail clear
```

## Debug UI

Open the debug UI:

```sh
herdlite debug
```

The debug UI includes mail, dumps, logs with project/source filters, and per-project Laravel queue worker controls.

Install or remove a desktop launcher for the debug UI:

```sh
herdlite app install
herdlite app remove
```

Dump capture is experimental. It uses a Herdlite-managed PHP prepend file and Symfony VarDumper when the project already provides Symfony VarDumper through its normal dependencies.

## Repair And Uninstall

Repair system integration:

```sh
herdlite repair
```

Uninstall Herdlite integration:

```sh
herdlite uninstall
```

The uninstall path is intentionally conservative. It removes only what Herdlite can identify safely and avoids destructive edits to unrelated system config.

## Filesystem Layout

Herdlite uses XDG-style user paths:

```text
~/.config/herdlite
~/.local/share/herdlite
~/.cache/herdlite
```

Generated service templates are embedded in the Go binary from package-local template directories:

```text
internal/nginx/templates
internal/phpmanager/templates
```

## Updating Herdlite and its components

```sh
herdlite update --dry-run       # Preview all installed components
herdlite update                 # Update installed components sequentially
herdlite upgrade                # Alias for update
herdlite nginx update            # nginx and required package dependencies
herdlite postgres update         # PostgreSQL packages and installed pgvector
herdlite dns update              # dnsmasq
herdlite composer update         # Latest stable Composer
herdlite node update             # Latest release in each installed Node major
herdlite self update             # Latest published Herdlite release
herdlite system update           # Installed Herdlite build/system dependencies
herdlite update nginx --dry-run  # Same as nginx update --dry-run
```

`update` and `upgrade` are interchangeable, including component subcommands.
The general command runs system dependencies, PHP, Composer, Node, and finally
Herdlite itself. Missing components are skipped. Updates ask for confirmation;
package managers also show their transaction prompts. A failed system package
step stops the batch; other failures are reported with a nonzero exit status.

System updates target only installed Herdlite packages and dependencies required
by pacman. They use `pacman -S --needed` against the existing repository database:
**no database refresh and no full system upgrade**. A dry run resolves and prints
the package transaction without installing anything. Available versions depend
on the age of that database; this is not a fresh upstream update check. Arch does
not guarantee compatibility for partial upgrades, and a selected package may
require wider system maintenance. Herdlite does not perform that maintenance
automatically. Installed pgvector is updated through `yay -S --needed pgvector`;
its AUR build/dependency transaction is shown by yay when applying the update.

Package updates preserve the running state of Herdlite hosting, workers and the
daemon; PostgreSQL is stopped/restored when its packages or pgvector are involved.
The package plan blocks a PostgreSQL major change against the managed cluster's
`PG_VERSION`; database-format migration and backups remain a separate operation.
nginx's low-port capability is reapplied after replacement. Updated dnsmasq is
activated by reloading an active NetworkManager. Package-manager changes are not
rolled back by Herdlite if a later step fails; restoration errors are reported.

PHP updates reuse the existing minor-series updater, including locks, extension
configuration and project-pin migration. Use `herdlite php update --help` for PHP
options such as `--force` and `--keep-build`. If system library changes require a
PHP rebuild, use `herdlite php update --force` after resolving library compatibility.
Node updates use nvm, retain previous installations and exact `.nvmrc`/global pins,
and migrate global packages from the newest installed patch in each major. npm
and npx come with the new Node runtime. nvm itself remains externally managed.
Composer is downloaded to a temporary file, validated using managed PHP, then
atomically replaces the PHAR.

Self-update includes the daemon, debug UI, mail capture and queue-management code.
It replaces the **currently running executable** with the latest published release
from `dev-idkwhoami/herdlite` (or `HERDLITE_REPO`). The release archive must have a
matching SHA-256 checksum and pass a version check. Only a newer stable version
can replace an installed release; older releases are rejected. Local/development
builds (`dev`, branch names, or other non-release versions) are preserved. To
switch from a development build to releases, explicitly install a published
release using `install.sh --version <tag>`. The old executable is retained
until a previously running daemon restarts successfully; on restart failure the
updater attempts rollback. A stopped daemon stays stopped. Development checkouts
can still use the local build/deployment procedure below. Self, Composer and Node
dry runs describe their actions without downloading replacements.

## Release Flow

For a local workstation update, build the current checkout with `make build`.
Back up `~/.local/share/herdlite/bin/herdlite`, copy `bin/herdlite` to a temporary
file beside the installed binary, then rename it to `herdlite`. This avoids
overwriting an executable in use. Verify with `herdlite version` and command help.
If the Herdlite daemon is running, stop it before replacing the binary and start
it afterward. No GitHub tag or full `herdlite install` is needed for a local update.

PHP upgrades are separate: `herdlite php install 8.5.11` installs that runtime and
its configured extensions alongside existing versions. Project pins remain
unchanged until `herdlite php use 8.5.11 --p <project>` is run, followed by
`herdlite start` to apply the hosting configuration.

GitHub Actions builds release artifacts when a tag beginning with `v` is pushed:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Current release artifacts:

```text
herdlite-linux-amd64.tar.gz
herdlite-linux-amd64.tar.gz.sha256
```

## Development Notes

This project is still in active development. SQLite migrations are intentionally not implemented yet. During development, deleting the local Herdlite database is acceptable when the schema changes.

Planning notes are intentionally not tracked in git. Keep local architecture notes under `docs/` if needed.

## TODO

1. Query listening.
2. Optional services such as RustFS, Meilisearch, and Redis.
3. Additional database systems, including MySQL and MariaDB.
