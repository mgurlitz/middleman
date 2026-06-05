package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/forge/internal/config"
	"go.kenn.io/forge/internal/gitclone"
	ghclient "go.kenn.io/forge/internal/github"
	azuredevops "go.kenn.io/forge/internal/platform/azuredevops"
	"go.kenn.io/forge/internal/server/httpapi"
	"go.kenn.io/forge/internal/server/pullapi"
	"go.kenn.io/forge/internal/testutil/dbtest"
	"go.kenn.io/forge/platform"
	gitcmd "go.kenn.io/kit/git/cmd"
)

type azureDevOpsStaticToken string

func (s azureDevOpsStaticToken) Token(context.Context) (string, error) { return string(s), nil }

const azureTestRepositoryUUID = "11111111-2222-4333-8444-555555555555"

type azureAPIFixture struct {
	srv     *Server
	syncer  *ghclient.Syncer
	repo    ghclient.RepoRef
	baseSHA string
	headSHA string
	merged  atomic.Bool
}

func newAzureAPIFixture(t *testing.T, owner, name string) *azureAPIFixture {
	t.Helper()
	remote, baseSHA, headSHA := setupAzureDevOpsCloneFixture(t)
	repositoryUUID, err := platform.ParseRepositoryUUID(azureTestRepositoryUUID)
	require.NoError(t, err)
	f := &azureAPIFixture{baseSHA: baseSHA, headSHA: headSHA, repo: ghclient.RepoRef{
		Platform: platform.KindAzureDevOps, PlatformHost: platform.DefaultAzureDevOpsHost,
		Owner: owner, Name: name, RepoPath: owner + "/" + name, CloneURL: remote,
		Key: platform.RepositoryUUIDKey(repositoryUUID),
	}}
	parts := strings.Split(owner, "/")
	repository := map[string]any{
		"id": azureTestRepositoryUUID, "name": name,
		"webUrl":        "https://dev.azure.com/" + owner + "/_git/" + name,
		"remoteUrl":     "https://dev.azure.com/" + owner + "/_git/" + name,
		"defaultBranch": "refs/heads/main",
		"project":       map[string]any{"name": parts[1], "visibility": "private", "lastUpdateTime": "2026-05-21T12:00:00Z"},
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer azure-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		basePath := "/" + owner + "/_apis/git/repositories/" + name
		pr := map[string]any{
			"pullRequestId": 17, "status": "active", "title": "Azure DevOps provider PR", "description": "Review body",
			"creationDate": "2026-05-21T12:00:00Z", "isDraft": true,
			"createdBy":     map[string]any{"displayName": "Ada Lovelace", "uniqueName": "ada@example.com"},
			"sourceRefName": "refs/heads/feature/ado", "targetRefName": "refs/heads/main", "mergeStatus": "conflicts",
			"lastMergeSourceCommit": map[string]string{"commitId": headSHA},
			"lastMergeTargetCommit": map[string]string{"commitId": baseSHA}, "repository": repository,
		}
		if f.merged.Load() {
			pr["status"], pr["mergeStatus"], pr["closedDate"] = "completed", "succeeded", "2026-05-21T12:08:00Z"
			pr["closedBy"] = map[string]string{"displayName": "Merge Bot"}
		}
		var response any
		switch r.URL.Path {
		case basePath:
			response = repository
		case basePath + "/pullRequests":
			assert.Equal(t, "active", r.URL.Query().Get("searchCriteria.status"))
			pulls := []any{}
			if !f.merged.Load() {
				pulls = append(pulls, pr)
			}
			response = map[string]any{"count": len(pulls), "value": pulls}
		case basePath + "/pullRequests/17":
			response = pr
		case basePath + "/pullRequests/17/threads":
			comments := []any{map[string]any{
				"id": 1001, "commentType": "text", "content": "Looks good from Azure DevOps",
				"publishedDate": "2026-05-21T12:05:00Z", "author": map[string]string{"displayName": "Grace Hopper"},
			}, map[string]any{
				"id": 1003, "commentType": "system", "content": "Grace Hopper approved this pull request.",
				"publishedDate": "2026-05-21T12:04:00Z", "author": map[string]string{"displayName": "Grace Hopper"},
			}}
			if f.merged.Load() {
				comments = append(comments, map[string]any{
					"id": 1002, "commentType": "system", "content": "Merged", "publishedDate": "2026-05-21T12:08:00Z",
					"author": map[string]string{"displayName": "Merge Bot"},
				})
			}
			response = map[string]any{"count": 1, "value": []any{map[string]any{"id": 55, "comments": comments}}}
		case basePath + "/pullRequests/17/iterations":
			response = map[string]any{"count": 1, "value": []any{map[string]any{
				"id": 1, "createdDate": "2026-05-21T12:02:00Z", "author": map[string]string{"displayName": "Ada Lovelace"},
				"sourceRefCommit": map[string]string{"commitId": headSHA}, "commonRefCommit": map[string]string{"commitId": baseSHA},
			}}}
		default:
			http.NotFound(w, r)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(api.Close)
	database := dbtest.Open(t)
	clones := gitclone.New(t.TempDir(), nil)
	provider, err := azuredevops.NewClient(platform.DefaultAzureDevOpsHost,
		azuredevops.WithBaseURLForTesting(api.URL), azuredevops.WithTokenSource(azureDevOpsStaticToken("azure-token")),
		azuredevops.WithCloneURLForTesting(remote))
	require.NoError(t, err)
	registry, err := platform.NewRegistry(provider)
	require.NoError(t, err)
	f.syncer = ghclient.NewSyncerWithRegistry(registry, database, clones, []ghclient.RepoRef{f.repo}, time.Minute, nil, nil)
	t.Cleanup(f.syncer.Stop)
	cfg := &config.Config{}
	cfg.Tmux.Command = []string{"sh", "-c", "exit 0"}
	f.srv = New(database, f.syncer, nil, "/", cfg, ServerOptions{Clones: clones, WorktreeDir: t.TempDir()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		require.NoError(t, f.srv.Shutdown(ctx))
	})
	f.syncer.RunOnce(t.Context())
	require.NoError(t, f.syncer.SyncMROnProvider(t.Context(), platform.KindAzureDevOps, f.repo.PlatformHost, owner, name, 17))
	return f
}

func (f *azureAPIFixture) detail(t *testing.T) pullapi.MergeRequestDetailResponse {
	t.Helper()
	raw := azureAPIRequest(t, f.srv, http.MethodGet, "/api/v1/pulls/azure_devops/"+url.PathEscape(f.repo.Owner)+"/"+url.PathEscape(f.repo.Name)+"/17", nil)
	require.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
	var detail pullapi.MergeRequestDetailResponse
	require.NoError(t, json.NewDecoder(raw.Body).Decode(&detail))
	require.NotNil(t, detail.MergeRequest)
	return detail
}

func TestAPIAzureDevOpsReadOnlySyncPersistsThroughServer(t *testing.T) {
	f := newAzureAPIFixture(t, "AcmeOrg/Payments", "Service")
	detail := f.detail(t)
	assert.Equal(t, "Azure DevOps provider PR", detail.MergeRequest.Title)
	assert.Equal(t, "dirty", detail.MergeRequest.MergeableState)
	assert.Equal(t, f.headSHA, detail.DiffHeadSHA)
	assert.Equal(t, f.baseSHA, detail.MergeBaseSHA)
	assert.True(t, detail.Repo.Capabilities.ReadRepositories)
	assert.True(t, detail.Repo.Capabilities.ReadMergeRequests)
	assert.False(t, detail.Repo.Capabilities.CommentMutation)
	require.Len(t, detail.Events, 3)
	assert.Equal(t, "issue_comment", detail.Events[0].EventType)
	assert.Equal(t, "approval", detail.Events[1].EventType)
	assert.Equal(t, "approved this pull request", detail.Events[1].Summary)
	assert.Equal(t, "iteration", detail.Events[2].EventType)
	for _, suffix := range []string{"files", "diff"} {
		raw := azureAPIRequest(t, f.srv, http.MethodGet, "/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17/"+suffix, nil)
		require.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
		var response httpapi.DiffResponse
		require.NoError(t, json.NewDecoder(raw.Body).Decode(&response))
		require.NotEmpty(t, response.Files)
		assert.Equal(t, "README.md", response.Files[0].Path)
	}
	raw := azureAPIRequest(t, f.srv, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"provider": "azure_devops", "platform_host": "dev.azure.com", "owner": f.repo.Owner, "name": f.repo.Name, "mr_number": 17,
	})
	require.Equal(t, http.StatusAccepted, raw.Code, raw.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(raw.Body).Decode(&created))
	require.Eventually(t, func() bool {
		summary, err := f.srv.workspaces.GetSummary(t.Context(), created.ID)
		return err == nil && summary != nil && summary.Status == "ready"
	}, 10*time.Second, 25*time.Millisecond)
}

func TestAPIAzureDevOpsMergedEventsAppearInDetailAndActivityAfterClosure(t *testing.T) {
	f := newAzureAPIFixture(t, "AcmeOrg/Payments", "Service")
	f.merged.Store(true)
	require.NoError(t, f.syncer.SyncMROnProvider(t.Context(), platform.KindAzureDevOps, f.repo.PlatformHost, f.repo.Owner, f.repo.Name, 17))
	detail := f.detail(t)
	assert.Equal(t, "merged", string(detail.MergeRequest.State))
	require.NotEmpty(t, detail.Events)
	assert.Equal(t, "merged", detail.Events[0].EventType)
	assert.Equal(t, "Merge Bot", detail.Events[0].Author)
	raw := azureAPIRequest(t, f.srv, http.MethodGet, "/api/v1/activity?since=2026-05-21T11%3A00%3A00Z&types=merged", nil)
	require.Equal(t, http.StatusOK, raw.Code, raw.Body.String())
	assert.Contains(t, raw.Body.String(), "Merge Bot")
}

func TestAPIAzureDevOpsSyncHandlesSpacesInRepoIdentity(t *testing.T) {
	f := newAzureAPIFixture(t, "Acme Org/Payments Team", "Space Repo")
	detail := f.detail(t)
	assert.Equal(t, "Acme Org/Payments Team", detail.Repo.Owner)
	assert.Equal(t, "Space Repo", detail.Repo.Name)
}

func azureAPIRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, req)
	return response
}

func setupAzureDevOpsCloneFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	remote, work := filepath.Join(dir, "remote.git"), filepath.Join(dir, "work")
	runAzureTestGit(t, dir, "init", "--bare", "--initial-branch=main", remote)
	runAzureTestGit(t, dir, "clone", remote, work)
	runAzureTestGit(t, work, "config", "user.email", "test@example.com")
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
	return remote, baseSHA, azureTestGitSHA(t, work, "HEAD")
}

func runAzureTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_, stderr, err := gitcmd.New().Run(t.Context(), dir, nil, args...)
	require.NoError(t, err, "git %v failed: %s", args, stderr)
}

func azureTestGitSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := gitcmd.New().Output(t.Context(), dir, "rev-parse", ref)
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
