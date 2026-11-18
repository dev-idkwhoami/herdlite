package updater

import (
	"fmt"
	"strings"

	"herdlite/internal/install"
)

// PackageTargets deliberately excludes a refresh or system-wide upgrade. pacman
// resolves dependencies from the administrator's existing sync databases.
func PackageTargets(component string) ([]string, error) {
	switch component {
	case "nginx":
		return []string{"nginx"}, nil
	case "postgres":
		return []string{"postgresql", "postgresql-libs"}, nil
	case "dns":
		return []string{"dnsmasq"}, nil
	case "system":
		return install.RequiredPackages(), nil
	default:
		return nil, fmt.Errorf("unknown package component: %s", component)
	}
}

// CheckPostgresTransaction examines dependencies too, before stopping services
// or replacing database binaries. Never automatically migrate database formats.
func CheckPostgresTransaction(plan, clusterMajor string) error {
	for _, line := range strings.Split(plan, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "postgresql" {
			continue
		}
		version := fields[1]
		if n := strings.IndexByte(version, ':'); n >= 0 {
			version = version[n+1:]
		}
		major := strings.Split(version, ".")[0]
		if strings.TrimSpace(clusterMajor) != "" && major != strings.TrimSpace(clusterMajor) {
			return fmt.Errorf("PostgreSQL %s requires migration of the existing PostgreSQL %s cluster; migrate/back up the database separately before updating", version, strings.TrimSpace(clusterMajor))
		}
	}
	return nil
}
