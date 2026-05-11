package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const archTrustAnchor = "/etc/ca-certificates/trust-source/anchors/herdlite-local-ca.crt"
const networkManagerHerdliteConf = "/etc/NetworkManager/conf.d/herdlite.conf"
const networkManagerDNSMasqConf = "/etc/NetworkManager/dnsmasq.d/herdlite-test.conf"
const nssCANickname = "Herdlite Local Development CA"

type SystemManager struct {
	Out io.Writer
}

func (s SystemManager) TrustCA(caCertPath string, dryRun bool) error {
	if caCertPath == "" {
		return fmt.Errorf("CA certificate path is empty")
	}

	s.printf("Trust CA: %s -> %s\n", caCertPath, archTrustAnchor)
	s.printf("Run: trust extract-compat\n")
	if dryRun {
		return nil
	}

	if _, err := os.Stat(caCertPath); err != nil {
		return err
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("trusting the local CA requires root")
	}

	if err := os.MkdirAll(filepath.Dir(archTrustAnchor), 0o755); err != nil {
		return err
	}

	data, err := os.ReadFile(caCertPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(archTrustAnchor, data, 0o644); err != nil {
		return err
	}

	trust, err := exec.LookPath("trust")
	if err != nil {
		return fmt.Errorf("trust command not found: %w", err)
	}

	cmd := exec.Command(trust, "extract-compat")
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	return cmd.Run()
}

func (s SystemManager) UntrustCA(dryRun bool) error {
	s.printf("Remove trusted CA: %s\n", archTrustAnchor)
	s.printf("Run: trust extract-compat\n")
	if dryRun {
		return nil
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("removing trusted CA requires root")
	}

	if err := removeIfExists(archTrustAnchor); err != nil {
		return err
	}

	trust, err := exec.LookPath("trust")
	if err != nil {
		return fmt.Errorf("trust command not found: %w", err)
	}

	cmd := exec.Command(trust, "extract-compat")
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	return cmd.Run()
}

func (s SystemManager) TrustUserNSSCA(target TargetUser, caCertPath string, dryRun bool) error {
	if caCertPath == "" {
		return fmt.Errorf("CA certificate path is empty")
	}

	nssDir := filepath.Join(target.HomeDir, ".pki", "nssdb")
	s.printf("Trust CA in user NSS DB: %s -> %s\n", caCertPath, nssDir)
	if dryRun {
		s.printf("Run: certutil -A -d sql:%s -n %q -t C,, -i %s\n", nssDir, nssCANickname, caCertPath)
		return nil
	}

	if _, err := os.Stat(caCertPath); err != nil {
		return err
	}
	if _, err := exec.LookPath("certutil"); err != nil {
		return fmt.Errorf("certutil not found: install the nss package")
	}
	if err := os.MkdirAll(nssDir, 0o700); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(filepath.Dir(nssDir), target.UID, target.GID); err != nil {
			return err
		}
		if err := os.Chown(nssDir, target.UID, target.GID); err != nil {
			return err
		}
	}

	dbPath := filepath.Join(nssDir, "cert9.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := s.runAsTarget(target, "certutil", "-N", "-d", "sql:"+nssDir, "--empty-password"); err != nil {
			return fmt.Errorf("initialize NSS DB: %w", err)
		}
	}

	_ = s.runAsTargetQuiet(target, "certutil", "-D", "-d", "sql:"+nssDir, "-n", nssCANickname)
	if err := s.runAsTarget(target, "certutil", "-A", "-d", "sql:"+nssDir, "-n", nssCANickname, "-t", "C,,", "-i", caCertPath); err != nil {
		return fmt.Errorf("add CA to NSS DB: %w", err)
	}
	return nil
}

func (s SystemManager) UntrustUserNSSCA(target TargetUser, dryRun bool) error {
	nssDir := filepath.Join(target.HomeDir, ".pki", "nssdb")
	s.printf("Remove CA from user NSS DB: %s\n", nssDir)
	if dryRun {
		s.printf("Run: certutil -D -d sql:%s -n %q\n", nssDir, nssCANickname)
		return nil
	}
	if _, err := os.Stat(filepath.Join(nssDir, "cert9.db")); os.IsNotExist(err) {
		return nil
	}
	if _, err := exec.LookPath("certutil"); err != nil {
		return fmt.Errorf("certutil not found: install the nss package")
	}
	if err := s.runAsTargetQuiet(target, "certutil", "-D", "-d", "sql:"+nssDir, "-n", nssCANickname); err != nil {
		message := err.Error()
		if strings.Contains(message, "SEC_ERROR_BAD_DATABASE") || strings.Contains(message, "could not find certificate") {
			return nil
		}
		return fmt.Errorf("remove CA from NSS DB: %w", err)
	}
	return nil
}

func (s SystemManager) EnableLowPortBinding(binary string, dryRun bool) error {
	if dryRun {
		if binary == "" {
			binary = "nginx"
		}
		s.printf("Enable low-port binding: setcap cap_net_bind_service=+ep %s\n", binary)
		return nil
	}

	if binary == "" {
		var err error
		binary, err = exec.LookPath("nginx")
		if err != nil {
			return fmt.Errorf("nginx not found")
		}
	}

	s.printf("Enable low-port binding: setcap cap_net_bind_service=+ep %s\n", binary)

	if os.Geteuid() != 0 {
		return fmt.Errorf("setting nginx capability requires root")
	}

	setcap, err := exec.LookPath("setcap")
	if err != nil {
		return fmt.Errorf("setcap not found: %w", err)
	}

	cmd := exec.Command(setcap, "cap_net_bind_service=+ep", binary)
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	return cmd.Run()
}

func (s SystemManager) runAsTarget(target TargetUser, name string, args ...string) error {
	command := name
	commandArgs := args
	if os.Geteuid() == 0 {
		sudo, err := exec.LookPath("sudo")
		if err != nil {
			return fmt.Errorf("sudo not found")
		}
		command = sudo
		commandArgs = append([]string{"-u", target.Username, "-H", name}, args...)
	}

	cmd := exec.Command(command, commandArgs...)
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	return cmd.Run()
}

func (s SystemManager) runAsTargetQuiet(target TargetUser, name string, args ...string) error {
	command := name
	commandArgs := args
	if os.Geteuid() == 0 {
		sudo, err := exec.LookPath("sudo")
		if err != nil {
			return fmt.Errorf("sudo not found")
		}
		command = sudo
		commandArgs = append([]string{"-u", target.Username, "-H", name}, args...)
	}

	out, err := exec.Command(command, commandArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s SystemManager) HasLowPortBinding(binary string) (bool, error) {
	if binary == "" {
		var err error
		binary, err = exec.LookPath("nginx")
		if err != nil {
			return false, fmt.Errorf("nginx not found")
		}
	}
	getcap, err := exec.LookPath("getcap")
	if err != nil {
		return false, fmt.Errorf("getcap not found: %w", err)
	}
	out, err := exec.Command(getcap, binary).CombinedOutput()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "cap_net_bind_service"), nil
}

func (s SystemManager) DisableLowPortBinding(binary string, dryRun bool) error {
	if dryRun {
		if binary == "" {
			binary = "nginx"
		}
		s.printf("Remove low-port binding if Herdlite recorded ownership: setcap -r %s\n", binary)
		return nil
	}

	if binary == "" {
		var err error
		binary, err = exec.LookPath("nginx")
		if err != nil {
			return fmt.Errorf("nginx not found")
		}
	}

	s.printf("Remove low-port binding: setcap -r %s\n", binary)
	if os.Geteuid() != 0 {
		return fmt.Errorf("removing nginx capability requires root")
	}
	setcap, err := exec.LookPath("setcap")
	if err != nil {
		return fmt.Errorf("setcap not found: %w", err)
	}
	cmd := exec.Command(setcap, "-r", binary)
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	return cmd.Run()
}

func (s SystemManager) ConfigureTestDNS(dryRun bool) error {
	if err := detectNetworkManagerDNSMasq(); err != nil {
		return err
	}

	s.printf("Configure .test DNS through NetworkManager dnsmasq\n")
	s.printf("Write: %s\n", networkManagerHerdliteConf)
	s.printf("Write: %s\n", networkManagerDNSMasqConf)
	s.printf("Run: systemctl reload NetworkManager\n")
	if dryRun {
		return nil
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("configuring .test DNS requires root")
	}

	if err := os.MkdirAll(filepath.Dir(networkManagerHerdliteConf), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(networkManagerDNSMasqConf), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(networkManagerHerdliteConf, []byte("[main]\ndns=dnsmasq\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(networkManagerDNSMasqConf, []byte("address=/.test/127.0.0.1\n"), 0o644); err != nil {
		return err
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		s.printf("Warning: systemctl not found; restart NetworkManager manually for DNS changes.\n")
		return nil
	}

	cmd := exec.Command("systemctl", "reload", "NetworkManager")
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	if err := cmd.Run(); err != nil {
		s.printf("Warning: failed to reload NetworkManager: %v\n", err)
		s.printf("Restart NetworkManager manually for DNS changes to take effect.\n")
	}
	return nil
}

func (s SystemManager) DisableTestDNS(dryRun bool) error {
	s.printf("Disable .test DNS NetworkManager dnsmasq config by commenting Herdlite files\n")
	s.printf("Comment: %s\n", networkManagerHerdliteConf)
	s.printf("Comment: %s\n", networkManagerDNSMasqConf)
	s.printf("Run: systemctl reload NetworkManager\n")
	if dryRun {
		return nil
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("disabling .test DNS config requires root")
	}

	if err := commentFile(networkManagerDNSMasqConf, "Disabled by Herdlite uninstall"); err != nil {
		return err
	}
	if err := commentFile(networkManagerHerdliteConf, "Disabled by Herdlite uninstall"); err != nil {
		return err
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		s.printf("Warning: systemctl not found; restart NetworkManager manually for DNS changes.\n")
		return nil
	}

	cmd := exec.Command("systemctl", "reload", "NetworkManager")
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	if err := cmd.Run(); err != nil {
		s.printf("Warning: failed to reload NetworkManager: %v\n", err)
		s.printf("Restart NetworkManager manually for DNS changes to take effect.\n")
	}
	return nil
}

func detectNetworkManagerDNSMasq() error {
	if _, err := exec.LookPath("NetworkManager"); err != nil {
		return fmt.Errorf("NetworkManager was not found; automatic .test DNS setup currently supports NetworkManager dnsmasq only")
	}
	if _, err := exec.LookPath("dnsmasq"); err != nil {
		return fmt.Errorf("dnsmasq was not found; install dnsmasq before configuring .test DNS")
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	out, err := exec.Command("systemctl", "is-enabled", "NetworkManager").CombinedOutput()
	if err == nil {
		return nil
	}
	status := strings.TrimSpace(string(out))
	if status == "disabled" || status == "masked" {
		return fmt.Errorf("NetworkManager is %s; automatic .test DNS setup currently requires NetworkManager", status)
	}
	return nil
}

func (s SystemManager) printf(format string, args ...any) {
	if s.Out == nil {
		return
	}
	fmt.Fprintf(s.Out, format, args...)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func commentFile(path string, reason string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	out := []string{"# " + reason}
	for _, line := range lines {
		if line == "" {
			out = append(out, "#")
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, line)
			continue
		}
		out = append(out, "# "+line)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
