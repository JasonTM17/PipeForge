package migrations

import "testing"

func TestLoadReturnsOrderedEmbeddedMigrations(t *testing.T) {
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded) != 7 || loaded[0].Version != 1 || loaded[0].Name != "000001_initial.sql" || loaded[1].Version != 2 || loaded[1].Name != "000002_identity.sql" || loaded[2].Version != 3 || loaded[2].Name != "000003_datasets.sql" || loaded[3].Version != 4 || loaded[3].Name != "000004_multipart_uploads.sql" || loaded[4].Version != 5 || loaded[4].Name != "000005_messaging.sql" || loaded[5].Version != 6 || loaded[5].Name != "000006_multipart_operation_fencing.sql" || loaded[6].Version != 7 || loaded[6].Name != "000007_jobs.sql" {
		t.Fatalf("unexpected migrations: %+v", loaded)
	}
}
