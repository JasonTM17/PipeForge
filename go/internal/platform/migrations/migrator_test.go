package migrations

import "testing"

func TestLoadReturnsOrderedEmbeddedMigrations(t *testing.T) {
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded) != 3 || loaded[0].Version != 1 || loaded[0].Name != "000001_initial.sql" || loaded[1].Version != 2 || loaded[1].Name != "000002_identity.sql" || loaded[2].Version != 3 || loaded[2].Name != "000003_datasets.sql" {
		t.Fatalf("unexpected migrations: %+v", loaded)
	}
}
