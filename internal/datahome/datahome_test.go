package datahome

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrecedenceAndLegacyGuard(t *testing.T) {
	h := t.TempDir()
	old := filepath.Join(h, "old.db")
	os.WriteFile(old, []byte("old"), 0600)
	o := Options{Home: h, OS: "linux", Legacy: []string{old}}
	if _, err := Resolve(o); err == nil {
		t.Fatal("legacy database must prevent silent new database")
	}
	for _, tc := range []struct{ flag, env, cfg, want string }{{"flag", "env", "cfg", "flag"}, {"", "env", "cfg", "env"}, {"", "", "cfg", "cfg"}} {
		o.Flag, o.Env, o.Config = tc.flag, tc.env, tc.cfg
		got, err := Resolve(o)
		if err != nil || got.Path != tc.want {
			t.Fatalf("%+v %v", got, err)
		}
	}
	o.Flag, o.Env, o.Config = "", "", ""
	o.Legacy = nil
	got, err := Resolve(o)
	if err != nil || got.Path != filepath.Join(h, ".local/share/optix/optix.db") {
		t.Fatalf("%+v %v", got, err)
	}
	o.OS = "darwin"
	got, _ = Resolve(o)
	if got.Path != filepath.Join(h, "Library/Application Support/optix/optix.db") {
		t.Fatal(got)
	}
	o.XDG = filepath.Join(h, "xdg")
	got, _ = Resolve(o)
	if got.Path != filepath.Join(o.XDG, "optix/optix.db") {
		t.Fatal(got)
	}
	o.XDG = "relative"
	if _, err := Resolve(o); err == nil {
		t.Fatal("relative XDG accepted")
	}
}

func TestMigrateIncludesWALAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.db")
	dst := filepath.Join(dir, "new", "optix.db")
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE journal(id INTEGER PRIMARY KEY, note TEXT); INSERT INTO journal VALUES(1,'kept')"); err != nil {
		t.Fatal(err)
	}
	backup, err := Migrate(context.Background(), src, dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{src, dst, backup} {
		c, e := sql.Open("sqlite", p)
		if e != nil {
			t.Fatal(e)
		}
		var note string
		e = c.QueryRow("SELECT note FROM journal WHERE id=1").Scan(&note)
		c.Close()
		if e != nil || note != "kept" {
			t.Fatalf("%s: %s %v", p, note, e)
		}
	}
	if _, err = Migrate(context.Background(), src, dst); err == nil {
		t.Fatal("existing destination overwritten")
	}
	if _, err = Migrate(context.Background(), src, src); err == nil {
		t.Fatal("source overwritten")
	}
}

func TestMigrateFailureLeavesNoDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "new.db")
	for _, src := range []string{filepath.Join(dir, "missing.db"), filepath.Join(dir, "invalid.db")} {
		if filepath.Base(src) == "invalid.db" {
			os.WriteFile(src, []byte("not sqlite"), 0600)
		}
		if _, err := Migrate(context.Background(), src, dst); err == nil {
			t.Fatal("invalid input accepted")
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Fatal("destination created on failure")
		}
	}
}

func TestMigrateCancellationAndDestinationSidecars(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	dst := filepath.Join(dir, "dst.db")
	db, _ := sql.Open("sqlite", src)
	defer db.Close()
	db.Exec("CREATE TABLE t(x); INSERT INTO t VALUES(42)")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Migrate(ctx, src, dst); err == nil {
		t.Fatal("canceled migration succeeded")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("canceled destination exists")
	}
	os.WriteFile(dst+"-wal", []byte("unrelated WAL"), 0600)
	if _, err := Migrate(context.Background(), src, dst); err == nil {
		t.Fatal("orphan WAL at target accepted")
	}
}

func TestMigrateConcurrentWriterAndIndependentBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	dst := filepath.Join(dir, "dst.db")
	db, _ := sql.Open("sqlite", src)
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE t(id INTEGER PRIMARY KEY, text TEXT); INSERT INTO t VALUES(1,'original')"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 2; i <= 100; i++ {
			if _, err := db.Exec("INSERT INTO t VALUES(?, 'committed')", i); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	backup, err := Migrate(context.Background(), src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	target, _ := sql.Open("sqlite", dst)
	defer target.Close()
	recovery, _ := sql.Open("sqlite", backup)
	defer recovery.Close()
	var a, b int
	target.QueryRow("SELECT count(*) FROM t").Scan(&a)
	recovery.QueryRow("SELECT count(*) FROM t").Scan(&b)
	if a < 1 || a > 100 || a != b {
		t.Fatalf("inconsistent snapshot: %d vs %d", a, b)
	}
	if _, err = target.Exec("UPDATE t SET text='new' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	var original string
	recovery.QueryRow("SELECT text FROM t WHERE id=1").Scan(&original)
	if original != "original" {
		t.Fatal("destination writes changed backup")
	}
}
