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

## PHP Management

List installed PHP runtimes:

```sh
herdlite php list
```

Install a PHP runtime:

```sh
herdlite php install 8.5
```

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

## Logs And Dumps

Open the daemon log viewer:

```sh
herdlite logs open
```

Open the experimental dump viewer:

```sh
herdlite dumps open
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

## Release Flow

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
