package github

import (
	"net/url"
	"strings"

	gh "github.com/google/go-github/v89/github"
)

// WorkflowApprovalState describes whether workflow approval is needed for a PR.
type WorkflowApprovalState struct {
	Checked  bool
	Required bool
	Count    int
	RunIDs   []int64
}

// PRSource identifies a pull request's head for matching workflow runs.
// HeadSHA is required. HeadRepoFullName (the canonical repo path, such as
// "owner/repo") and HeadRef (branch name) are required to disambiguate
// fork-triggered runs whose pull_requests array is empty; without them the
// filter fails closed.
type PRSource struct {
	Number           int
	HeadSHA          string
	HeadRepoFullName string
	HeadRef          string
}

// FilterWorkflowRunsAwaitingApproval narrows action-required workflow runs to
// those that target the given PR.
//
// Matching rules:
//   - Run must be at PRSource.HeadSHA.
//   - If the run's pull_requests array is populated (same-repo PRs), it must
//     contain PRSource.Number.
//   - If pull_requests is empty (fork-triggered runs), the run's head
//     repository full name and head branch must match PRSource. Two distinct
//     fork PRs can share a head SHA, so head SHA alone is unsafe in the
//     approval path. If HeadRepoFullName or HeadRef is empty we fail closed.
func FilterWorkflowRunsAwaitingApproval(
	runs []*gh.WorkflowRun,
	pr PRSource,
) []*gh.WorkflowRun {
	var filtered []*gh.WorkflowRun
	for _, run := range runs {
		if run.GetHeadSHA() != pr.HeadSHA {
			continue
		}
		if !workflowRunMatchesPR(run, pr) {
			continue
		}
		filtered = append(filtered, run)
	}
	return filtered
}

func workflowRunMatchesPR(run *gh.WorkflowRun, pr PRSource) bool {
	if len(run.PullRequests) > 0 {
		for _, runPR := range run.PullRequests {
			if runPR.GetNumber() == pr.Number {
				return true
			}
		}
		return false
	}
	if pr.HeadRepoFullName == "" || pr.HeadRef == "" {
		return false
	}
	if run.GetHeadRepository().GetFullName() != pr.HeadRepoFullName {
		return false
	}
	if run.GetHeadBranch() != pr.HeadRef {
		return false
	}
	return true
}

// WorkflowApprovalStateFromRuns converts matched workflow runs into state.
func WorkflowApprovalStateFromRuns(runs []*gh.WorkflowRun) WorkflowApprovalState {
	state := WorkflowApprovalState{Checked: true}
	for _, run := range runs {
		state.RunIDs = append(state.RunIDs, run.GetID())
	}
	state.Count = len(state.RunIDs)
	state.Required = state.Count > 0
	return state
}

// ParseHeadRepoFullName extracts the canonical repo path from a clone URL.
// Accepts both HTTPS (https://host/owner/repo[.git]) and SSH
// (git@host:owner/repo[.git]) forms, and normalizes Azure DevOps
// /_git/ paths back to owner/project/repo.
func ParseHeadRepoFullName(cloneURL string) string {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" {
		return ""
	}
	var repoPath string
	if strings.Contains(cloneURL, "://") {
		parsed, err := url.Parse(cloneURL)
		if err != nil || parsed.Host == "" {
			return ""
		}
		repoPath = parsed.Path
	} else {
		prefix, path, ok := strings.Cut(cloneURL, ":")
		if !ok || strings.Contains(prefix, "/") {
			return ""
		}
		repoPath = path
	}
	repoPath = strings.Trim(strings.TrimSpace(repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	repoPath = strings.ReplaceAll(repoPath, "/_git/", "/")
	if repoPath == "" || !strings.Contains(repoPath, "/") || strings.Contains(repoPath, "\\") {
		return ""
	}
	return repoPath
}
