package install

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"herdlite/internal/paths"
)

type TargetUser struct {
	Username string
	HomeDir  string
	UID      int
	GID      int
	Paths    paths.Paths
}

func ResolveTargetUser() (TargetUser, error) {
	username := os.Getenv("SUDO_USER")
	if username == "" {
		current, err := user.Current()
		if err != nil {
			return TargetUser{}, err
		}
		return targetFromOSUser(current)
	}

	u, err := user.Lookup(username)
	if err != nil {
		return TargetUser{}, err
	}
	return targetFromOSUser(u)
}

func targetFromOSUser(u *user.User) (TargetUser, error) {
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return TargetUser{}, fmt.Errorf("parse uid for %s: %w", u.Username, err)
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return TargetUser{}, fmt.Errorf("parse gid for %s: %w", u.Username, err)
	}

	return TargetUser{
		Username: u.Username,
		HomeDir:  u.HomeDir,
		UID:      uid,
		GID:      gid,
		Paths:    paths.ResolveForHome(u.HomeDir),
	}, nil
}

func EnsureTargetDirs(target TargetUser) error {
	if err := target.Paths.EnsureUserDirs(); err != nil {
		return err
	}

	if os.Geteuid() == 0 {
		return RestoreTargetOwnership(target)
	}

	return nil
}

func RestoreTargetOwnership(target TargetUser) error {
	roots := []string{
		target.Paths.ConfigDir,
		target.Paths.DataDir,
		target.Paths.CacheDir,
	}

	for _, root := range roots {
		if err := chownTree(root, target.UID, target.GID); err != nil {
			return err
		}
	}
	return nil
}

func chownTree(root string, uid int, gid int) error {
	return filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := os.Lchown(path, uid, gid); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}
