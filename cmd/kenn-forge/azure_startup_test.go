package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/forge/internal/config"
	"go.kenn.io/forge/internal/testutil/dbtest"
	"go.kenn.io/forge/internal/tokenauth"
	"go.kenn.io/forge/platform"
)

func TestBuildProviderControlPlaneRegistersAzureDevOpsWithoutStoredToken(t *testing.T) {
	assert := assert.New(t)
	set := tokenauth.NewSourceSet(tokenauth.Options{})
	descriptor := tokenauth.Descriptor{Key: tokenauth.Key{Platform: string(platform.KindAzureDevOps), Host: platform.DefaultAzureDevOpsHost}}
	managed := set.Upsert(descriptor)
	called := false
	startup, err := buildProviderControlPlane(t.Context(), dbtest.Open(t), &config.Config{}, set,
		map[string]tokenauth.Source{providerHostKey(descriptor.Key.Platform, descriptor.Key.Host): managed},
		map[string]providerFactory{descriptor.Key.Platform: func(_ context.Context, input providerFactoryInput) (providerFactoryOutput, error) {
			called = true
			assert.Equal(descriptor, input.tokenSource.Descriptor())
			assert.IsType(&azureCLITokenSource{}, input.tokenSource)
			return providerFactoryOutput{provider: mainTestRepositoryReader{kind: platform.KindAzureDevOps, host: input.host}}, nil
		}}, nil)
	require.NoError(t, err)
	assert.True(called)
	reader, err := startup.registry.RepositoryReader(platform.KindAzureDevOps, platform.DefaultAzureDevOpsHost)
	require.NoError(t, err)
	assert.NotNil(reader)
}

func TestAzureGitStartupRegistersLazyCLICredentials(t *testing.T) {
	cfg := &config.Config{Repos: []config.Repo{{Platform: string(platform.KindAzureDevOps), PlatformHost: platform.DefaultAzureDevOpsHost, Owner: "ExampleOrg/ExampleProject", Name: "ExampleRepo"}}}
	startup, err := buildGitStartup(cfg, tokenauth.NewSourceSet(tokenauth.Options{}))
	require.NoError(t, err)
	source := startup.SourceForRepo(string(platform.KindAzureDevOps), platform.DefaultAzureDevOpsHost, "ExampleOrg/ExampleProject", "ExampleRepo")
	require.NotNil(t, source)
	assert.IsType(t, &azureCLITokenSource{}, source)
}
