package azuredevops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wesm/middleman/internal/platform"
	"github.com/wesm/middleman/internal/procutil"
	"github.com/wesm/middleman/internal/ratelimit"
)

const (
	defaultAPIVersion        = "7.1"
	azureDevOpsResource      = "499b84ac-1321-427f-aa17-267ca6975798"
	azureCLITokenTimeout     = 10 * time.Second
	azureCLITokenCacheTTL    = 5 * time.Minute
)

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type ClientOption func(*clientOptions)

type clientOptions struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource TokenSource
	rateTracker *ratelimit.RateTracker
}

type Client struct {
	host    string
	baseURL string
	http    *http.Client
}

type listResponse[T any] struct {
	Count int `json:"count"`
	Value []T `json:"value"`
}

type azureAPIError struct {
	Message string `json:"message"`
}

var azCommandContext = procutil.CommandContext

func WithBaseURLForTesting(baseURL string) ClientOption {
	return func(opts *clientOptions) {
		opts.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClientForTesting(client *http.Client) ClientOption {
	return func(opts *clientOptions) {
		opts.httpClient = client
	}
}

func WithTokenSource(tokenSource TokenSource) ClientOption {
	return func(opts *clientOptions) {
		opts.tokenSource = tokenSource
	}
}

func WithTokenSourceForTesting(tokenSource TokenSource) ClientOption {
	return WithTokenSource(tokenSource)
}

func WithRateTracker(rateTracker *ratelimit.RateTracker) ClientOption {
	return func(opts *clientOptions) {
		opts.rateTracker = rateTracker
	}
}

func NewClient(host string, options ...ClientOption) (*Client, error) {
	opts := clientOptions{
		baseURL:     "https://" + strings.TrimRight(host, "/"),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		tokenSource: NewCLITokenSource(),
	}
	for _, option := range options {
		option(&opts)
	}
	if opts.httpClient == nil {
		opts.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	transport := opts.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clonedClient := *opts.httpClient
	clonedClient.Transport = &authTransport{
		base:        transport,
		host:        host,
		tokenSource: opts.tokenSource,
		rateTracker: opts.rateTracker,
	}
	return &Client{
		host:    host,
		baseURL: strings.TrimRight(opts.baseURL, "/"),
		http:    &clonedClient,
	}, nil
}

func (c *Client) Platform() platform.Kind {
	return platform.KindAzureDevOps
}

func (c *Client) Host() string {
	return c.host
}

func (c *Client) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		ReadRepositories:  true,
		ReadMergeRequests: true,
		ReadComments:      true,
	}
}

func (c *Client) GetRepository(ctx context.Context, ref platform.RepoRef) (platform.Repository, error) {
	scope, err := parseRepoScope(ref)
	if err != nil {
		return platform.Repository{}, err
	}
	var repo repositoryDTO
	if err := c.requestJSON(ctx, http.MethodGet, []string{
		scope.Org, scope.Project, "_apis", "git", "repositories", scope.Repo,
	}, nil, &repo); err != nil {
		return platform.Repository{}, err
	}
	return NormalizeRepository(c.host, scope.Org, scope.Project, repo), nil
}

func (c *Client) ListRepositories(
	ctx context.Context,
	owner string,
	opts platform.RepositoryListOptions,
) ([]platform.Repository, error) {
	scope, err := parseProjectScope(owner)
	if err != nil {
		return nil, err
	}
	var response listResponse[repositoryDTO]
	if err := c.requestJSON(ctx, http.MethodGet, []string{
		scope.Org, scope.Project, "_apis", "git", "repositories",
	}, nil, &response); err != nil {
		return nil, err
	}
	repos := make([]platform.Repository, 0, len(response.Value))
	for _, repo := range response.Value {
		repos = append(repos, NormalizeRepository(c.host, scope.Org, scope.Project, repo))
	}
	return applyRepositoryListOptions(repos, opts), nil
}

func (c *Client) ListOpenMergeRequests(
	ctx context.Context,
	ref platform.RepoRef,
) ([]platform.MergeRequest, error) {
	scope, err := parseRepoScope(ref)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("searchCriteria.status", "active")
	var response listResponse[pullRequestDTO]
	if err := c.requestJSON(ctx, http.MethodGet, []string{
		scope.Org, scope.Project, "_apis", "git", "repositories", scope.Repo, "pullRequests",
	}, query, &response); err != nil {
		return nil, err
	}
	out := make([]platform.MergeRequest, 0, len(response.Value))
	for _, pr := range response.Value {
		out = append(out, NormalizePullRequest(c.host, scope, ref, pr))
	}
	return out, nil
}

func (c *Client) GetMergeRequest(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) (platform.MergeRequest, error) {
	scope, err := parseRepoScope(ref)
	if err != nil {
		return platform.MergeRequest{}, err
	}
	var pr pullRequestDTO
	if err := c.requestJSON(ctx, http.MethodGet, []string{
		scope.Org, scope.Project, "_apis", "git", "repositories", scope.Repo, "pullRequests", strconv.Itoa(number),
	}, nil, &pr); err != nil {
		return platform.MergeRequest{}, err
	}
	return NormalizePullRequest(c.host, scope, ref, pr), nil
}

func (c *Client) ListMergeRequestEvents(
	ctx context.Context,
	ref platform.RepoRef,
	number int,
) ([]platform.MergeRequestEvent, error) {
	scope, err := parseRepoScope(ref)
	if err != nil {
		return nil, err
	}
	var response listResponse[threadDTO]
	if err := c.requestJSON(ctx, http.MethodGet, []string{
		scope.Org, scope.Project, "_apis", "git", "repositories", scope.Repo, "pullRequests", strconv.Itoa(number), "threads",
	}, nil, &response); err != nil {
		return nil, err
	}
	repoRef := ref
	if strings.TrimSpace(repoRef.RepoPath) == "" {
		repoRef.Owner = scope.Org + "/" + scope.Project
		repoRef.Name = scope.Repo
		repoRef.RepoPath = repoRef.Owner + "/" + repoRef.Name
		repoRef.Platform = platform.KindAzureDevOps
		repoRef.Host = c.host
	}
	return NormalizeMergeRequestEvents(repoRef, number, response.Value), nil
}

func NewCLITokenSource() TokenSource {
	return &cachedTokenSource{
		base: azureCLITokenSource{},
		ttl:  azureCLITokenCacheTTL,
	}
}

type authTransport struct {
	base        http.RoundTripper
	host        string
	tokenSource TokenSource
	rateTracker *ratelimit.RateTracker
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenSource.Token(req.Context())
	if err != nil {
		return nil, mapTokenSourceError(t.host, err)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Accept", "application/json")
	clone.Header.Set("Authorization", "Bearer "+token)

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(clone)
	if resp == nil || t.rateTracker == nil {
		return resp, err
	}
	t.rateTracker.RecordRequest()
	if rate, ok := parseRateLimitHeaders(resp); ok {
		t.rateTracker.UpdateFromRate(rate)
	}
	return resp, err
}

func parseRateLimitHeaders(resp *http.Response) (ratelimit.Rate, bool) {
	remaining, ok := parseHeaderInt(resp, "X-RateLimit-Remaining", "RateLimit-Remaining")
	if !ok {
		return ratelimit.Rate{}, false
	}
	limit, _ := parseHeaderInt(resp, "X-RateLimit-Limit", "RateLimit-Limit")
	resetAt := time.Now().UTC().Add(time.Minute)
	if raw := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); raw != "" {
		if unix, err := strconv.ParseInt(raw, 10, 64); err == nil {
			resetAt = time.Unix(unix, 0).UTC()
		}
	}
	return ratelimit.Rate{Limit: limit, Remaining: remaining, Reset: resetAt}, true
}

func parseHeaderInt(resp *http.Response, names ...string) (int, bool) {
	for _, name := range names {
		raw := strings.TrimSpace(resp.Header.Get(name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func (c *Client) requestJSON(
	ctx context.Context,
	method string,
	segments []string,
	query url.Values,
	out any,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	clean := make([]string, 0, len(segments))
	for _, segment := range segments {
		clean = append(clean, strings.Trim(segment, "/"))
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.Join(clean, "/")
	base.RawPath = ""
	if query == nil {
		query = url.Values{}
	}
	query.Set("api-version", defaultAPIVersion)
	base.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, base.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapHTTPError(c.host, resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func mapHTTPError(host string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	message := strings.TrimSpace(string(body))
	var apiErr azureAPIError
	if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Message) != "" {
		message = strings.TrimSpace(apiErr.Message)
	}
	if message == "" {
		message = resp.Status
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &platform.Error{
			Code:         platform.ErrCodePermissionDenied,
			Provider:     platform.KindAzureDevOps,
			PlatformHost: host,
			Err:          errors.New(message),
		}
	case http.StatusNotFound:
		return &platform.Error{
			Code:         platform.ErrCodeNotFound,
			Provider:     platform.KindAzureDevOps,
			PlatformHost: host,
			Err:          errors.New(message),
		}
	case http.StatusTooManyRequests:
		return &platform.Error{
			Code:         platform.ErrCodeRateLimited,
			Provider:     platform.KindAzureDevOps,
			PlatformHost: host,
			ResetAt:      retryAfter(resp),
			Err:          errors.New(message),
		}
	default:
		return fmt.Errorf("azure devops API %s: %s", resp.Status, message)
	}
}

func retryAfter(resp *http.Response) *time.Time {
	if resp == nil {
		return nil
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		t := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
		return &t
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		t := parsed.UTC()
		return &t
	}
	return nil
}

func applyRepositoryListOptions(
	repos []platform.Repository,
	opts platform.RepositoryListOptions,
) []platform.Repository {
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.Offset >= len(repos) {
		return []platform.Repository{}
	}
	repos = repos[opts.Offset:]
	if opts.Limit <= 0 || opts.Limit >= len(repos) {
		return repos
	}
	return repos[:opts.Limit]
}

type cachedTokenSource struct {
	base TokenSource
	ttl  time.Duration

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (c *cachedTokenSource) Token(ctx context.Context) (string, error) {
	if c == nil || c.base == nil {
		return "", fmt.Errorf("missing Azure DevOps token source")
	}
	now := time.Now()
	c.mu.Lock()
	if c.token != "" && now.Before(c.expiresAt) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	token, err := c.base.Token(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token = token
	c.expiresAt = time.Now().Add(c.ttl)
	c.mu.Unlock()
	return token, nil
}

type azureCLITokenSource struct{}

func (azureCLITokenSource) Token(ctx context.Context) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, azureCLITokenTimeout)
	defer cancel()
	cmd := azCommandContext(
		cmdCtx,
		"az",
		"account",
		"get-access-token",
		"--resource",
		azureDevOpsResource,
		"--query",
		"accessToken",
		"-o",
		"tsv",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := procutil.Output(cmdCtx, cmd, "fetch Azure DevOps access token")
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("azure CLI token request failed: %s", msg)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("azure CLI returned an empty access token")
	}
	return token, nil
}

func mapTokenSourceError(host string, err error) error {
	var platformErr *platform.Error
	if errors.As(err, &platformErr) {
		return err
	}
	return &platform.Error{
		Code:         platform.ErrCodeMissingToken,
		Provider:     platform.KindAzureDevOps,
		PlatformHost: host,
		Err:          err,
	}
}

var _ platform.RepositoryReader = (*Client)(nil)
var _ platform.MergeRequestReader = (*Client)(nil)
