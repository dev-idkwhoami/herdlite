# Herdlite PHP Extension

Tiny PHP extension used as an early bootstrap hook for Herdlite-managed PHP builds.

Design rules:

- Keep the extension minimal.
- Do not put Laravel-specific logic in C.
- Load a configured PHP bootstrap file when enabled.
- Build one extension target per PHP minor/ABI as needed.

The Laravel/debug behavior belongs in `packages/php-runtime`.

## Local Build Probe

Build against a Herdlite-managed PHP version:

```sh
phpize
./configure --with-php-config="$HOME/.local/share/herdlite/php/8.5.6/bin/php-config" --enable-herdlite
make
```

Load it by writing a small scanned ini file instead of editing the main `php.ini`:

```ini
extension=/absolute/path/to/herdlite.so
```
