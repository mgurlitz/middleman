package platform

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureRepositoryIdentityPreservesUUID(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	id, err := ParseRepositoryUUID("11111111-2222-4333-8444-555555555555")
	require.NoError(t, err)
	identity := RepositoryIdentity{
		Provider: string(KindAzureDevOps), PlatformHost: DefaultAzureDevOpsHost,
		Key: RepositoryUUIDKey(id),
	}
	assert.True(identity.Valid())
	data, err := json.Marshal(identity)
	require.NoError(t, err)
	var decoded RepositoryIdentity
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(identity, decoded)
	integer := identity
	integer.Key = RepositoryIDKey(17)
	assert.False(integer.Valid())
	github := identity
	github.Provider, github.PlatformHost = string(KindGitHub), DefaultGitHubHost
	assert.False(github.Valid())
}

func TestAzureRemoteIdentityUsesCanonicalRoute(t *testing.T) {
	t.Parallel()
	ref := RepoRef{Platform: KindAzureDevOps, Host: DefaultAzureDevOpsHost, Owner: "Example Org/Example Project", Name: "Example Repo"}
	remote := "https://dev.azure.com/Example%20Org/Example%20Project/_git/Example%20Repo"
	require.NoError(t, ValidateRemoteIdentity(ref, remote))
	assert.Equal(t, "Example Org/Example Project/Example Repo", RemoteRepoPath(ref.Platform, ref.Host, remote))
	require.Error(t, ValidateRemoteIdentity(ref, "https://dev.azure.com/Example%20Org/Other/_git/Example%20Repo"))
	require.Error(t, ValidateRemoteIdentity(ref, "https://other.example/Example%20Org/Example%20Project/_git/Example%20Repo"))
}
