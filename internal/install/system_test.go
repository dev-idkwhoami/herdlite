package install

import (
	"strings"
	"testing"
)

func TestNginxCapabilityHookContent(t *testing.T) {
	content := nginxCapabilityHookContent("/usr/bin/nginx")

	for _, want := range []string{
		"Operation = Install",
		"Operation = Upgrade",
		"Type = Package",
		"Target = nginx",
		"When = PostTransaction",
		"Depends = libcap",
		"Exec = /usr/bin/setcap cap_net_bind_service=+ep /usr/bin/nginx",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected hook content to contain %q, got:\n%s", want, content)
		}
	}
}
