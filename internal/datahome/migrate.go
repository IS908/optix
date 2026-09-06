package datahome

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

func readDB(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	return sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(1000)")
}

// Migrate exports a transactionally consistent SQLite snapshot, including WAL,
// and publishes a verified copy without replacing any existing target. It never
// deletes or switches the source. Stop writers before using the new database:
// writes committed after the snapshot remain solely in the retained source.
func Migrate(ctx context.Context, source, target string) (string, error) {
	src, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	dst, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source is not a regular database file")
	}
	for _, path := range []string{dst, dst + "-wal", dst + "-shm", dst + "-journal"} {
		if _, err = os.Lstat(path); err == nil {
			return "", fmt.Errorf("destination or sidecar already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".optix-migration-")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(stage)
		}
	}()
	backup := filepath.Join(stage, "backup.db")
	db, err := readDB(src)
	if err != nil {
		return "", err
	}
	defer db.Close()
	// VACUUM INTO is SQLite's consistent logical backup operation; unlike file
	// copying it includes committed pages still resident in the WAL.
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	if err = os.Chmod(backup, 0600); err != nil {
		return "", err
	}
	check, err := readDB(backup)
	if err != nil {
		return "", err
	}
	var integrity string
	err = check.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity)
	check.Close()
	if err != nil {
		return "", err
	}
	if integrity != "ok" {
		return "", fmt.Errorf("snapshot integrity: %s", integrity)
	}
	// Keep an independent recovery copy. The published DB must not share an
	// inode with the backup, because subsequent SQLite writes mutate the file.
	pending := filepath.Join(stage, "pending.db")
	in, err := os.Open(backup)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	copied, err := os.Open(pending)
	if err != nil {
		return "", err
	}
	actual := sha256.New()
	_, err = io.Copy(actual, copied)
	copied.Close()
	if err != nil {
		return "", err
	}
	if string(h.Sum(nil)) != string(actual.Sum(nil)) {
		return "", fmt.Errorf("snapshot copy checksum mismatch")
	}
	// Durably flush the backup before publication too.
	if err = in.Sync(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	// link is atomic and fails for any existing destination, including symlinks.
	if err = os.Link(pending, dst); err != nil {
		return "", fmt.Errorf("publish without overwrite: %w", err)
	}
	keep = true
	os.Remove(pending)
	dir, err := os.Open(filepath.Dir(dst))
	if err != nil {
		return backup, err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return backup, err
	}
	return backup, nil
}
