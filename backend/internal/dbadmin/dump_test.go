package dbadmin

import "testing"

func TestDumpCommandMongo(t *testing.T) {
	cmd, ct, err := dumpCommand(EngineMongoDB, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if cmd[0] != "mongodump" || ct == "" {
		t.Fatalf("unexpected dump command %#v %q", cmd, ct)
	}
}

func TestRestoreCommandPostgresRequiresUser(t *testing.T) {
	if _, err := restoreCommand(EnginePostgres, Credentials{}); err == nil {
		t.Fatal("expected error without user")
	}
}
