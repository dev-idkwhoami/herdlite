<?php

declare(strict_types=1);

function env_values(string $path): array
{
    if (!is_file($path)) {
        return [];
    }

    $values = [];
    foreach (file($path, FILE_IGNORE_NEW_LINES) ?: [] as $line) {
        $line = trim($line);
        if ($line === '' || str_starts_with($line, '#') || !str_contains($line, '=')) {
            continue;
        }
        [$key, $value] = explode('=', $line, 2);
        $values[trim($key)] = trim($value, "\"'");
    }
    return $values;
}

function db_status(array $env): string
{
    if (($env['DB_CONNECTION'] ?? '') !== 'pgsql') {
        return 'DB_CONNECTION is not pgsql yet.';
    }

    $dsn = sprintf(
        'pgsql:host=%s;port=%s;dbname=%s',
        $env['DB_HOST'] ?? '127.0.0.1',
        $env['DB_PORT'] ?? '5432',
        $env['DB_DATABASE'] ?? ''
    );

    try {
        $pdo = new PDO($dsn, $env['DB_USERNAME'] ?? 'root', $env['DB_PASSWORD'] ?? '');
        $row = $pdo->query('select current_database() as db, current_user as user')->fetch(PDO::FETCH_ASSOC);
        return sprintf('Connected to %s as %s.', $row['db'] ?? '?', $row['user'] ?? '?');
    } catch (Throwable $e) {
        return 'Connection failed: ' . $e->getMessage();
    }
}

$env = env_values(dirname(__DIR__) . '/.env');
$checks = [
    'PHP version' => PHP_VERSION,
    'HTTPS' => (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') ? 'yes' : 'no',
    'Host' => $_SERVER['HTTP_HOST'] ?? 'unknown',
    'Document root' => $_SERVER['DOCUMENT_ROOT'] ?? 'unknown',
    'DB name' => $env['DB_DATABASE'] ?? 'not set',
    'DB user' => $env['DB_USERNAME'] ?? 'not set',
    'PostgreSQL' => db_status($env),
];

?><!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Herdlite Smoke Test</title>
    <style>
        body {
            margin: 0;
            font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            background: #f5f7f8;
            color: #172026;
        }
        main {
            max-width: 760px;
            margin: 8vh auto;
            padding: 0 24px;
        }
        h1 {
            font-size: 32px;
            margin: 0 0 8px;
        }
        p {
            margin: 0 0 24px;
            color: #52616b;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            background: white;
            border: 1px solid #d8e0e5;
        }
        th, td {
            text-align: left;
            padding: 12px 14px;
            border-bottom: 1px solid #e8edf0;
            vertical-align: top;
        }
        th {
            width: 180px;
            color: #52616b;
            font-weight: 600;
        }
        tr:last-child th, tr:last-child td {
            border-bottom: 0;
        }
        code {
            font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
            font-size: 13px;
        }
    </style>
</head>
<body>
    <main>
        <h1>Herdlite Smoke Test</h1>
        <p>If you can see this over HTTPS, nginx, SSL, and PHP-FPM are working.</p>
        <table>
            <?php foreach ($checks as $label => $value): ?>
                <tr>
                    <th><?= htmlspecialchars($label, ENT_QUOTES, 'UTF-8') ?></th>
                    <td><code><?= htmlspecialchars($value, ENT_QUOTES, 'UTF-8') ?></code></td>
                </tr>
            <?php endforeach; ?>
        </table>
    </main>
</body>
</html>
