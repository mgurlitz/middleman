package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/middleman/internal/db"
	"go.kenn.io/middleman/internal/gitenv"
	"go.kenn.io/middleman/internal/gitclone"
	ghclient "go.kenn.io/middleman/internal/github"
	"go.kenn.io/middleman/internal/platform"
	azuredevops "go.kenn.io/middleman/internal/platform/azuredevops"
	"go.kenn.io/middleman/internal/procutil"
	"go.kenn.io/middleman/internal/testutil/dbtest"
	"go.kenn.io/middleman/internal/workspace"
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
	remote, baseSHA, headSHA := setupAzureDevOpsCloneFixture(t)

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
			_, _ = w.Write([]byte(fmt.Sprintf(`{
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
					"lastMergeSourceCommit": {"commitId": %q},
					"lastMergeTargetCommit": {"commitId": %q},
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
			}`, headSHA, baseSHA)))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17":
			_, _ = w.Write([]byte(fmt.Sprintf(`{
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
				"lastMergeSourceCommit": {"commitId": %q},
				"lastMergeTargetCommit": {"commitId": %q},
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
			}`, headSHA, baseSHA)))
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
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17/iterations":
			_, _ = w.Write([]byte(fmt.Sprintf(`{
				"count": 2,
				"value": [{
					"id": 1,
					"createdDate": "2026-05-21T12:02:00Z",
					"author": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefCommit": {"commitId": %q},
					"targetRefCommit": {"commitId": %q},
					"commonRefCommit": {"commitId": %q}
				}, {
					"id": 2,
					"updatedDate": "2026-05-21T12:06:00Z",
					"reason": "push",
					"author": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefCommit": {"commitId": %q},
					"targetRefCommit": {"commitId": %q},
					"commonRefCommit": {"commitId": %q}
				}]
			}`, baseSHA, baseSHA, baseSHA, headSHA, baseSHA, baseSHA)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	database := dbtest.Open(t)
	clones := gitclone.New(t.TempDir(), nil)
	provider, err := azuredevops.NewClient(
		platform.DefaultAzureDevOpsHost,
		azuredevops.WithBaseURLForTesting(api.URL),
		azuredevops.WithTokenSource(azureDevOpsStaticToken("azure-token")),
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
		CloneURL:     remote,
	}
	syncer := ghclient.NewSyncerWithRegistry(
		registry,
		database,
		clones,
		[]ghclient.RepoRef{repo},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{Clones: clones})
	srv.workspaces = workspace.NewManager(database, t.TempDir())
	srv.workspaces.SetClones(clones)
	srv.workspaces.SetTmuxCommand([]string{"sh", "-c", "exit 0"})
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	syncer.RunOnce(ctx)
	require.NoError(syncer.SyncMROnProvider(ctx, platform.KindAzureDevOps, platform.DefaultAzureDevOpsHost, repo.Owner, repo.Name, 17))

	repoRow, err := database.GetRepoByHostOwnerName(ctx, repo.PlatformHost, repo.Owner, repo.Name)
	require.NoError(err)
	require.NotNil(repoRow)
	require.NoError(database.UpdateRepoProviderMetadata(ctx, repoRow.ID, db.RepoProviderMetadata{
		PlatformRepoID: repoRow.PlatformRepoID,
		WebURL:         repoRow.WebURL,
		CloneURL:       remote,
		DefaultBranch:  repoRow.DefaultBranch,
	}))

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
	require.Len(detail.Events, 3)
	assert.Equal("iteration", detail.Events[0].EventType)
	assert.Equal("Iteration 1", detail.Events[0].Summary)
	assert.Equal("issue_comment", detail.Events[1].EventType)
	assert.Equal("Looks good from Azure DevOps", detail.Events[1].Body)
	assert.Equal("iteration", detail.Events[2].EventType)
	assert.Equal("Iteration 2", detail.Events[2].Summary)
	assert.Equal(1, detail.MergeRequest.CommentCount)
	assert.Equal(now.Add(6*time.Minute), detail.MergeRequest.LastActivityAt)
	assert.Equal(headSHA, detail.DiffHeadSHA)
	assert.Equal(baseSHA, detail.MergeBaseSHA)
	assert.True(detail.Repo.Capabilities.ReadRepositories)
	assert.True(detail.Repo.Capabilities.ReadMergeRequests)
	assert.True(detail.Repo.Capabilities.ReadComments)
	assert.False(detail.Repo.Capabilities.CommentMutation)
	assert.Nil(detail.Warnings)

	rawFiles := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17/files", nil)
	require.Equal(http.StatusOK, rawFiles.Code, rawFiles.Body.String())
	var files filesResponse
	require.NoError(json.NewDecoder(rawFiles.Body).Decode(&files))
	require.NotEmpty(files.Files)
	assert.Equal("README.md", files.Files[0].Path)

	rawDiff := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17/diff", nil)
	require.Equal(http.StatusOK, rawDiff.Code, rawDiff.Body.String())
	var diff diffResponse
	require.NoError(json.NewDecoder(rawDiff.Body).Decode(&diff))
	require.NotEmpty(diff.Files)
	assert.Equal("README.md", diff.Files[0].Path)

	rawWorkspace := doJSON(t, srv, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"platform_host": platform.DefaultAzureDevOpsHost,
		"owner":         repo.Owner,
		"name":          repo.Name,
		"mr_number":     17,
	})
	require.Equal(http.StatusAccepted, rawWorkspace.Code, rawWorkspace.Body.String())
	var created workspaceResponse
	require.NoError(json.NewDecoder(rawWorkspace.Body).Decode(&created))
	require.NotEmpty(created.ID)

	require.Eventually(func() bool {
		summary, err := srv.workspaces.GetSummary(ctx, created.ID)
		return err == nil && summary != nil && summary.Status == "ready"
	}, 5*time.Second, 25*time.Millisecond)
}

func TestAPIAzureDevOpsMergedEventsAppearInDetailAndActivityAfterClosure(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()
	openedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	mergedAt := openedAt.Add(8 * time.Minute)
	remote, baseSHA, headSHA := setupAzureDevOpsCloneFixture(t)

	merged := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if merged {
				_, _ = w.Write([]byte(`{"count": 0, "value": []}`))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{
				"count": 1,
				"value": [{
					"pullRequestId": 17,
					"status": "active",
					"title": "Azure DevOps provider PR",
					"description": "POC body",
					"creationDate": "2026-05-21T12:00:00Z",
					"createdBy": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefName": "refs/heads/feature/ado",
					"targetRefName": "refs/heads/main",
					"mergeStatus": "conflicts",
					"lastMergeSourceCommit": {"commitId": %q},
					"lastMergeTargetCommit": {"commitId": %q},
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
			}`, headSHA, baseSHA)))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17":
			if merged {
				_, _ = w.Write([]byte(fmt.Sprintf(`{
					"pullRequestId": 17,
					"status": "completed",
					"title": "Azure DevOps provider PR",
					"description": "POC body",
					"creationDate": "2026-05-21T12:00:00Z",
					"closedDate": %q,
					"createdBy": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefName": "refs/heads/feature/ado",
					"targetRefName": "refs/heads/main",
					"mergeStatus": "succeeded",
					"lastMergeSourceCommit": {"commitId": %q},
					"lastMergeTargetCommit": {"commitId": %q},
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
				}`, mergedAt.Format(time.RFC3339), headSHA, baseSHA)))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{
				"pullRequestId": 17,
				"status": "active",
				"title": "Azure DevOps provider PR",
				"description": "POC body",
				"creationDate": "2026-05-21T12:00:00Z",
				"createdBy": {
					"displayName": "Ada Lovelace",
					"uniqueName": "ada@example.com"
				},
				"sourceRefName": "refs/heads/feature/ado",
				"targetRefName": "refs/heads/main",
				"mergeStatus": "conflicts",
				"lastMergeSourceCommit": {"commitId": %q},
				"lastMergeTargetCommit": {"commitId": %q},
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
			}`, headSHA, baseSHA)))
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17/threads":
			if merged {
				_, _ = w.Write([]byte(`{
					"count": 2,
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
					}, {
						"id": 56,
						"comments": [{
							"id": 1002,
							"parentCommentId": 0,
							"commentType": "system",
							"content": "Merged",
							"publishedDate": "2026-05-21T12:08:00Z",
							"author": {"displayName": "Merge Bot"}
						}]
					}]
				}`))
				return
			}
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
		case "/AcmeOrg/Payments/_apis/git/repositories/Service/pullRequests/17/iterations":
			_, _ = w.Write([]byte(`{"count": 0, "value": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	database := dbtest.Open(t)
	clones := gitclone.New(t.TempDir(), nil)
	provider, err := azuredevops.NewClient(
		platform.DefaultAzureDevOpsHost,
		azuredevops.WithBaseURLForTesting(api.URL),
		azuredevops.WithTokenSource(azureDevOpsStaticToken("azure-token")),
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
		CloneURL:     remote,
	}
	syncer := ghclient.NewSyncerWithRegistry(
		registry,
		database,
		clones,
		[]ghclient.RepoRef{repo},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{Clones: clones})
	srv.workspaces = workspace.NewManager(database, t.TempDir())
	srv.workspaces.SetClones(clones)
	srv.workspaces.SetTmuxCommand([]string{"sh", "-c", "exit 0"})
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	syncer.RunOnce(ctx)
	merged = true
	syncer.RunOnce(ctx)

	repoRow, err := database.GetRepoByHostOwnerName(ctx, repo.PlatformHost, repo.Owner, repo.Name)
	require.NoError(err)
	require.NotNil(repoRow)
	require.NoError(database.UpdateRepoProviderMetadata(ctx, repoRow.ID, db.RepoProviderMetadata{
		PlatformRepoID: repoRow.PlatformRepoID,
		WebURL:         repoRow.WebURL,
		CloneURL:       remote,
		DefaultBranch:  repoRow.DefaultBranch,
	}))

	rawDetail := doJSON(t, srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17", nil)
	require.Equal(http.StatusOK, rawDetail.Code, rawDetail.Body.String())
	var detail mergeRequestDetailResponse
	require.NoError(json.NewDecoder(rawDetail.Body).Decode(&detail))
	require.NotNil(detail.MergeRequest)
	assert.Equal("merged", detail.MergeRequest.State)
	assert.Equal(mergedAt, detail.MergeRequest.LastActivityAt)
	require.Len(detail.Events, 2)
	assert.Equal("issue_comment", detail.Events[0].EventType)
	assert.Equal("merged", detail.Events[1].EventType)
	assert.Equal("Merged", detail.Events[1].Summary)
	assert.Equal("Merged", detail.Events[1].Body)
	assert.Equal("Merge Bot", detail.Events[1].Author)
	assert.Equal(1, detail.MergeRequest.CommentCount)

	since := openedAt.Add(-time.Hour).Format(time.RFC3339)
	rawActivity := doJSON(t, srv, http.MethodGet, "/api/v1/activity?since="+url.QueryEscape(since)+"&types=merged", nil)
	require.Equal(http.StatusOK, rawActivity.Code, rawActivity.Body.String())
	var activity activityResponse
	require.NoError(json.NewDecoder(rawActivity.Body).Decode(&activity))
	require.Len(activity.Items, 1)
	assert.Equal("merged", activity.Items[0].ActivityType)
	assert.Equal("merged", activity.Items[0].ItemState)
	assert.Equal(17, activity.Items[0].ItemNumber)
	assert.Equal("Merge Bot", activity.Items[0].Author)
	assert.Equal("Merged", activity.Items[0].BodyPreview)
}

func TestAPIAzureDevOpsSyncHandlesSpacesInRepoIdentity(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := t.Context()

	var requestedPaths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/Acme%20Org/Payments%20Team/_apis/git/repositories/Space%20Repo":
			_, _ = w.Write([]byte(`{
				"id": "repo-guid",
				"name": "Space Repo",
				"webUrl": "https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo",
				"remoteUrl": "https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo",
				"defaultBranch": "refs/heads/main",
				"project": {
					"id": "project-guid",
					"name": "Payments Team",
					"visibility": "private",
					"lastUpdateTime": "2026-05-21T12:00:00Z"
				}
			}`))
		case "/Acme%20Org/Payments%20Team/_apis/git/repositories/Space%20Repo/pullRequests":
			assert.Equal("active", r.URL.Query().Get("searchCriteria.status"))
			_, _ = w.Write([]byte(`{
				"count": 1,
				"value": [{
					"pullRequestId": 17,
					"status": "active",
					"title": "Handle spaces in Azure DevOps repo paths",
					"description": "POC body",
					"creationDate": "2026-05-21T12:00:00Z",
					"createdBy": {
						"displayName": "Ada Lovelace",
						"uniqueName": "ada@example.com"
					},
					"sourceRefName": "refs/heads/feature/space-paths",
					"targetRefName": "refs/heads/main",
					"mergeStatus": "succeeded",
					"repository": {
						"id": "repo-guid",
						"name": "Space Repo",
						"webUrl": "https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo",
						"remoteUrl": "https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo",
						"defaultBranch": "refs/heads/main",
						"project": {
							"id": "project-guid",
							"name": "Payments Team",
							"visibility": "private",
							"lastUpdateTime": "2026-05-21T12:00:00Z"
						}
					},
					"_links": {
						"web": {"href": "https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo/pullrequest/17"}
					}
				}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	database := dbtest.Open(t)
	clones := gitclone.New(t.TempDir(), nil)
	provider, err := azuredevops.NewClient(
		platform.DefaultAzureDevOpsHost,
		azuredevops.WithBaseURLForTesting(api.URL),
		azuredevops.WithTokenSource(azureDevOpsStaticToken("azure-token")),
	)
	require.NoError(err)
	registry, err := platform.NewRegistry(provider)
	require.NoError(err)

	repo := ghclient.RepoRef{
		Platform:     platform.KindAzureDevOps,
		PlatformHost: platform.DefaultAzureDevOpsHost,
		Owner:        "Acme Org/Payments Team",
		Name:         "Space Repo",
		RepoPath:     "Acme Org/Payments Team/Space Repo",
	}
	syncer := ghclient.NewSyncerWithRegistry(
		registry,
		database,
		clones,
		[]ghclient.RepoRef{repo},
		time.Minute,
		nil,
		nil,
	)
	t.Cleanup(syncer.Stop)
	srv := New(database, syncer, nil, "/", nil, ServerOptions{Clones: clones})
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	syncer.RunOnce(ctx)

	repoRow, err := database.GetRepoByHostOwnerName(ctx, repo.PlatformHost, repo.Owner, repo.Name)
	require.NoError(err)
	require.NotNil(repoRow)
	assert.Equal("Acme Org/Payments Team", repoRow.Owner)
	assert.Equal("Space Repo", repoRow.Name)

	rawPulls := doJSON(
		t,
		srv,
		http.MethodGet,
		"/api/v1/pulls/azure_devops/"+url.PathEscape(repo.Owner)+"/"+url.PathEscape(repo.Name),
		nil,
	)
	require.Equal(http.StatusOK, rawPulls.Code, rawPulls.Body.String())
	var pulls []mergeRequestResponse
	require.NoError(json.NewDecoder(rawPulls.Body).Decode(&pulls))
	require.Len(pulls, 1)
	assert.Equal("Acme Org/Payments Team", pulls[0].RepoOwner)
	assert.Equal("Space Repo", pulls[0].RepoName)
	assert.Equal("Handle spaces in Azure DevOps repo paths", pulls[0].Title)
	assert.Contains(requestedPaths, "/Acme%20Org/Payments%20Team/_apis/git/repositories/Space%20Repo")
	assert.Contains(requestedPaths, "/Acme%20Org/Payments%20Team/_apis/git/repositories/Space%20Repo/pullRequests")
}

func setupAzureDevOpsCloneFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")
	runAzureTestGit(t, dir, "init", "--bare", "--initial-branch=main", remote)
	runAzureTestGit(t, dir, "clone", remote, work)
	runAzureTestGit(t, work, "config", "user.email", "test@test.com")
	runAzureTestGit(t, work, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("base\n"), 0o644))
	runAzureTestGit(t, work, "add", ".")
	runAzureTestGit(t, work, "commit", "-m", "base")
	runAzureTestGit(t, work, "push", "origin", "main")
	baseSHA := azureTestGitSHA(t, work, "HEAD")
	runAzureTestGit(t, work, "checkout", "-b", "feature/ado")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("base\nchange\n"), 0o644))
	runAzureTestGit(t, work, "add", ".")
	runAzureTestGit(t, work, "commit", "-m", "feature")
	runAzureTestGit(t, work, "push", "origin", "feature/ado")
	headSHA := azureTestGitSHA(t, work, "HEAD")
	return remote, baseSHA, headSHA
}

func runAzureTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := procutil.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func azureTestGitSHA(t *testing.T, dir string, ref string) string {
	t.Helper()
	cmd := procutil.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	cmd.Env = append(gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git rev-parse %s failed: %s", ref, out)
	return strings.TrimSpace(string(out))
}
