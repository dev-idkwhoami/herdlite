package phpmanager

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"herdlite/internal/state"
)

var requiredModules = []string{
	"bcmath",
	"curl",
	"dom",
	"fileinfo",
	"intl",
	"mbstring",
	"openssl",
	"pcntl",
	"PDO",
	"pdo_pgsql",
	"pdo_sqlite",
	"pgsql",
	"Phar",
	"SimpleXML",
	"sockets",
	"sqlite3",
	"tokenizer",
	"xml",
	"xmlreader",
	"xmlwriter",
	"Zend OPcache",
	"zip",
	"zlib",
}

func ValidateRuntime(runtime state.PHPRuntime) error {
	if err := executable(runtime.PHPBinary); err != nil {
		return err
	}
	if err := executable(runtime.PHPFPMBinary); err != nil {
		return err
	}
	if err := validatePHPVersion(runtime); err != nil {
		return err
	}
	if err := validatePHPModules(runtime); err != nil {
		return err
	}
	if err := validateFPMConfig(runtime); err != nil {
		return err
	}
	return nil
}

func validatePHPVersion(runtime state.PHPRuntime) error {
	cmd := exec.Command(runtime.PHPBinary, "-r", "echo PHP_VERSION;")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("php version check failed: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if version != runtime.Version {
		return fmt.Errorf("expected PHP %s, got %s", runtime.Version, version)
	}
	return nil
}

func validatePHPModules(runtime state.PHPRuntime) error {
	cmd := exec.Command(runtime.PHPBinary, "-m")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("php module check failed: %w", err)
	}
	modules := "\n" + string(out) + "\n"
	missing := []string{}
	for _, module := range requiredModules {
		if !strings.Contains(modules, "\n"+module+"\n") {
			missing = append(missing, module)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required PHP modules: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateFPMConfig(runtime state.PHPRuntime) error {
	config := filepath.Join(runtime.Prefix, "etc", "php-fpm.conf")
	if _, err := os.Stat(config); err != nil {
		return fmt.Errorf("php-fpm config missing: %w", err)
	}

	cmd := exec.Command(runtime.PHPFPMBinary, "-t", "--fpm-config", config)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("php-fpm config check failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}
