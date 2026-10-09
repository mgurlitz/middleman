package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureUUIDUpgradeStopsBeforeDeletingCachedData(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	file := filepath.Join(t.TempDir(), "azure-v59.db")
	openAtVersionForTest(t, file, 59, func(raw *sql.DB) {
		_, err := raw.ExecContext(t.Context(), `
			INSERT INTO forge_repos (
				id, platform, platform_host, platform_repo_id,
				owner, name, repo_path, owner_key, name_key, repo_path_key, lifecycle_state
			) VALUES (
				1, 'azure_devops', 'dev.azure.com', '11111111-2222-4333-8444-555555555555',
				'ExampleOrg/ExampleProject', 'ExampleRepo', 'ExampleOrg/ExampleProject/ExampleRepo',
				'exampleorg/exampleproject', 'examplerepo', 'exampleorg/exampleproject/examplerepo', 'active'
			);
			INSERT INTO forge_merge_requests (
				id, repo_id, platform_id, number, title, created_at, updated_at, last_activity_at
			) VALUES (10, 1, 17, 17, 'Retained review', datetime('now'), datetime('now'), datetime('now'));
		`)
		require.NoError(t, err)
	})
	_, err := Open(file)
	require.ErrorContains(t, err, "Azure DevOps database requires a dedicated UUID upgrade")
	raw, err := sql.Open("sqlite", file)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	var version, dirty int
	require.NoError(t, raw.QueryRowContext(t.Context(), `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty))
	assert.Equal(59, version)
	assert.Zero(dirty)
	var repositoryUUID, title string
	require.NoError(t, raw.QueryRowContext(t.Context(), `SELECT platform_repo_id FROM forge_repos WHERE id = 1`).Scan(&repositoryUUID))
	assert.Equal("11111111-2222-4333-8444-555555555555", repositoryUUID)
	require.NoError(t, raw.QueryRowContext(t.Context(), `SELECT title FROM forge_merge_requests WHERE repo_id = 1 AND number = 17`).Scan(&title))
	assert.Equal("Retained review", title)
}
