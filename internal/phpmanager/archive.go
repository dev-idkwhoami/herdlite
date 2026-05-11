package phpmanager

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ExtractTarGz(archivePath string, destination string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}

	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	topDirs := map[string]bool{}

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		cleanName := filepath.Clean(header.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return "", fmt.Errorf("unsafe archive path: %s", header.Name)
		}

		parts := strings.Split(cleanName, string(filepath.Separator))
		if len(parts) > 0 && parts[0] != "." {
			topDirs[parts[0]] = true
		}

		target := filepath.Join(destination, cleanName)
		if !strings.HasPrefix(target, filepath.Clean(destination)+string(filepath.Separator)) && filepath.Clean(target) != filepath.Clean(destination) {
			return "", fmt.Errorf("unsafe archive target: %s", target)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, reader); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		case tar.TypeSymlink:
			if strings.HasPrefix(header.Linkname, "/") || strings.Contains(header.Linkname, "..") {
				return "", fmt.Errorf("unsafe archive symlink: %s -> %s", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			if err := os.Symlink(header.Linkname, target); err != nil && !os.IsExist(err) {
				return "", err
			}
		}
	}

	if len(topDirs) != 1 {
		return "", fmt.Errorf("expected one top-level directory, found %d", len(topDirs))
	}

	for dir := range topDirs {
		return filepath.Join(destination, dir), nil
	}

	return "", fmt.Errorf("archive was empty")
}
