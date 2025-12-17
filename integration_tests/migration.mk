record-migration:
	GH_TOKEN=${gh_token} RECORD_MIGRATION=1 go test -run TestMigrationTestSuite/TestDumps/${repo}