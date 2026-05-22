package azuredevops

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/platform"
)

type projectRefDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Visibility     string `json:"visibility"`
	LastUpdateTime string `json:"lastUpdateTime"`
}

type repositoryDTO struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	WebURL          string        `json:"webUrl"`
	RemoteURL       string        `json:"remoteUrl"`
	DefaultBranch   string        `json:"defaultBranch"`
	Project         projectRefDTO `json:"project"`
	IsDisabled      bool          `json:"isDisabled"`
	IsInMaintenance bool          `json:"isInMaintenance"`
}

type identityDTO struct {
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
	ID          string `json:"id"`
}

type commitRefDTO struct {
	CommitID string `json:"commitId"`
	URL      string `json:"url"`
}

type linkHrefDTO struct {
	Href string `json:"href"`
}

type pullRequestLinksDTO struct {
	Web linkHrefDTO `json:"web"`
}

type pullRequestDTO struct {
	PullRequestID         int                 `json:"pullRequestId"`
	CodeReviewID          int                 `json:"codeReviewId"`
	Status                string              `json:"status"`
	Title                 string              `json:"title"`
	Description           string              `json:"description"`
	CreationDate          string              `json:"creationDate"`
	ClosedDate            string              `json:"closedDate"`
	IsDraft               bool                `json:"isDraft"`
	CreatedBy             identityDTO         `json:"createdBy"`
	SourceRefName         string              `json:"sourceRefName"`
	TargetRefName         string              `json:"targetRefName"`
	MergeStatus           string              `json:"mergeStatus"`
	LastMergeSourceCommit *commitRefDTO       `json:"lastMergeSourceCommit"`
	LastMergeTargetCommit *commitRefDTO       `json:"lastMergeTargetCommit"`
	LastMergeCommit       *commitRefDTO       `json:"lastMergeCommit"`
	Repository            repositoryDTO       `json:"repository"`
	Links                 pullRequestLinksDTO `json:"_links"`
}

type threadDTO struct {
	ID       int64        `json:"id"`
	Comments []commentDTO `json:"comments"`
}

type commentDTO struct {
	ID              int64       `json:"id"`
	ParentCommentID int64       `json:"parentCommentId"`
	Content         string      `json:"content"`
	PublishedDate   string      `json:"publishedDate"`
	LastUpdatedDate string      `json:"lastUpdatedDate"`
	CommentType     string      `json:"commentType"`
	IsDeleted       bool        `json:"isDeleted"`
	Author          identityDTO `json:"author"`
}

type pullRequestIterationDTO struct {
	ID              int          `json:"id"`
	Description     string       `json:"description"`
	Reason          string       `json:"reason"`
	CreatedDate     string       `json:"createdDate"`
	UpdatedDate     string       `json:"updatedDate"`
	Author          identityDTO  `json:"author"`
	SourceRefCommit *commitRefDTO `json:"sourceRefCommit"`
	TargetRefCommit *commitRefDTO `json:"targetRefCommit"`
	CommonRefCommit *commitRefDTO `json:"commonRefCommit"`
}

type repoScope struct {
	Org     string
	Project string
	Repo    string
}

func parseProjectScope(owner string) (repoScope, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(owner), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return repoScope{}, &platform.Error{
			Code:       platform.ErrCodeInvalidRepoRef,
			Provider:   platform.KindAzureDevOps,
			Field:      "owner",
			Err:        fmt.Errorf("azure_devops owner must be ORG/PROJECT"),
		}
	}
	return repoScope{Org: parts[0], Project: parts[1]}, nil
}

func parseRepoScope(ref platform.RepoRef) (repoScope, error) {
	repoPath := strings.Trim(strings.TrimSpace(ref.RepoPath), "/")
	if repoPath != "" {
		parts := strings.Split(repoPath, "/")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return repoScope{}, &platform.Error{
				Code:       platform.ErrCodeInvalidRepoRef,
				Provider:   platform.KindAzureDevOps,
				Field:      "repo_path",
				Err:        fmt.Errorf("azure_devops repo_path must be ORG/PROJECT/REPO"),
			}
		}
		return repoScope{Org: parts[0], Project: parts[1], Repo: parts[2]}, nil
	}
	scope, err := parseProjectScope(ref.Owner)
	if err != nil {
		return repoScope{}, err
	}
	repo := strings.Trim(strings.TrimSpace(ref.Name), "/")
	if repo == "" || strings.Contains(repo, "/") {
		return repoScope{}, &platform.Error{
			Code:       platform.ErrCodeInvalidRepoRef,
			Provider:   platform.KindAzureDevOps,
			Field:      "name",
			Err:        fmt.Errorf("azure_devops repo name must be a single path segment"),
		}
	}
	scope.Repo = repo
	return scope, nil
}

func NormalizeRepository(host, org, requestedProject string, repo repositoryDTO) platform.Repository {
	project := strings.TrimSpace(repo.Project.Name)
	if project == "" {
		project = strings.TrimSpace(requestedProject)
	}
	name := strings.TrimSpace(repo.Name)
	owner := strings.TrimSpace(org) + "/" + project
	webURL := strings.TrimSpace(repo.WebURL)
	if webURL == "" {
		webURL = strings.TrimSpace(repo.RemoteURL)
	}
	cloneURL := strings.TrimSpace(repo.RemoteURL)
	defaultBranch := trimRefPrefix(repo.DefaultBranch)
	updatedAt := parseAzureTime(repo.Project.LastUpdateTime)
	ref := platform.RepoRef{
		Platform:           platform.KindAzureDevOps,
		Host:               host,
		Owner:              owner,
		Name:               name,
		RepoPath:           owner + "/" + name,
		PlatformExternalID: strings.TrimSpace(repo.ID),
		WebURL:             webURL,
		CloneURL:           cloneURL,
		DefaultBranch:      defaultBranch,
	}
	return platform.Repository{
		Ref:                ref,
		PlatformExternalID: ref.PlatformExternalID,
		Private:            strings.EqualFold(strings.TrimSpace(repo.Project.Visibility), "private"),
		Archived:           repo.IsDisabled || repo.IsInMaintenance,
		DefaultBranch:      defaultBranch,
		WebURL:             webURL,
		CloneURL:           cloneURL,
		UpdatedAt:          updatedAt,
	}
}

func NormalizePullRequest(host string, scope repoScope, fallback platform.RepoRef, pr pullRequestDTO) platform.MergeRequest {
	repoRef := fallback
	if strings.TrimSpace(pr.Repository.ID) != "" || strings.TrimSpace(pr.Repository.Name) != "" {
		repo := NormalizeRepository(host, scope.Org, scope.Project, pr.Repository)
		repoRef = repo.Ref
	}
	createdAt := parseAzureTime(pr.CreationDate)
	closedAt := timePtr(parseAzureTime(pr.ClosedDate))
	updatedAt := createdAt
	if closedAt != nil {
		updatedAt = *closedAt
	}
	author, authorDisplay := normalizeIdentity(pr.CreatedBy)
	state := normalizePullRequestState(pr.Status)
	mr := platform.MergeRequest{
		Repo:               repoRef,
		PlatformID:         int64(pr.PullRequestID),
		PlatformExternalID: strconv.Itoa(pr.PullRequestID),
		Number:             pr.PullRequestID,
		URL:                pullRequestWebURL(pr, repoRef),
		Title:              pr.Title,
		Author:             author,
		AuthorDisplayName:  authorDisplay,
		State:              state,
		IsDraft:            pr.IsDraft,
		Body:               pr.Description,
		HeadBranch:         trimRefPrefix(pr.SourceRefName),
		BaseBranch:         trimRefPrefix(pr.TargetRefName),
		HeadSHA:            commitID(pr.LastMergeSourceCommit),
		BaseSHA:            commitID(pr.LastMergeTargetCommit),
		HeadRepoCloneURL:   repoRef.CloneURL,
		MergeableState:     normalizeMergeStatus(pr.MergeStatus),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		LastActivityAt:     updatedAt,
	}
	if state == "merged" {
		mr.MergedAt = closedAt
	} else if state == "closed" {
		mr.ClosedAt = closedAt
	}
	return mr
}

func NormalizeMergeRequestEvents(
	repo platform.RepoRef,
	mrNumber int,
	threads []threadDTO,
) []platform.MergeRequestEvent {
	return NormalizeMergeRequestTimelineEvents(repo, mrNumber, threads, nil)
}

func NormalizeMergeRequestTimelineEvents(
	repo platform.RepoRef,
	mrNumber int,
	threads []threadDTO,
	iterations []pullRequestIterationDTO,
) []platform.MergeRequestEvent {
	events := make([]platform.MergeRequestEvent, 0, len(iterations))
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			event, ok := normalizeThreadCommentEvent(repo, mrNumber, thread, comment)
			if !ok {
				continue
			}
			events = append(events, event)
		}
	}
	iterationEvents := normalizeIterationEvents(repo, mrNumber, iterations)
	events = append(events, iterationEvents...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].DedupeKey < events[j].DedupeKey
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events
}

func normalizeThreadCommentEvent(
	repo platform.RepoRef,
	mrNumber int,
	thread threadDTO,
	comment commentDTO,
) (platform.MergeRequestEvent, bool) {
	commentType := strings.ToLower(strings.TrimSpace(comment.CommentType))
	if comment.IsDeleted {
		return platform.MergeRequestEvent{}, false
	}
	body := strings.TrimSpace(comment.Content)
	author, _ := normalizeIdentity(comment.Author)
	event := platform.MergeRequestEvent{
		Repo:               repo,
		PlatformID:         comment.ID,
		PlatformExternalID: fmt.Sprintf("%d:%d", thread.ID, comment.ID),
		MergeRequestNumber: mrNumber,
		Author:             author,
		CreatedAt:          commentTime(comment),
		DedupeKey:          fmt.Sprintf("%s:%s:%s:mr:%d:thread:%d:comment:%d", platform.KindAzureDevOps, repo.Host, repo.DisplayName(), mrNumber, thread.ID, comment.ID),
	}
	switch commentType {
	case "", "text":
		if body == "" {
			return platform.MergeRequestEvent{}, false
		}
		event.EventType = "issue_comment"
		event.Body = body
		return event, true
	case "system":
		if !isMergedSystemComment(body) {
			return platform.MergeRequestEvent{}, false
		}
		event.EventType = "merged"
		event.Summary = "Merged"
		event.Body = body
		return event, true
	default:
		return platform.MergeRequestEvent{}, false
	}
}

func isMergedSystemComment(body string) bool {
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(body)), ".!?")
	return normalized == "merged" || strings.HasPrefix(normalized, "merged ")
}

type iterationEventMetadata struct {
	IterationID    int    `json:"iteration_id"`
	Reason         string `json:"reason,omitempty"`
	Description    string `json:"description,omitempty"`
	CompareFromSHA string `json:"compare_from_sha,omitempty"`
	CompareToSHA   string `json:"compare_to_sha,omitempty"`
	SourceRefSHA   string `json:"source_ref_sha,omitempty"`
	TargetRefSHA   string `json:"target_ref_sha,omitempty"`
	CommonRefSHA   string `json:"common_ref_sha,omitempty"`
}

func normalizeIterationEvents(
	repo platform.RepoRef,
	mrNumber int,
	iterations []pullRequestIterationDTO,
) []platform.MergeRequestEvent {
	if len(iterations) == 0 {
		return nil
	}
	sorted := append([]pullRequestIterationDTO(nil), iterations...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ID == sorted[j].ID {
			return iterationTime(sorted[i]).Before(iterationTime(sorted[j]))
		}
		return sorted[i].ID < sorted[j].ID
	})
	events := make([]platform.MergeRequestEvent, 0, len(sorted))
	for i, iteration := range sorted {
		toSHA := commitID(iteration.SourceRefCommit)
		if toSHA == "" {
			continue
		}
		fromSHA := ""
		if i > 0 {
			fromSHA = commitID(sorted[i-1].SourceRefCommit)
		}
		if fromSHA == "" {
			fromSHA = commitID(iteration.CommonRefCommit)
		}
		if fromSHA == "" {
			fromSHA = commitID(iteration.TargetRefCommit)
		}
		author, _ := normalizeIdentity(iteration.Author)
		metadata, _ := json.Marshal(iterationEventMetadata{
			IterationID:    iteration.ID,
			Reason:         strings.TrimSpace(iteration.Reason),
			Description:    strings.TrimSpace(iteration.Description),
			CompareFromSHA: fromSHA,
			CompareToSHA:   toSHA,
			SourceRefSHA:   toSHA,
			TargetRefSHA:   commitID(iteration.TargetRefCommit),
			CommonRefSHA:   commitID(iteration.CommonRefCommit),
		})
		body := fmt.Sprintf("Source updated: %s -> %s", shortSHA(fromSHA), shortSHA(toSHA))
		if fromSHA == "" {
			body = fmt.Sprintf("Source updated to %s", shortSHA(toSHA))
		}
		if description := strings.TrimSpace(iteration.Description); description != "" {
			body += "\n" + description
		} else if reason := strings.TrimSpace(iteration.Reason); reason != "" {
			body += "\nReason: " + reason
		}
		events = append(events, platform.MergeRequestEvent{
			Repo:               repo,
			PlatformID:         int64(iteration.ID),
			PlatformExternalID: strconv.Itoa(iteration.ID),
			MergeRequestNumber: mrNumber,
			EventType:          "iteration",
			Author:             author,
			Summary:            fmt.Sprintf("Iteration %d", iteration.ID),
			Body:               body,
			MetadataJSON:       string(metadata),
			CreatedAt:          iterationTime(iteration),
			DedupeKey:          fmt.Sprintf("%s:%s:%s:mr:%d:iteration:%d", platform.KindAzureDevOps, repo.Host, repo.DisplayName(), mrNumber, iteration.ID),
		})
	}
	return events
}

func iterationTime(iteration pullRequestIterationDTO) time.Time {
	if t := parseAzureTime(iteration.UpdatedDate); !t.IsZero() {
		return t
	}
	return parseAzureTime(iteration.CreatedDate)
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func countCommentEvents(events []platform.MergeRequestEvent) int {
	count := 0
	for _, event := range events {
		if event.EventType == "issue_comment" {
			count++
		}
	}
	return count
}

func latestEventTime(events []platform.MergeRequestEvent) time.Time {
	var latest time.Time
	for _, event := range events {
		if event.CreatedAt.After(latest) {
			latest = event.CreatedAt
		}
	}
	return latest
}

func normalizeIdentity(identity identityDTO) (string, string) {
	author := strings.TrimSpace(identity.UniqueName)
	display := strings.TrimSpace(identity.DisplayName)
	if author == "" {
		author = display
	}
	if display == "" || display == author {
		return author, ""
	}
	return author, display
}

func normalizePullRequestState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return "open"
	case "completed":
		return "merged"
	case "abandoned":
		return "closed"
	default:
		return strings.TrimSpace(status)
	}
}

func normalizeMergeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded":
		return "clean"
	case "conflicts":
		return "dirty"
	case "rejectedbypolicy":
		return "blocked"
	case "queued", "inprogress":
		return "unknown"
	case "failure", "notset":
		return "unknown"
	default:
		return ""
	}
}

func pullRequestWebURL(pr pullRequestDTO, repo platform.RepoRef) string {
	if href := strings.TrimSpace(pr.Links.Web.Href); href != "" {
		return href
	}
	if repo.WebURL != "" {
		return strings.TrimRight(repo.WebURL, "/") + "/pullrequest/" + strconv.Itoa(pr.PullRequestID)
	}
	return ""
}

func trimRefPrefix(ref string) string {
	return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
}

func commitID(commit *commitRefDTO) string {
	if commit == nil {
		return ""
	}
	return strings.TrimSpace(commit.CommitID)
}

func commentTime(comment commentDTO) time.Time {
	if t := parseAzureTime(comment.PublishedDate); !t.IsZero() {
		return t
	}
	return parseAzureTime(comment.LastUpdatedDate)
}

func parseAzureTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
