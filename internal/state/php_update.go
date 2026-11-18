package state

import (
	"database/sql"
	"fmt"
)

// Locks are independent of runtime upserts, so a registration cannot clear one.
func (s *Store) PHPLocked(version string) (bool, error) {
	_, found, err := s.MetaValue("php_locked:" + version)
	return found, err
}

func (s *Store) SetPHPLocked(version string, locked bool) error {
	runtime, found, err := s.PHPRuntimeForRequest(version)
	if err != nil {
		return err
	}
	if !found || runtime.Version != version {
		return fmt.Errorf("PHP %s is not an installed exact patch version", version)
	}
	if locked {
		return s.SetMetaValue("php_locked:"+version, "1")
	}
	return s.DeleteMetaValue("php_locked:" + version)
}

// PHPUpdateState is the state needed to reverse a single minor's replacement.
type PHPUpdateState struct {
	Old       []PHPRuntime
	Target    *PHPRuntime
	Projects  []Project
	Global    string
	HasGlobal bool
}

func putRuntime(tx *sql.Tx, r PHPRuntime) error {
	_, err := tx.Exec(`INSERT INTO php_runtimes
 (version, minor, tag, source, source_url, prefix, php_binary, php_fpm_binary, installed_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(version) DO UPDATE SET minor=excluded.minor, tag=excluded.tag,
 source=excluded.source, source_url=excluded.source_url, prefix=excluded.prefix,
 php_binary=excluded.php_binary, php_fpm_binary=excluded.php_fpm_binary, installed_at=excluded.installed_at`,
		r.Version, r.Minor, r.Tag, r.Source, r.SourceURL, r.Prefix, r.PHPBinary, r.PHPFPMBinary, formatTime(r.InstalledAt))
	return err
}

// ApplyPHPUpdate switches registration and exact references in one transaction.
func (s *Store) ApplyPHPUpdate(before PHPUpdateState, target PHPRuntime) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := putRuntime(tx, target); err != nil {
		return err
	}
	for _, old := range before.Old {
		var locked int
		if err := tx.QueryRow(`SELECT count(*) FROM meta WHERE key=?`, "php_locked:"+old.Version).Scan(&locked); err != nil {
			return err
		}
		if locked != 0 {
			return fmt.Errorf("PHP %s was locked while preparing the update", old.Version)
		}
		if _, err := tx.Exec(`UPDATE projects SET php_version=? WHERE php_version=?`, target.Version, old.Version); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE meta SET value=? WHERE key='global_php_version' AND value=?`, target.Version, old.Version); err != nil {
			return err
		}
		if old.Version != target.Version {
			if _, err := tx.Exec(`DELETE FROM php_runtimes WHERE version=?`, old.Version); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) RestorePHPUpdate(before PHPUpdateState, targetVersion string) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// An empty target restores a removal without touching replacement runtimes.
	if targetVersion != "" {
		if _, err := tx.Exec(`DELETE FROM php_runtimes WHERE version=?`, targetVersion); err != nil {
			return err
		}
	}
	if before.Target != nil {
		if err := putRuntime(tx, *before.Target); err != nil {
			return err
		}
	}
	for _, old := range before.Old {
		if err := putRuntime(tx, old); err != nil {
			return err
		}
	}
	for _, p := range before.Projects {
		if _, err := tx.Exec(`UPDATE projects SET php_version=? WHERE name=?`, p.PHPVersion, p.Name); err != nil {
			return err
		}
	}
	if before.HasGlobal {
		if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES ('global_php_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, before.Global); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`DELETE FROM meta WHERE key='global_php_version'`); err != nil {
		return err
	}
	return tx.Commit()
}
