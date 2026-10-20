package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

type mockReviewResponse struct {
	query *copilotReviewQuery
	err   error
}

type mockReviewQuerier struct {
	responses []mockReviewResponse
	callCount int
	// variablesByCall captures the variables map of each Query call so
	// pagination tests can assert cursor plumbing.
	variablesByCall []map[string]interface{}
}

func (m *mockReviewQuerier) Query(_ context.Context, q interface{}, variables map[string]interface{}) error {
	if m.callCount >= len(m.responses) {
		return fmt.Errorf("unexpected query call #%d", m.callCount+1)
	}
	m.variablesByCall = append(m.variablesByCall, variables)
	resp := m.responses[m.callCount]
	m.callCount++
	if resp.err != nil {
		return resp.err
	}
	target := q.(*copilotReviewQuery)
	*target = *resp.query
	return nil
}

func makeReviewNode(authorLogin, state, commitOID, submittedAt string) reviewNode {
	var submittedAtTime time.Time
	if submittedAt != "" {
		if t, err := time.Parse(time.RFC3339, submittedAt); err == nil {
			submittedAtTime = t
		}
	}
	return reviewNode{
		Author:      struct{ Login string }{Login: authorLogin},
		State:       state,
		SubmittedAt: githubv4.DateTime{Time: submittedAtTime},
		Commit:      struct{ OID string }{OID: commitOID},
	}
}

func makeRequestNode(login string) requestNode {
	return requestNode{
		RequestedReviewer: struct {
			Typename string `graphql:"__typename"`
			Bot      struct {
				Login string
			} `graphql:"... on Bot"`
			User struct {
				Login string
			} `graphql:"... on User"`
		}{
			Typename: "Bot",
			Bot:      struct{ Login string }{Login: login},
		},
	}
}

func makeReviewQuery(requests []requestNode, reviews []reviewNode, rateLimit int) *copilotReviewQuery {
	q := &copilotReviewQuery{
		RateLimit: struct {
			Limit     int
			Remaining int
		}{Limit: 5000, Remaining: rateLimit},
	}
	q.Repository.PullRequest.ReviewRequests.Nodes = requests
	q.Repository.PullRequest.Reviews.Nodes = reviews
	return q
}

// requestPageInfo and reviewPageInfo configure one mock page's connection
// cursors so pagination tests can drive fetchCopilotReview's page walks.
type requestPageInfo struct {
	hasNextPage bool
	endCursor   string
}

type reviewPageInfo struct {
	hasPreviousPage bool
	startCursor     string
}

// makeReviewQueryPaged builds a single-page response with explicit PageInfo.
func makeReviewQueryPaged(requests []requestNode, reqPage requestPageInfo, reviews []reviewNode, revPage reviewPageInfo, rateLimit int) *copilotReviewQuery {
	q := makeReviewQuery(requests, reviews, rateLimit)
	q.Repository.PullRequest.ReviewRequests.PageInfo.HasNextPage = reqPage.hasNextPage
	q.Repository.PullRequest.ReviewRequests.PageInfo.EndCursor = githubv4.String(reqPage.endCursor)
	q.Repository.PullRequest.Reviews.PageInfo.HasPreviousPage = revPage.hasPreviousPage
	q.Repository.PullRequest.Reviews.PageInfo.StartCursor = githubv4.String(revPage.startCursor)
	return q
}

// humanReviews returns n human-authored COMMENTED reviews targeting the
// given commit, for padding the reviews connection past a page boundary.
func humanReviews(n int, commitOID string) []reviewNode {
	nodes := make([]reviewNode, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, makeReviewNode("some-user", "COMMENTED", commitOID, ""))
	}
	return nodes
}

// humanRequests returns n human requested-reviewer nodes, for padding the
// reviewRequests connection past a page boundary.
func humanRequests(n int) []requestNode {
	nodes := make([]requestNode, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, makeRequestNode(fmt.Sprintf("human-%d", i)))
	}
	return nodes
}

func TestFetchCopilotReview(t *testing.T) {
	headSHA := "abc123def456"

	tests := []struct {
		name             string
		query            *copilotReviewQuery
		err              error
		wantState        string
		wantStale        bool
		wantPending      bool
		wantNotRequested bool
		wantRateLimit    int
		wantErr          bool
	}{
		{
			name: "approved on HEAD",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", headSHA, ""),
				},
				4999,
			),
			wantState:     "approved",
			wantRateLimit: 4999,
		},
		{
			name: "changes_requested on HEAD",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, ""),
				},
				4998,
			),
			wantState:     "changes_requested",
			wantRateLimit: 4998,
		},
		{
			name: "commented on HEAD",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "COMMENTED", headSHA, ""),
				},
				4997,
			),
			wantState:     "commented",
			wantRateLimit: 4997,
		},
		{
			name: "stale review targets old commit",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", "oldsha789", ""),
				},
				4996,
			),
			wantStale:        true,
			wantNotRequested: true,
			wantRateLimit:    4996,
		},
		{
			// Bug #3 from review: a fresh review request (copilotRequested)
			// combined with a stale review from a previous commit and no
			// HEAD review yet must report Pending=true and Stale=false.
			// Previously the trailing sawStale block also set Stale=true,
			// masking the in-progress review and satisfying the gate.
			name: "requested with stale review present - pending wins, not stale",
			query: makeReviewQuery(
				[]requestNode{
					makeRequestNode("copilot-pull-request-reviewer"),
				},
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", "oldsha789", ""),
				},
				4990,
			),
			wantPending:   true,
			wantState:     "pending",
			wantStale:     false,
			wantRateLimit: 4990,
		},
		{
			name: "in progress - requested but no review",
			query: makeReviewQuery(
				[]requestNode{
					makeRequestNode("copilot-pull-request-reviewer"),
				},
				nil,
				4995,
			),
			wantPending:   true,
			wantState:     "pending",
			wantRateLimit: 4995,
		},
		{
			name: "pending review on HEAD (submitted but state PENDING)",
			query: makeReviewQuery(
				[]requestNode{
					makeRequestNode("copilot-pull-request-reviewer"),
				},
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "PENDING", headSHA, ""),
				},
				4994,
			),
			wantPending:   true,
			wantState:     "pending",
			wantRateLimit: 4994,
		},
		{
			name: "not requested - no requests and no reviews",
			query: makeReviewQuery(
				nil,
				nil,
				4993,
			),
			wantNotRequested: true,
			wantRateLimit:    4993,
		},
		{
			name: "non-copilot review ignored",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("some-user", "APPROVED", headSHA, ""),
				},
				4992,
			),
			wantNotRequested: true,
			wantRateLimit:    4992,
		},
		{
			name: "stale review plus fresh HEAD review - not stale (fresh wins)",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", headSHA, ""),
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", "oldsha789", ""),
				},
				4991,
			),
			wantState:     "approved",
			wantRateLimit: 4991,
		},
		{
			// Regression: the reviews connection returns nodes in
			// ascending submission order (oldest first). When multiple
			// Copilot reviews target HEAD, the last (newest) one must win.
			// Here an older CHANGES_REQUESTED precedes a newer APPROVED;
			// the result must be "approved", not "changes_requested".
			name: "multiple HEAD reviews - newest wins (ascending order)",
			query: makeReviewQuery(
				nil,
				[]reviewNode{
					makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, "2025-01-01T00:00:00Z"),
					makeReviewNode("copilot-pull-request-reviewer", "APPROVED", headSHA, "2025-01-02T00:00:00Z"),
				},
				4989,
			),
			wantState:     "approved",
			wantRateLimit: 4989,
		},
		{
			name:          "query error",
			err:           fmt.Errorf("graphql error"),
			wantErr:       true,
			wantRateLimit: 5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockReviewQuerier{
				responses: []mockReviewResponse{{query: tt.query, err: tt.err}},
			}

			review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if rateLimit != tt.wantRateLimit {
					t.Errorf("rate limit = %d, want %d", rateLimit, tt.wantRateLimit)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if review.State != tt.wantState {
				t.Errorf("State = %q, want %q", review.State, tt.wantState)
			}
			if review.Stale != tt.wantStale {
				t.Errorf("Stale = %v, want %v", review.Stale, tt.wantStale)
			}
			if review.Pending != tt.wantPending {
				t.Errorf("Pending = %v, want %v", review.Pending, tt.wantPending)
			}
			if review.NotRequested != tt.wantNotRequested {
				t.Errorf("NotRequested = %v, want %v", review.NotRequested, tt.wantNotRequested)
			}
			if rateLimit != tt.wantRateLimit {
				t.Errorf("rateLimit = %d, want %d", rateLimit, tt.wantRateLimit)
			}
			if mock.callCount != 1 {
				t.Errorf("callCount = %d, want 1", mock.callCount)
			}
		})
	}
}

// TestFetchCopilotReview_BeyondFirstPage is a regression test for the
// `reviews(first: 25)` bug (issue #409 review). On a heavily-reviewed PR with
// more than 25 total reviews across all reviewers, `first: 25` returned the
// oldest 25 of the ascending reviews connection, dropping the newest Copilot
// review on HEAD entirely — so headReview stayed nil and the gate fell
// through to "not requested", silently passing a changes_requested review.
// With `reviews(last: 25)` the most recent 25 are returned and the newest
// Copilot review on HEAD is found. Here 25 non-Copilot reviews precede a
// Copilot CHANGES_REQUESTED on HEAD; the fetch must surface that state.
func TestFetchCopilotReview_BeyondFirstPage(t *testing.T) {
	headSHA := "abc123def456"

	// 25 non-Copilot reviews fill the first page under the old `first: 25`
	// semantics, pushing the Copilot review out of the returned window.
	reviews := humanReviews(25, headSHA)
	// Newest Copilot review on HEAD — the one that must win.
	reviews = append(reviews, makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, "2025-01-02T00:00:00Z"))

	query := makeReviewQuery(nil, reviews, 4980)
	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: query}}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.State != "changes_requested" {
		t.Errorf("State = %q, want %q (newest Copilot review on HEAD must be found, not dropped)", review.State, "changes_requested")
	}
	if review.NotRequested {
		t.Error("NotRequested = true, want false (must not fall through to not-requested when a HEAD review exists beyond the old first:25 window)")
	}
	if rateLimit != 4980 {
		t.Errorf("rateLimit = %d, want 4980", rateLimit)
	}
}

// TestFetchCopilotReview_ReviewsWalkBackPages is a regression test for
// issue #486: `reviews(last: 25)` was a single un-paginated window, so a PR
// with more than 25 reviews AFTER the newest Copilot review dropped it and
// a completed Copilot review read as not-requested. The fetch must walk
// backward pages (last: 25, before: $reviewsCursor) until the Copilot
// review is found or the connection is exhausted.
func TestFetchCopilotReview_ReviewsWalkBackPages(t *testing.T) {
	headSHA := "abc123def456"

	// Page 1: the newest 25 of 31 reviews — all human, no Copilot review.
	page1 := makeReviewQueryPaged(nil, requestPageInfo{},
		humanReviews(25, headSHA),
		reviewPageInfo{hasPreviousPage: true, startCursor: "rev-page-1"}, 4990)

	// Page 2 (the 6 oldest reviews): 5 more humans, then the Copilot
	// CHANGES_REQUESTED on HEAD — the one that must be detected.
	older := append(humanReviews(5, headSHA),
		makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, "2025-01-02T00:00:00Z"))
	page2 := makeReviewQueryPaged(nil, requestPageInfo{}, older, reviewPageInfo{}, 4989)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}, {query: page2}}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.State != "changes_requested" {
		t.Errorf("State = %q, want %q (Copilot review beyond the newest 25-review page must be found)", review.State, "changes_requested")
	}
	if review.NotRequested {
		t.Error("NotRequested = true, want false")
	}
	if rateLimit != 4989 {
		t.Errorf("rateLimit = %d, want 4989 (minimum across pages)", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (walk back one page)", mock.callCount)
	}
	// The first query must start at the newest window (nil cursor).
	if rc, ok := mock.variablesByCall[0]["reviewsCursor"].(*githubv4.String); !ok || rc != nil {
		t.Errorf("reviewsCursor on call 1 = %v, want nil", mock.variablesByCall[0]["reviewsCursor"])
	}
	// The second query must carry page 1's startCursor.
	if got, ok := mock.variablesByCall[1]["reviewsCursor"].(githubv4.String); !ok || got != githubv4.String("rev-page-1") {
		t.Errorf("reviewsCursor on call 2 = %v, want rev-page-1", mock.variablesByCall[1]["reviewsCursor"])
	}
}

// TestFetchCopilotReview_RequestsWalkForwardPages is a regression test for
// issue #486: `reviewRequests(first: 10)` was a single un-paginated window,
// so more than 10 requested reviewers hid the Copilot request and a
// genuinely pending review read as not-requested. The fetch must walk
// forward pages (first: 10, after: $requestsCursor) until the Copilot
// request is found or the connection is exhausted.
func TestFetchCopilotReview_RequestsWalkForwardPages(t *testing.T) {
	headSHA := "abc123def456"

	// Page 1: 10 requested humans; the Copilot request is the 15th of 15.
	page1 := makeReviewQueryPaged(humanRequests(10),
		requestPageInfo{hasNextPage: true, endCursor: "req-page-1"},
		nil, reviewPageInfo{}, 4990)

	// Page 2: 4 more humans, then the Copilot request that must be found.
	page2Requests := append(humanRequests(4), makeRequestNode("copilot-pull-request-reviewer"))
	page2 := makeReviewQueryPaged(page2Requests, requestPageInfo{}, nil, reviewPageInfo{}, 4989)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}, {query: page2}}}

	review, _, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !review.Pending {
		t.Error("Pending = false, want true (Copilot request beyond the first 10-request page must be found)")
	}
	if review.State != "pending" {
		t.Errorf("State = %q, want %q", review.State, "pending")
	}
	if review.NotRequested {
		t.Error("NotRequested = true, want false")
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (walk forward one page)", mock.callCount)
	}
	// The first query must start at the beginning (nil cursor).
	if rc, ok := mock.variablesByCall[0]["requestsCursor"].(*githubv4.String); !ok || rc != nil {
		t.Errorf("requestsCursor on call 1 = %v, want nil", mock.variablesByCall[0]["requestsCursor"])
	}
	// The second query must carry page 1's endCursor.
	if got, ok := mock.variablesByCall[1]["requestsCursor"].(githubv4.String); !ok || got != githubv4.String("req-page-1") {
		t.Errorf("requestsCursor on call 2 = %v, want req-page-1", mock.variablesByCall[1]["requestsCursor"])
	}
}

// TestFetchCopilotReview_BothConnectionsPaginate walks both connections
// across their page boundaries in the same round trips: page 1 holds only
// human data on both connections, page 2 holds both the Copilot review and
// the Copilot request. Both cursors must advance together (issue #486).
func TestFetchCopilotReview_BothConnectionsPaginate(t *testing.T) {
	headSHA := "abc123def456"

	page1 := makeReviewQueryPaged(
		humanRequests(10), requestPageInfo{hasNextPage: true, endCursor: "req-page-1"},
		humanReviews(25, headSHA), reviewPageInfo{hasPreviousPage: true, startCursor: "rev-page-1"},
		4990)
	page2 := makeReviewQueryPaged(
		append(humanRequests(4), makeRequestNode("copilot-pull-request-reviewer")), requestPageInfo{},
		append(humanReviews(5, headSHA),
			makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, "2025-01-02T00:00:00Z")),
		reviewPageInfo{}, 4989)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}, {query: page2}}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.State != "changes_requested" {
		t.Errorf("State = %q, want %q", review.State, "changes_requested")
	}
	if review.Pending {
		t.Error("Pending = true, want false (submitted HEAD review takes precedence over the request)")
	}
	if review.NotRequested {
		t.Error("NotRequested = true, want false")
	}
	if rateLimit != 4989 {
		t.Errorf("rateLimit = %d, want 4989 (minimum across pages)", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2", mock.callCount)
	}
	if got, ok := mock.variablesByCall[1]["reviewsCursor"].(githubv4.String); !ok || got != githubv4.String("rev-page-1") {
		t.Errorf("reviewsCursor on call 2 = %v, want rev-page-1", mock.variablesByCall[1]["reviewsCursor"])
	}
	if got, ok := mock.variablesByCall[1]["requestsCursor"].(githubv4.String); !ok || got != githubv4.String("req-page-1") {
		t.Errorf("requestsCursor on call 2 = %v, want req-page-1", mock.variablesByCall[1]["requestsCursor"])
	}
}

// TestFetchCopilotReview_EarlyExitOnFirstPage verifies pagination does not
// fire when the first page already holds a Copilot review targeting HEAD,
// even though older reviews exist (HasPreviousPage true): the newest HEAD
// review wins, so older pages cannot change the result and the common case
// stays a single round trip.
func TestFetchCopilotReview_EarlyExitOnFirstPage(t *testing.T) {
	headSHA := "abc123def456"

	reviews := append(humanReviews(24, headSHA),
		makeReviewNode("copilot-pull-request-reviewer", "APPROVED", headSHA, "2025-01-02T00:00:00Z"))
	// Older reviews exist beyond the page, but the Copilot HEAD review is
	// already in hand.
	page1 := makeReviewQueryPaged(nil, requestPageInfo{}, reviews,
		reviewPageInfo{hasPreviousPage: true, startCursor: "rev-page-1"}, 4999)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}}}

	review, _, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.State != "approved" {
		t.Errorf("State = %q, want %q", review.State, "approved")
	}
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no extra page needed once the HEAD review is found)", mock.callCount)
	}
}

// TestFetchCopilotReview_MixedPagination exercises the interleaved walk:
// the reviews connection is satisfied on page 1 (Copilot HEAD review found)
// while the requests connection still needs page 2. The done connection's
// cursor stays pinned and its re-fetched page is not accumulated — page 2
// below serves a trap CHANGES_REQUESTED review that would flip the result
// if appended to the accumulated slice.
func TestFetchCopilotReview_MixedPagination(t *testing.T) {
	headSHA := "abc123def456"

	// Page 1: 10 requested humans (Copilot request is #15), and the
	// Copilot review on HEAD is already present in the reviews window.
	reviews := []reviewNode{
		makeReviewNode("some-user", "COMMENTED", headSHA, ""),
		makeReviewNode("copilot-pull-request-reviewer", "APPROVED", headSHA, ""),
	}
	page1 := makeReviewQueryPaged(
		humanRequests(10), requestPageInfo{hasNextPage: true, endCursor: "req-page-1"},
		reviews, reviewPageInfo{}, 4990)

	// Page 2: the Copilot request is found; the re-fetched reviews page
	// (cursor pinned) must be ignored.
	trapReviews := []reviewNode{
		makeReviewNode("copilot-pull-request-reviewer", "CHANGES_REQUESTED", headSHA, "2025-01-03T00:00:00Z"),
	}
	page2Requests := append(humanRequests(4), makeRequestNode("copilot-pull-request-reviewer"))
	page2 := makeReviewQueryPaged(page2Requests, requestPageInfo{}, trapReviews, reviewPageInfo{}, 4989)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}, {query: page2}}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.State != "approved" {
		t.Errorf("State = %q, want %q (re-fetched reviews after the HEAD review is found must not accumulate)", review.State, "approved")
	}
	if review.Pending {
		t.Error("Pending = true, want false (submitted HEAD review takes precedence over the request)")
	}
	if rateLimit != 4989 {
		t.Errorf("rateLimit = %d, want 4989 (minimum across pages)", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (requests still need page 2)", mock.callCount)
	}
}

// TestFetchCopilotReview_StaleAcrossPages walks backward past a stale-only
// Copilot review spanning multiple pages: no HEAD review exists anywhere,
// so the walk must exhaust the connection (never early-exit on a stale
// review) and parseCopilotReview's sawStale branch must fire from data
// gathered on a later page (review of #535: multi-page sawStale coverage).
func TestFetchCopilotReview_StaleAcrossPages(t *testing.T) {
	headSHA := "abc123def456"

	// Page 1: the newest 25 reviews — all human.
	page1 := makeReviewQueryPaged(nil, requestPageInfo{},
		humanReviews(25, headSHA),
		reviewPageInfo{hasPreviousPage: true, startCursor: "rev-page-1"}, 4990)

	// Page 2: 24 older human reviews and a stale Copilot review targeting
	// a previous commit; the connection ends here.
	older := append(humanReviews(24, "oldsha789"),
		makeReviewNode("copilot-pull-request-reviewer", "APPROVED", "oldsha789", ""))
	page2 := makeReviewQueryPaged(nil, requestPageInfo{}, older, reviewPageInfo{}, 4989)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{{query: page1}, {query: page2}}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !review.Stale {
		t.Error("Stale = false, want true (stale Copilot review on a later page must set sawStale)")
	}
	if !review.NotRequested {
		t.Error("NotRequested = false, want true (no HEAD review and no request)")
	}
	if review.State != "" {
		t.Errorf("State = %q, want empty", review.State)
	}
	if rateLimit != 4989 {
		t.Errorf("rateLimit = %d, want 4989 (minimum across pages)", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (walk back one page to exhaust the connection)", mock.callCount)
	}
}

// TestFetchCopilotReview_MidPaginationError verifies the mid-walk failure
// path (review of #535): an error on a later page must fail the whole
// fetch — discarding any partial state on purpose — while keeping the
// minimum rate limit observed so far.
func TestFetchCopilotReview_MidPaginationError(t *testing.T) {
	headSHA := "abc123def456"

	// Page 1: humans only with more history before it, so the walk wants
	// a second page.
	page1 := makeReviewQueryPaged(nil, requestPageInfo{},
		humanReviews(25, headSHA),
		reviewPageInfo{hasPreviousPage: true, startCursor: "rev-page-1"}, 4990)

	mock := &mockReviewQuerier{responses: []mockReviewResponse{
		{query: page1},
		{err: fmt.Errorf("graphql error mid-walk")},
	}}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if review.State != "" || review.Stale || review.Pending || review.NotRequested {
		t.Errorf("partial state must be discarded on mid-walk error, got %+v", review)
	}
	if review.CommitOID != "" {
		t.Errorf("CommitOID = %q, want empty (partial state discarded)", review.CommitOID)
	}
	if rateLimit != 4990 {
		t.Errorf("rateLimit = %d, want 4990 (minimum from the successful page)", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2", mock.callCount)
	}
}

// TestFetchCopilotReview_PageCap verifies the maxCopilotQueryPages guard
// (review of #535): a PR whose review history never contains a Copilot
// HEAD review must not walk the connection unbounded on every poll. The
// mock serves exactly maxCopilotQueryPages endless-history pages; the cap
// must stop the walk without requesting an 11th page (which the mock
// would reject) and without erroring.
func TestFetchCopilotReview_PageCap(t *testing.T) {
	headSHA := "abc123def456"

	responses := make([]mockReviewResponse, 0, maxCopilotQueryPages)
	for i := 0; i < maxCopilotQueryPages; i++ {
		q := makeReviewQueryPaged(nil, requestPageInfo{},
			humanReviews(25, headSHA),
			reviewPageInfo{hasPreviousPage: true, startCursor: fmt.Sprintf("rev-page-%d", i+1)}, 4990)
		responses = append(responses, mockReviewResponse{query: q})
	}
	mock := &mockReviewQuerier{responses: responses}

	review, rateLimit, err := fetchCopilotReview(context.Background(), mock, "owner", "repo", 42, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v (page cap must stop the walk, not run out of pages)", err)
	}
	if !review.NotRequested {
		t.Error("NotRequested = false, want true (no Copilot review within the capped window)")
	}
	if review.Stale || review.Pending {
		t.Errorf("Stale = %v, Pending = %v, want false, false", review.Stale, review.Pending)
	}
	if rateLimit != 4990 {
		t.Errorf("rateLimit = %d, want 4990", rateLimit)
	}
	if mock.callCount != maxCopilotQueryPages {
		t.Errorf("callCount = %d, want %d (walk must stop at the cap)", mock.callCount, maxCopilotQueryPages)
	}
}

// TestFetchCopilotReview_NullableCursorVariables pins the generated query
// document through the real githubv4 client (review of #535): the
// in-package mock asserts cursor *values* but cannot see the query string
// githubv4 builds from the struct. The first page passes typed nil
// *githubv4.String cursors, which must produce nullable declarations
// ($reviewsCursor: String, $requestsCursor: String — no "!") with null
// variable values; GitHub rejects a null passed to a non-null variable, so
// the nil-cursor round trip depends on exactly this shape. Together with
// the mock tests' cursor-passthrough assertions, this covers both pages of
// the cursor plumbing end to end.
func TestFetchCopilotReview_NullableCursorVariables(t *testing.T) {
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {"pullRequest": {
					"reviewRequests": {"nodes": [], "pageInfo": {"hasNextPage": false, "endCursor": null}},
					"reviews": {"nodes": [], "pageInfo": {"hasPreviousPage": false, "startCursor": null}}
				}},
				"rateLimit": {"limit": 5000, "remaining": 4999}
			}
		}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv(APIURLEnvOverride, server.URL)

	review, rateLimit, err := FetchCopilotReview(context.Background(), "test-token", "ghe.example.com", "owner", "repo", 42, "abc123def456")
	if err != nil {
		t.Fatalf("FetchCopilotReview() error: %v", err)
	}
	if !review.NotRequested {
		t.Error("NotRequested = false, want true (empty response)")
	}
	if rateLimit != 4999 {
		t.Errorf("rateLimit = %d, want 4999", rateLimit)
	}

	select {
	case body := <-bodies:
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if !strings.Contains(req.Query, "reviews(last: 25, before: $reviewsCursor)") {
			t.Errorf("query missing reviews pagination args:\n%s", req.Query)
		}
		if !strings.Contains(req.Query, "reviewRequests(first: 10, after: $requestsCursor)") {
			t.Errorf("query missing reviewRequests pagination args:\n%s", req.Query)
		}
		for _, name := range []string{"$reviewsCursor", "$requestsCursor"} {
			// shurcooL emits compact declarations ("$name:String"), so
			// match without the space; the negative check pins that the
			// typed nil cursor did not become non-null ("$name:String!").
			if !strings.Contains(req.Query, name+":String") {
				t.Errorf("query must declare %s as nullable String:\n%s", name, req.Query)
			}
			if strings.Contains(req.Query, name+":String!") {
				t.Errorf("query must not declare %s non-null (typed nil cursors):\n%s", name, req.Query)
			}
		}
		if req.Variables["reviewsCursor"] != nil || req.Variables["requestsCursor"] != nil {
			t.Errorf("first-page cursors must serialize as null, got %v / %v",
				req.Variables["reviewsCursor"], req.Variables["requestsCursor"])
		}
	default:
		t.Error("expected exactly one GraphQL request, got none")
	}
}

func TestCopilotReviewFails(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"changes_requested", true},
		{"approved", false},
		{"commented", false},
		{"dismissed", false},
		{"pending", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := CopilotReviewFails(tt.state); got != tt.want {
			t.Errorf("CopilotReviewFails(%q) = %v, want %v", tt.state, got, tt.want)
		}
	}
}
