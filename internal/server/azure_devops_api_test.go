package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/platform"
	azuredevops "github.com/wesm/middleman/internal/platform/azuredevops"
	"github.com/wesm/middleman/internal/testutil/dbtest"
	"github.com/wesm/middleman/internal/workspace"
)

type azureDevOpsStaticToken string

func (s azureDevOpsStaticToken) Token(_ context.Context) (string, error) {
	return string(s), nil
}

func TestAPIAzureDevOpsReadOnlySyncPersistsThroughServer(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	var authHeaders []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/AcmeOrg/Payments/_apis/git/repositories/Service":
			_, _ = w.Write([]byte(`{
				"id": "repo-guid",
				"name": "Service",
				"webUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
				"remoteUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
				"defaultBranch": "refs/heads/main",
				"project": {
					"id": "project-guid",
					"name": "Payments",
					"visibility": "private",
					"lastUpdateTime": "2026-05-21T12:00:00Z"
				}
			}`))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests":
			assert.Equal("active", r.URL.Query().Get("searchCriteria.status"))
			_, _ = w.Write([]byte(`{
				"count": 1,
				"value": [{
					"pullRequestId": 17,
					"status": "active",
					"title": "Azure DevOps provider PR",
					"description": "POC body",
					"creationDate": "2026-05-21T12:00:00Z",
					"isDraft": true,
					"createdBy": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefName": "refs/heads/feature/ado",
					"targetRefName": "refs/heads/main",
					"mergeStatus": "conflicts",
					"lastMergeSourceCommit": {"commitId": "abc123"},
					"lastMergeTargetCommit": {"commitId": "def456"},
					"repository": {
						"id": "repo-guid",
						"name": "Service",
						"webUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
						"remoteUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
						"defaultBranch": "refs/heads/main",
						"project": {
							"id": "project-guid",
							"name": "Payments",
							"visibility": "private",
							"lastUpdateTime": "2026-05-21T12:00:00Z"
						}
					},
					"_links": {
						"web": {"href": "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17"}
					}
				}]
			}`))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17":
			_, _ = w.Write([]byte(`{
				"pullRequestId": 17,
				"status": "active",
				"title": "Azure DevOps provider PR",
				"description": "POC body",
				"creationDate": "2026-05-21T12:00:00Z",
				"isDraft": true,
				"createdBy": {
					"displayName": "Ada Lovelace",
					"uniqueName": "ada@example.com"
				},
				"sourceRefName": "refs/heads/feature/ado",
				"targetRefName": "refs/heads/main",
				"mergeStatus": "conflicts",
				"lastMergeSourceCommit": {"commitId": "abc123"},
				"lastMergeTargetCommit": {"commitId": "def456"},
				"repository": {
					"id": "repo-guid",
					"name": "Service",
					"webUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
					"remoteUrl": "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
					"defaultBranch": "refs/heads/main",
					"project": {
						"id": "project-guid",
						"name": "Payments",
						"visibility": "private",
						"lastUpdateTime": "2026-05-21T12:00:00Z"
					}
				},
				"_links": {
					"web": {"href": "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17"}
				}
			}`))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17/threads":
			_, _ = w.Write([]byte(`{
				"count": 1,
				"value": [{
					"id": 55,
					"comments": [{
						"id": 1001,
						"parentCommentId": 0,
						"commentType": "text",
						"content": "Looks good from Azure DevOps",
						"publishedDate": "2026-05-21T12:05:00Z",
						"author": {"displayName": "Grace Hopper"}
					}]
				}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	database := dbtest.Open(t)
	provider, err := azuredevops.NewClient(
		platform.DefaultAzureDevOpsHost,
		azuredevops.WithBaseURLForTesting(api.URL),
		azuredevops.WithTokenSourceForTesting(azureDevOpsStaticToken("azure-token")),
	)
	require.NoError(err)
	registry, err := platform.NewRegistry(provider)
	require.NoError(err)

	repo := ghclient.RepoRef{
		Platform:     platform.KindAzureDevOps,
		PlatformHost: platform.DefaultAzureDevOpsHost,
		Owner:        "AcmeOrg/Payments",
		Name:         "Service",
		RepoPath:     "AcmeOrg/Payments/Service",
	}
	syncer := ghclient.NewSyncerWithRegistry(
		registry,
		database,
		nil,
		[]ghclient.RepoRef{repo},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{})
	srv.workspaces = workspace.NewManager(database, t.TempDir())
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	syncer.RunOnce(ctx)
	require.NoError(syncer.SyncMROnProvider(ctx, platform.KindAzureDevOps, platform.DefaultAzureDevOpsHost, repo.Owner, repo.Name, 17))

	require.NotEmpty(authHeaders)
	for _, header := range authHeaders {
		assert.Equal("Bearer azure-token", header)
	}

	rawPulls := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service", nil)
	require.Equal(http.StatusOK, rawPulls.Code, rawPulls.Body.String())
	var pulls []mergeRequestResponse
	require.NoError(json.NewDecoder(rawPulls.Body).Decode(&pulls))
	require.Len(pulls, 1)
	assert.Equal("azure_devops", pulls[0].Repo.Provider)
	assert.Equal("AcmeOrg/Payments", pulls[0].RepoOwner)
	assert.Equal("Service", pulls[0].RepoName)
	assert.Equal("Azure DevOps provider PR", pulls[0].Title)
	assert.Equal("dirty", pulls[0].MergeableState)

	rawDetail := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17", nil)
	require.Equal(http.StatusOK, rawDetail.Code, rawDetail.Body.String())
	var detail mergeRequestDetailResponse
	require.NoError(json.NewDecoder(rawDetail.Body).Decode(&detail))
	require.NotNil(detail.MergeRequest)
	require.Len(detail.Events, 1)
	assert.Equal("Looks good from Azure DevOps", detail.Events[0].Body)
	assert.Equal(1, detail.MergeRequest.CommentCount)
	assert.Equal(now.Add(5*time.Minute), detail.MergeRequest.LastActivityAt)
	assert.True(detail.Repo.Capabilities.ReadRepositories)
	assert.True(detail.Repo.Capabilities.ReadMergeRequests)
	assert.True(detail.Repo.Capabilities.ReadComments)
	assert.False(detail.Repo.Capabilities.CommentMutation)
	assert.Nil(detail.Warnings)

	rawFiles := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17/files", nil)
	require.Equal(http.StatusConflict, rawFiles.Code, rawFiles.Body.String())
	var fileProblem ProblemError
	require.NoError(json.NewDecoder(rawFiles.Body).Decode(&fileProblem))
	assert.Equal(CodeUnsupportedCapability, fileProblem.Code)
	assert.Equal("local_clone", fileProblem.Details["capability"])

	rawWorkspace := doJSON(t, srv, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"platform_host": platform.DefaultAzureDevOpsHost,
		"owner":         repo.Owner,
		"name":          repo.Name,
		"mr_number":     17,
	})
	require.Equal(http.StatusConflict, rawWorkspace.Code, rawWorkspace.Body.String())
	var workspaceProblem ProblemError
	require.NoError(json.NewDecoder(rawWorkspace.Body).Decode(&workspaceProblem))
	assert.Equal(CodeUnsupportedCapability, workspaceProblem.Code)
	assert.Equal("local_clone", workspaceProblem.Details["capability"])
}
