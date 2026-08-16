package github

import (
	"context"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// copilotReviewerLogin is the GitHub App login for Copilot code reviews.
const copilotReviewerLogin = "copilot-pull-request-reviewer"

// CopilotReview represents the state of a GitHub Copilot code review on a PR.
// Unlike CheckRunInfo, reviews have no startedAt/completedAt timestamps, so
// they must not participate in timing calculations.
type CopilotReview struct {
	State        string    // lowercase review state: approved, changes_requested, commented, dismissed, pending
	SubmittedAt  time.Time // zero if pending or no review found
	CommitOID    string    // the commit the review targets (empty if no review)
	Stale        bool      // true if a review exists but targets a different commit than headSHA
	Pending      bool      // true if Copilot review is requested but not yet submitted
	NotRequested bool      // true if no Copilot review request and no review found
}

// CopilotReviewFails returns true if the review state indicates the PR should
// not merge (changes_requested). This is deliberately separate from
// FailureConclusion to avoid overloading check-run conclusion semantics.
func CopilotReviewFails(state string) bool {
	return state == "changes_requested"
}

// copilotReviewQuery fetches Copilot review requests and submitted reviews
// via a second GraphQL path alongside the check-runs query. The
// reviewRequests fragment uses ... on Bot / ... on User because GitHub
// silently drops Bot reviewers from the REST requested_reviewers endpoint
// (issue #288), but GraphQL surfaces them via inline fragments.
type copilotReviewQuery struct {
	Repository struct {
		PullRequest struct {
			ReviewRequests struct {
				Nodes []struct {
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
			} `graphql:"reviewRequests(first: 10)"`
			Reviews struct {
				Nodes []struct {
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
			} `graphql:"reviews(first: 25)"`
		} `graphql:"pullRequest(number: $prNumber)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Remaining int
	}
}

// FetchCopilotReview fetches the Copilot code review state for a PR,
// comparing the review's commit OID against headSHA to detect stale reviews.
// Returns the review state, the GraphQL rate limit remaining, and any error.
func FetchCopilotReview(ctx context.Context, token, owner, repo string, prNumber int, headSHA string) (CopilotReview, int, error) {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(ctx, src)
	client := githubv4.NewClient(httpClient)
	return fetchCopilotReview(ctx, client, owner, repo, prNumber, headSHA)
}

func fetchCopilotReview(ctx context.Context, client graphqlQuerier, owner, repo string, prNumber int, headSHA string) (CopilotReview, int, error) {
	var query copilotReviewQuery
	variables := map[string]any{
		"owner":    githubv4.String(owner),
		"repo":     githubv4.String(repo),
		"prNumber": githubv4.Int(prNumber),
	}

	if err := client.Query(ctx, &query, variables); err != nil {
		debug.Log("copilot review query failed", "owner", owner, "repo", repo, "pr", prNumber, "err", err)
		return CopilotReview{}, 5000, err
	}

	debug.Log("copilot review query success", "owner", owner, "repo", repo, "pr", prNumber,
		"rate_limit_remaining", query.RateLimit.Remaining,
		"review_requests", len(query.Repository.PullRequest.ReviewRequests.Nodes),
		"reviews", len(query.Repository.PullRequest.Reviews.Nodes))

	review := parseCopilotReview(&query, headSHA)
	return review, query.RateLimit.Remaining, nil
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
	var headReview *struct {
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

	sawStale := false
	for i := range query.Repository.PullRequest.Reviews.Nodes {
		node := &query.Repository.PullRequest.Reviews.Nodes[i]
		if !strings.EqualFold(node.Author.Login, copilotReviewerLogin) {
			continue
		}

		// Track any Copilot review that doesn't target HEAD as stale
		if node.Commit.OID != headSHA {
			sawStale = true
			continue
		}

		// Latest review targeting HEAD (reviews are first:25, newest first)
		if headReview == nil {
			headReview = node
		}
	}

	switch {
	case headReview != nil:
		review.State = strings.ToLower(headReview.State)
		review.CommitOID = headReview.Commit.OID
		if !headReview.SubmittedAt.IsZero() {
			review.SubmittedAt = headReview.SubmittedAt.Time
		}
		// Complete (state is APPROVED/CHANGES_REQUESTED/COMMENTED/DISMISSED),
		// not PENDING — a PENDING review on HEAD means it's still in progress.
		if review.State == "pending" {
			review.Pending = true
		}
	case copilotRequested:
		// Review requested but no submitted review on HEAD yet
		review.Pending = true
		review.State = "pending"
	case sawStale:
		// No HEAD review, but a stale one exists — mark stale so the caller
		// can warn the user to re-request.
		review.Stale = true
		review.NotRequested = true
	default:
		review.NotRequested = true
	}

	// If we found a HEAD review but also saw stale ones, the stale flag
	// still matters for the warning. But if HEAD review is complete,
	// staleness is informational only — set it but don't block.
	if sawStale && headReview == nil {
		review.Stale = true
	}

	return review
}