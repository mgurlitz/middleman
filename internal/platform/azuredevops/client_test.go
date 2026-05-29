package azuredevops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/middleman/internal/platform"
)

type staticTokenSource string

func (s staticTokenSource) Token(context.Context) (string, error) {
	return string(s), nil
}

func TestClientAttachesBearerTokenFromTokenSource(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"repo-1","name":"widgets","webUrl":"https://dev.azure.com/acme/platform/_git/widgets","remoteUrl":"https://dev.azure.com/acme/platform/_git/widgets","defaultBranch":"refs/heads/main","project":{"name":"platform","visibility":"private"}}`))
	}))
	defer server.Close()

	client, err := NewClient(
		"dev.azure.com",
		WithBaseURLForTesting(server.URL),
		WithTokenSourceForTesting(staticTokenSource("azure-token")),
	)
	require.NoError(err)

	repo, err := client.GetRepository(t.Context(), platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "acme/platform",
		Name:     "widgets",
		RepoPath: "acme/platform/widgets",
	})
	require.NoError(err)
	assert.Equal("Bearer azure-token", authHeader)
	assert.Equal("acme/platform/widgets", repo.Ref.RepoPath)
}

func TestClientGetRepositoryEncodesAzurePathSegmentsWithSpacesOnce(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"repo-1","name":"Space Repo","webUrl":"https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo","remoteUrl":"https://dev.azure.com/Acme%20Org/Payments%20Team/_git/Space%20Repo","defaultBranch":"refs/heads/main","project":{"name":"Payments Team","visibility":"private"}}`))
	}))
	defer server.Close()

	client, err := NewClient(
		"dev.azure.com",
		WithBaseURLForTesting(server.URL),
		WithTokenSourceForTesting(staticTokenSource("azure-token")),
	)
	require.NoError(err)

	repo, err := client.GetRepository(t.Context(), platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "Acme Org/Payments Team",
		Name:     "Space Repo",
		RepoPath: "Acme Org/Payments Team/Space Repo",
	})
	require.NoError(err)
	assert.Equal("/Acme%20Org/Payments%20Team/_apis/git/repositories/Space%20Repo", escapedPath)
	assert.Equal("Acme Org/Payments Team/Space Repo", repo.Ref.RepoPath)
}

func TestAzureCLITokenSourceUsesAzureCLICommand(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv")
	azPath := filepath.Join(dir, "az")
	if runtime.GOOS == "windows" {
		azPath += ".cmd"
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$FAKE_AZ_ARGV\"\n" +
		"printf '%s\\n' 'cli-token'\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n" +
			"echo %*>>\"%FAKE_AZ_ARGV%\"\r\n" +
			"echo cli-token\r\n"
	}
	require.NoError(os.WriteFile(azPath, []byte(script), 0o755))
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_AZ_ARGV", argvPath)

	token, err := (azureCLITokenSource{}).Token(context.Background())
	require.NoError(err)
	assert.Equal("cli-token", token)

	data, err := os.ReadFile(argvPath)
	require.NoError(err)
	assert.Contains(string(data), "account get-access-token --resource "+azureDevOpsResource+" --query accessToken -o tsv")
}

func TestAzureCLITokenSourceDoesNotLeakStdoutTokenOnFailure(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	azPath := filepath.Join(dir, "az")
	if runtime.GOOS == "windows" {
		azPath += ".cmd"
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' 'secret-token'\n" +
		"printf '%s\\n' 'login expired' 1>&2\n" +
		"exit 1\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n" +
			"echo secret-token\r\n" +
			">&2 echo login expired\r\n" +
			"exit /b 1\r\n"
	}
	require.NoError(os.WriteFile(azPath, []byte(script), 0o755))
	t.Setenv("PATH", dir)

	_, err := (azureCLITokenSource{}).Token(context.Background())
	require.Error(err)
	assert.Contains(err.Error(), "login expired")
	assert.NotContains(err.Error(), "secret-token")
}

func TestNewCLITokenSourceCachesAccessTokenInMemory(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv")
	azPath := filepath.Join(dir, "az")
	if runtime.GOOS == "windows" {
		azPath += ".cmd"
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$FAKE_AZ_ARGV\"\n" +
		"printf '%s\\n' 'cached-token'\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\n" +
			"echo %*>>\"%FAKE_AZ_ARGV%\"\r\n" +
			"echo cached-token\r\n"
	}
	require.NoError(os.WriteFile(azPath, []byte(script), 0o755))
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_AZ_ARGV", argvPath)

	source := NewCLITokenSource()
	first, err := source.Token(context.Background())
	require.NoError(err)
	second, err := source.Token(context.Background())
	require.NoError(err)
	assert.Equal("cached-token", first)
	assert.Equal("cached-token", second)

	data, err := os.ReadFile(argvPath)
	require.NoError(err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(lines, 1)
}

func TestClientCapabilitiesExposeReadOnlyAzureDevOpsPOC(t *testing.T) {
	client, err := NewClient(
		"dev.azure.com",
		WithBaseURLForTesting("http://127.0.0.1"),
		WithTokenSourceForTesting(staticTokenSource("token")),
	)
	require.NoError(t, err)
	assert.Equal(t, platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadComments:      true,
	}, client.Capabilities())
}

func TestNormalizeRepositoryMapsAzureProjectIdentity(t *testing.T) {
	repo := NormalizeRepository("dev.azure.com", "AcmeOrg", "Payments", repositoryDTO{
		ID:            "repo-guid",
		Name:          "Service",
		WebURL:        "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
		RemoteURL:     "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
		DefaultBranch: "refs/heads/main",
		Project: projectRefDTO{
			Name:           "Payments",
			Visibility:     "private",
			LastUpdateTime: "2026-05-21T10:15:00Z",
		},
	})

	assert.Equal(t, platform.KindAzureDevOps, repo.Ref.Platform)
	assert.Equal(t, "AcmeOrg/Payments", repo.Ref.Owner)
	assert.Equal(t, "Service", repo.Ref.Name)
	assert.Equal(t, "AcmeOrg/Payments/Service", repo.Ref.RepoPath)
	assert.Equal(t, "repo-guid", repo.PlatformExternalID)
	assert.Equal(t, "main", repo.DefaultBranch)
	assert.True(t, repo.Private)
	assert.Equal(t, time.Date(2026, 5, 21, 10, 15, 0, 0, time.UTC), repo.UpdatedAt)
}

func TestNormalizePullRequestMapsAzureFieldsAndThreads(t *testing.T) {
	ref := platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "AcmeOrg/Payments",
		Name:     "Service",
		RepoPath: "AcmeOrg/Payments/Service",
		CloneURL: "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
	}
	scope := repoScope{Org: "AcmeOrg", Project: "Payments", Repo: "Service"}
	mr := NormalizePullRequest("dev.azure.com", scope, ref, pullRequestDTO{
		PullRequestID:  17,
		Status:         "active",
		Title:          "Wire Azure DevOps provider",
		Description:    "POC body",
		CreationDate:   "2026-05-21T11:00:00Z",
		IsDraft:        true,
		CreatedBy:      identityDTO{DisplayName: "Ada Lovelace", UniqueName: "ada@example.com"},
		SourceRefName:  "refs/heads/feature/ado",
		TargetRefName:  "refs/heads/main",
		MergeStatus:    "conflicts",
		LastMergeSourceCommit: &commitRefDTO{CommitID: "abc123"},
		LastMergeTargetCommit: &commitRefDTO{CommitID: "def456"},
		Links:          pullRequestLinksDTO{Web: linkHrefDTO{Href: "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17"}},
	})
	assert.Equal(t, 17, mr.Number)
	assert.Equal(t, "open", mr.State)
	assert.True(t, mr.IsDraft)
	assert.Equal(t, "feature/ado", mr.HeadBranch)
	assert.Equal(t, "main", mr.BaseBranch)
	assert.Equal(t, "dirty", mr.MergeableState)
	assert.Equal(t, "ada@example.com", mr.Author)
	assert.Equal(t, "Ada Lovelace", mr.AuthorDisplayName)
	assert.Equal(t, "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17", mr.URL)
	assert.Equal(t, "abc123", mr.HeadSHA)
	assert.Equal(t, "def456", mr.BaseSHA)

	events := NormalizeMergeRequestEvents(ref, 17, []threadDTO{{
		ID: 55,
		Comments: []commentDTO{
			{ID: 1001, CommentType: "text", Content: "Looks good", PublishedDate: "2026-05-21T12:00:00Z", Author: identityDTO{DisplayName: "Grace Hopper"}},
			{ID: 1002, CommentType: "system", Content: "ignored", PublishedDate: "2026-05-21T12:01:00Z", Author: identityDTO{DisplayName: "Bot"}},
		},
	}})
	require.Len(t, events, 1)
	assert.Equal(t, "issue_comment", events[0].EventType)
	assert.Equal(t, "Grace Hopper", events[0].Author)
	assert.Equal(t, "Looks good", events[0].Body)
	assert.Equal(t, "azure_devops:dev.azure.com:AcmeOrg/Payments/Service:mr:17:thread:55:comment:1001", events[0].DedupeKey)
	assert.Equal(t, 1, countCommentEvents(events))
	assert.Equal(t, time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC), latestEventTime(events))
}

func TestNormalizeMergeRequestTimelineEventsIncludesMergedSystemEvents(t *testing.T) {
	ref := platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "AcmeOrg/Payments",
		Name:     "Service",
		RepoPath: "AcmeOrg/Payments/Service",
	}

	events := NormalizeMergeRequestTimelineEvents(ref, 17, []threadDTO{{
		ID: 55,
		Comments: []commentDTO{
			{ID: 1001, CommentType: "text", Content: "Looks good", PublishedDate: "2026-05-21T12:00:00Z", Author: identityDTO{DisplayName: "Grace Hopper"}},
			{ID: 1002, CommentType: "system", Content: "Merged", PublishedDate: "2026-05-21T12:06:00Z", Author: identityDTO{DisplayName: "Merge Bot"}},
		},
	}}, nil)

	require.Len(t, events, 2)
	assert.Equal(t, "issue_comment", events[0].EventType)
	assert.Equal(t, "merged", events[1].EventType)
	assert.Equal(t, "Merged", events[1].Summary)
	assert.Equal(t, "Merged", events[1].Body)
	assert.Equal(t, "Merge Bot", events[1].Author)
	assert.Equal(t, "azure_devops:dev.azure.com:AcmeOrg/Payments/Service:mr:17:thread:55:comment:1002", events[1].DedupeKey)
	assert.Equal(t, time.Date(2026, 5, 21, 12, 6, 0, 0, time.UTC), latestEventTime(events))
}

func TestNormalizeMergeRequestTimelineEventsAddsIterationEvents(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	ref := platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "AcmeOrg/Payments",
		Name:     "Service",
		RepoPath: "AcmeOrg/Payments/Service",
	}
	iterations := []pullRequestIterationDTO{
		{
			ID:              1,
			CreatedDate:     "2026-05-21T11:45:00Z",
			Author:          identityDTO{DisplayName: "Ada Lovelace", UniqueName: "ada@example.com"},
			SourceRefCommit: &commitRefDTO{CommitID: "1111111222222233333334444444555555566666"},
			TargetRefCommit: &commitRefDTO{CommitID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			CommonRefCommit: &commitRefDTO{CommitID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		{
			ID:              2,
			UpdatedDate:     "2026-05-21T12:10:00Z",
			Reason:          "push",
			Author:          identityDTO{DisplayName: "Ada Lovelace", UniqueName: "ada@example.com"},
			SourceRefCommit: &commitRefDTO{CommitID: "7777777888888899999990000000111111122222"},
			TargetRefCommit: &commitRefDTO{CommitID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			CommonRefCommit: &commitRefDTO{CommitID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}

	events := NormalizeMergeRequestTimelineEvents(ref, 17, []threadDTO{ {
		ID: 55,
		Comments: []commentDTO{{
			ID:            1001,
			CommentType:   "text",
			Content:       "Looks good",
			PublishedDate: "2026-05-21T12:00:00Z",
			Author:        identityDTO{DisplayName: "Grace Hopper"},
		}},
	}}, iterations)
	require.Len(events, 3)
	assert.Equal("iteration", events[0].EventType)
	assert.Equal("Iteration 1", events[0].Summary)
	assert.Equal("ada@example.com", events[0].Author)
	assert.Equal("Source updated: aaaaaaa -> 1111111", events[0].Body)

	assert.Equal("issue_comment", events[1].EventType)
	assert.Equal("iteration", events[2].EventType)
	assert.Equal("Iteration 2", events[2].Summary)
	assert.Equal("Source updated: 1111111 -> 7777777\nReason: push", events[2].Body)
	assert.Equal("azure_devops:dev.azure.com:AcmeOrg/Payments/Service:mr:17:iteration:2", events[2].DedupeKey)
	assert.Equal(time.Date(2026, 5, 21, 12, 10, 0, 0, time.UTC), latestEventTime(events))

	var metadata map[string]any
	require.NoError(json.Unmarshal([]byte(events[2].MetadataJSON), &metadata))
	assert.Equal(float64(2), metadata["iteration_id"])
	assert.Equal("1111111222222233333334444444555555566666", metadata["compare_from_sha"])
	assert.Equal("7777777888888899999990000000111111122222", metadata["compare_to_sha"])
}

func TestParseRepoScopeRejectsInvalidAzureOwnerShape(t *testing.T) {
	_, err := parseRepoScope(platform.RepoRef{
		Platform: platform.KindAzureDevOps,
		Host:     "dev.azure.com",
		Owner:    "just-org",
		Name:     "repo",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, platform.ErrInvalidRepoRef)
	assert.True(t, strings.Contains(err.Error(), "invalid_repo_ref") || strings.Contains(err.Error(), "azure_devops owner"))
}
