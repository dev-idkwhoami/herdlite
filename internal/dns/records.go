package dns

import (
	"fmt"
	"sort"
	"strings"

	"herdlite/internal/debugui"
	"herdlite/internal/state"
)

func HostRecordConfig(store *state.Store) (string, error) {
	domains := map[string]bool{debugui.Domain: true}
	if store != nil {
		projects, err := store.Projects()
		if err != nil {
			return "", err
		}
		for _, project := range projects {
			addDomain(domains, project.Domain)
			if project.Websocket.Enabled {
				addDomain(domains, project.Websocket.Domain)
			}
		}
	}

	names := make([]string, 0, len(domains))
	for domain := range domains {
		names = append(names, domain)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Managed by Herdlite. Do not edit by hand.\n")
	for _, domain := range names {
		fmt.Fprintf(&b, "host-record=%s,127.0.0.1\n", domain)
	}
	return b.String(), nil
}

func addDomain(domains map[string]bool, domain string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return
	}
	domains[domain] = true
}
