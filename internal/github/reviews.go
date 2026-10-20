package github

import (
	"context"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/shurcooL/githubv4"
)

// copilotReviewerLogin is the GitHub App login for Copilot code reviews.
const copilotReviewerLogin = "copilot-pull-request-reviewer"

// maxCopilotQueryPages bounds the pagination walk (review of #535): a PR
// whose newest Copilot review is stale and no HEAD review exists walks
// every review page backward — and repeats the walk on every poll cycle,
// a recurring API cost that grows with PR age. Ten pages spans 250
// reviews and 100 requested reviewers; past that, give up on deeper
// history and parse what was gathered rather than risk unbounded walks.
const maxCopilotQueryPages = 10

// CopilotReview represents the state of a GitHub Copilot code review on a PR.
// Unlike CheckRunInfo, reviews have no startedAt/completedAt timestamps, so
// they must not participate in timing calculations.
type CopilotReview struct {
	State        string // lowercase review state: approved, changes_requested, commented, dismissed, pending
	CommitOID    string // the commit the review targets (empty if no review)
	Stale        bool   // true if a review exists but targets a different commit than headSHA
	Pending      bool   // true if Copilot review is requested but not yet submitted
	NotRequested bool   // true if no Copilot review request and no review found
}

// CopilotReviewFails returns true if the review state indicates the PR should
// not merge (changes_requested). This is deliberately separate from
// FailureConclusion to avoid overloading check-run conclusion semantics.
func CopilotReviewFails(state string) bool {
	return state == "changes_requested"
}

// requestNode represents a single requested reviewer from the
// reviewRequests connection. The ... on Bot / ... on User inline fragments
// matter because GitHub silently drops Bot reviewers from the REST
// requested_reviewers endpoint (issue #288); GraphQL surfaces them.
type requestNode struct {
	RequestedReviewer struct {
		Typename string `graphql:"__typename"`
		Bot      struct {
			Login string
		} `graphql:"... on Bot"`
		User struct {
			Login string
		} `graphql:"... on User"`
	}
}

// reviewNode represents a single submitted review from the reviews
// connection.
type reviewNode struct {
	Author struct {
		Login string
	}
	State       string
	SubmittedAt githubv4.DateTime
	Commit      struct {
		OID string
	}
	Body string
}

// copilotReviewQuery fetches Copilot review requests and submitted reviews
// via a second GraphQL path alongside the check-runs query. Both
// connections are cursor-paginated (issue #486) so a PR with more than 25
// reviews or more than 10 requested reviewers cannot hide Copilot state:
// reviewRequests walks forward (first: 10, after: $requestsCursor) and
// reviews walks backward (last: 25, before: $reviewsCursor), mirroring the
// contexts pagination in graphql.go. PRs whose data fits on the first page
// of both connections still cost a single round trip.
type copilotReviewQuery struct {
	Repository struct {
		PullRequest struct {
			ReviewRequests struct {
				Nodes    []requestNode
				PageInfo struct {
					HasNextPage bool
					EndCursor   githubv4.String
				}
			} `graphql:"reviewRequests(first: 10, after: $requestsCursor)"`
			Reviews struct {
				Nodes    []reviewNode
				PageInfo struct {
					HasPreviousPage bool
					StartCursor     githubv4.String
				}
			} `graphql:"reviews(last: 25, before: $reviewsCursor)"`
		} `graphql:"pullRequest(number: $prNumber)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Limit     int
		Remaining int
	}
}

// FetchCopilotReview fetches the Copilot code review state for a PR,
// comparing the review's commit OID against headSHA to detect stale reviews.
// Returns the review state, the GraphQL rate limit remaining, and any error.
// The host selects the GraphQL endpoint ("" or github.com = public; else
// enterprise).
//
// Both connections paginate (issue #486): reviewRequests walks forward until
// the Copilot request is found or the connection is exhausted, and reviews
// walks backward until a Copilot review targeting HEAD is found or the
// connection is exhausted. A PR with more than 25 reviews after the newest
// Copilot review, or more than 10 requested reviewers, can therefore no
// longer hide Copilot state. The walk is bounded by maxCopilotQueryPages so
// a PR with deep non-Copilot review history cannot drive unbounded API
// traffic on every poll cycle.
func FetchCopilotReview(ctx context.Context, token, host, owner, repo string, prNumber int, headSHA string) (CopilotReview, int, error) {
	httpClient := newAuthenticatedHTTPClient(ctx, token)
	client := newGraphQLClient(host, httpClient)
	return fetchCopilotReview(ctx, client, owner, repo, prNumber, headSHA)
}

// containsCopilotRequest reports whether any requested reviewer on a page
// is the Copilot app.
func containsCopilotRequest(nodes []requestNode) bool {
	for _, req := range nodes {
		login := req.RequestedReviewer.Bot.Login
		if login == "" {
			login = req.RequestedReviewer.User.Login
		}
		if strings.EqualFold(login, copilotReviewerLogin) {
			return true
		}
	}
	return false
}

// containsCopilotHeadReview reports whether any review on a page is
// authored by Copilot and targets headSHA.
func containsCopilotHeadReview(nodes []reviewNode, headSHA string) bool {
	for _, node := range nodes {
		if strings.EqualFold(node.Author.Login, copilotReviewerLogin) && node.Commit.OID == headSHA {
			return true
		}
	}
	return false
}

func fetchCopilotReview(ctx context.Context, client graphqlQuerier, owner, repo string, prNumber int, headSHA string) (CopilotReview, int, error) {
	prNum, err := safeGraphQLInt(prNumber)
	if err != nil {
		return CopilotReview{}, UnknownRateLimit, err
	}

	var allRequests []requestNode
	var allReviews []reviewNode
	var requestsCursor, reviewsCursor *githubv4.String
	requestsDone, reviewsDone := false, false
	rateLimitRemaining := UnknownRateLimit

	for pages := 1; ; pages++ {
		var query copilotReviewQuery
		variables := map[string]any{
			"owner":    githubv4.String(owner),
			"repo":     githubv4.String(repo),
			"prNumber": prNum,
		}
		if requestsCursor != nil {
			variables["requestsCursor"] = *requestsCursor
		} else {
			variables["requestsCursor"] = (*githubv4.String)(nil)
		}
		if reviewsCursor != nil {
			variables["reviewsCursor"] = *reviewsCursor
		} else {
			variables["reviewsCursor"] = (*githubv4.String)(nil)
		}

		if err := client.Query(ctx, &query, variables); err != nil {
			debug.Log("copilot review query failed", "owner", owner, "repo", repo, "pr", prNumber, "err", err)
			// Partial state is discarded on purpose: pages gathered before
			// the failure are an incomplete window (the walk had not yet
			// reached the page that could hold the Copilot review), so a
			// state derived from them could answer "not requested" when
			// the unfetched page held the opposite. Callers treat fetch
			// errors as transient and the next poll refetches from page 1.
			return CopilotReview{}, rateLimitRemaining, err
		}

		debug.Log("copilot review query success", "owner", owner, "repo", repo, "pr", prNumber,
			"rate_limit_remaining", query.RateLimit.Remaining,
			"review_requests", len(query.Repository.PullRequest.ReviewRequests.Nodes),
			"reviews", len(query.Repository.PullRequest.Reviews.Nodes))

		// Normalize per page: an enterprise host with rate limiting
		// disabled returns null here (zero-value struct), which must not
		// look like a "0 remaining" quota (see normalizeRateLimit). Keep
		// the minimum across pages, mirroring fetchCheckRunsGraphQL.
		remaining := normalizeRateLimit(query.RateLimit.Limit, query.RateLimit.Remaining)
		if remaining < rateLimitRemaining {
			rateLimitRemaining = remaining
		}

		pr := query.Repository.PullRequest

		// Requests walk forward (oldest page first), so append preserves
		// connection order. Stop once the Copilot request is in hand;
		// remaining pages cannot change the requested/not-requested
		// outcome.
		if !requestsDone {
			allRequests = append(allRequests, pr.ReviewRequests.Nodes...)
			if containsCopilotRequest(pr.ReviewRequests.Nodes) {
				requestsDone = true
			} else if pr.ReviewRequests.PageInfo.HasNextPage {
				cursor := pr.ReviewRequests.PageInfo.EndCursor
				requestsCursor = &cursor
			} else {
				requestsDone = true
			}
		}

		// Reviews walk backward (newest page first), so prepend each page
		// to keep the accumulated slice in ascending submission order —
		// parseCopilotReview's newest-wins reverse iteration relies on it.
		// Stop once a Copilot review targeting HEAD is in hand: pages
		// further back are strictly older, so they cannot change which
		// Copilot review is the newest one on HEAD. Keep walking past
		// stale (non-HEAD) Copilot reviews, though — an older review may
		// still target HEAD (e.g. a force-push back to a previous commit).
		if !reviewsDone {
			allReviews = append(pr.Reviews.Nodes, allReviews...)
			if containsCopilotHeadReview(pr.Reviews.Nodes, headSHA) {
				reviewsDone = true
			} else if pr.Reviews.PageInfo.HasPreviousPage {
				cursor := pr.Reviews.PageInfo.StartCursor
				reviewsCursor = &cursor
			} else {
				reviewsDone = true
			}
		}

		// A connection that is done keeps its cursor pinned for any
		// remaining round trips the other connection still needs; its
		// re-fetched pages are simply not accumulated. The re-fetch is a
		// known, accepted cost: both connections share one query document,
		// so skipping a done connection would require a second,
		// dynamically built query — more complexity than the spare page
		// is worth (review of #535).

		if reviewsDone && requestsDone {
			break
		}
		if pages >= maxCopilotQueryPages {
			debug.Log("copilot review pagination hit page cap",
				"owner", owner, "repo", repo, "pr", prNumber,
				"pages", pages, "max_pages", maxCopilotQueryPages)
			break
		}
	}

	var result copilotReviewQuery
	result.Repository.PullRequest.ReviewRequests.Nodes = allRequests
	result.Repository.PullRequest.Reviews.Nodes = allReviews
	review := parseCopilotReview(&result, headSHA)
	return review, rateLimitRemaining, nil
}

// parseCopilotReview extracts the Copilot review state from a GraphQL response,
// applying the HEAD-oid completion criterion and stale detection from
// template-repo's wait_for_copilot.sh.
func parseCopilotReview(query *copilotReviewQuery, headSHA string) CopilotReview {
	review := CopilotReview{}

	// Check review requests for Copilot
	copilotRequested := false
	for _, req := range query.Repository.PullRequest.ReviewRequests.Nodes {
		login := req.RequestedReviewer.Bot.Login
		if login == "" {
			login = req.RequestedReviewer.User.Login
		}
		if strings.EqualFold(login, copilotReviewerLogin) {
			copilotRequested = true
			break
		}
	}

	// Scan reviews for the latest Copilot-authored review targeting HEAD
	var headReview *reviewNode

	sawStale := false
	// The reviews connection returns nodes in chronological (ascending)
	// submission order — oldest first, newest last — whether fetched via
	// `reviews(first: N)` or `reviews(last: N)`. fetchCopilotReview walks
	// backward pages and prepends them, so the accumulated slice here is a
	// contiguous ascending window ending at the newest review; iterate in
	// reverse so the first HEAD-matching node encountered is the most
	// recent review on that commit.
	for i := len(query.Repository.PullRequest.Reviews.Nodes) - 1; i >= 0; i-- {
		node := &query.Repository.PullRequest.Reviews.Nodes[i]
		if !strings.EqualFold(node.Author.Login, copilotReviewerLogin) {
			continue
		}

		// Track any Copilot review that doesn't target HEAD as stale
		if node.Commit.OID != headSHA {
			sawStale = true
			continue
		}

		// Newest review targeting HEAD (reverse iteration means the
		// first match is the latest submission on that commit).
		if headReview == nil {
			headReview = node
		}
	}

	switch {
	case headReview != nil:
		review.State = strings.ToLower(headReview.State)
		review.CommitOID = headReview.Commit.OID
		// Complete (state is APPROVED/CHANGES_REQUESTED/COMMENTED/DISMISSED),
		// not PENDING — a PENDING review on HEAD means it's still in progress.
		if review.State == "pending" {
			review.Pending = true
		}
	case copilotRequested:
		// Review requested but no submitted review on HEAD yet. A fresh
		// review is actively pending, so do NOT set Stale even if older
		// reviews from previous commits are present — the pending review
		// takes precedence over the stale record.
		review.Pending = true
		review.State = "pending"
	case sawStale:
		// No HEAD review and no pending request, but a stale one exists —
		// mark stale so the caller can warn the user to re-request.
		review.Stale = true
		review.NotRequested = true
	default:
		review.NotRequested = true
	}

	return review
}
