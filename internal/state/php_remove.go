package state

import "fmt"

// ApplyPHPRemoval migrates every reference before unregistering an exact patch.
// Targets may differ by project and may belong to another minor series.
func (s *Store) ApplyPHPRemoval(before PHPUpdateState, replacements map[string]string, global string) error {
	if len(before.Old) != 1 {
		return fmt.Errorf("removal requires one exact runtime")
	}
	version := before.Old[0].Version
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
	var locked int
	if err := tx.QueryRow(`SELECT count(*) FROM meta WHERE key=?`, "php_locked:"+version).Scan(&locked); err != nil {
		return err
	}
	if locked != 0 {
		return fmt.Errorf("PHP %s is locked; unlock it before removing it", version)
	}
	validate := func(target string) error {
		if target == version || target == "" {
			return fmt.Errorf("choose a different installed patch version")
		}
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM php_runtimes WHERE version=?`, target).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("replacement PHP %s is not installed", target)
		}
		return nil
	}
	for _, p := range before.Projects {
		target := replacements[p.Name]
		if err := validate(target); err != nil {
			return fmt.Errorf("project %s: %w", p.Name, err)
		}
		result, err := tx.Exec(`UPDATE projects SET php_version=? WHERE name=? AND php_version=?`, target, p.Name, version)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("project %s changed since removal was planned", p.Name)
		}
	}
	var remaining int
	if err := tx.QueryRow(`SELECT count(*) FROM projects WHERE php_version=?`, version).Scan(&remaining); err != nil {
		return err
	}
	if remaining != 0 {
		return fmt.Errorf("PHP %s has additional project references; retry removal", version)
	}
	if before.HasGlobal && before.Global == version {
		if err := validate(global); err != nil {
			return fmt.Errorf("global PHP: %w", err)
		}
		if _, err := tx.Exec(`UPDATE meta SET value=? WHERE key='global_php_version' AND value=?`, global, version); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(`SELECT count(*) FROM meta WHERE key='global_php_version' AND value=?`, version).Scan(&remaining); err != nil {
		return err
	}
	if remaining != 0 {
		return fmt.Errorf("global PHP changed since removal was planned; retry removal")
	}
	result, err := tx.Exec(`DELETE FROM php_runtimes WHERE version=?`, version)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("PHP %s is no longer registered", version)
	}
	return tx.Commit()
}
