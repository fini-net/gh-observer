package github

import (
	"context"
	"fmt"
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
}

func (m *mockReviewQuerier) Query(_ context.Context, q interface{}, _ map[string]interface{}) error {
	if m.callCount >= len(m.responses) {
		return fmt.Errorf("unexpected query call #%d", m.callCount+1)
	}
	resp := m.responses[m.callCount]
	m.callCount++
	if resp.err != nil {
		return resp.err
	}
	target := q.(*copilotReviewQuery)
	*target = *resp.query
	return nil
}

func makeReviewNode(authorLogin, state, commitOID, submittedAt string) struct {
	Author struct {
		Login string
	}
	State       string
	SubmittedAt githubv4.DateTime
	Commit      struct {
		OID string
	}
	Body string
} {
	var submittedAtTime time.Time
	if submittedAt != "" {
		if t, err := time.Parse(time.RFC3339, submittedAt); err == nil {
			submittedAtTime = t
		}
	}
	return struct {
		Author struct {
			Login string
		}
		State       string
		SubmittedAt githubv4.DateTime
		Commit      struct {
			OID string
		}
		Body string
	}{
		Author:      struct{ Login string }{Login: authorLogin},
		State:       state,
		SubmittedAt: githubv4.DateTime{Time: submittedAtTime},
		Commit:      struct{ OID string }{OID: commitOID},
	}
}

func makeRequestNode(login string) struct {
	RequestedReviewer struct {
		Typename string `graphql:"__typename"`
		Bot      struct {
			Login string
		} `graphql:"... on Bot"`
		User struct {
			Login string
		} `graphql:"... on User"`
	}
} {
	return struct {
		RequestedReviewer struct {
			Typename string `graphql:"__typename"`
			Bot      struct {
				Login string
			} `graphql:"... on Bot"`
			User struct {
				Login string
			} `graphql:"... on User"`
		}
	}{
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

func makeReviewQuery(requests []struct {
	RequestedReviewer struct {
		Typename string `graphql:"__typename"`
		Bot      struct {
			Login string
		} `graphql:"... on Bot"`
		User struct {
			Login string
		} `graphql:"... on User"`
	}
}, reviews []struct {
	Author struct {
		Login string
	}
	State       string
	SubmittedAt githubv4.DateTime
	Commit      struct {
		OID string
	}
	Body string
}, rateLimit int) *copilotReviewQuery {
	q := &copilotReviewQuery{
		RateLimit: struct{ Remaining int }{Remaining: rateLimit},
	}
	q.Repository.PullRequest.ReviewRequests.Nodes = requests
	q.Repository.PullRequest.Reviews.Nodes = reviews
	return q
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					RequestedReviewer struct {
						Typename string `graphql:"__typename"`
						Bot      struct {
							Login string
						} `graphql:"... on Bot"`
						User struct {
							Login string
						} `graphql:"... on User"`
					}
				}{
					makeRequestNode("copilot-pull-request-reviewer"),
				},
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					RequestedReviewer struct {
						Typename string `graphql:"__typename"`
						Bot      struct {
							Login string
						} `graphql:"... on Bot"`
						User struct {
							Login string
						} `graphql:"... on User"`
					}
				}{
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
				[]struct {
					RequestedReviewer struct {
						Typename string `graphql:"__typename"`
						Bot      struct {
							Login string
						} `graphql:"... on Bot"`
						User struct {
							Login string
						} `graphql:"... on User"`
					}
				}{
					makeRequestNode("copilot-pull-request-reviewer"),
				},
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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
				[]struct {
					Author struct {
						Login string
					}
					State       string
					SubmittedAt githubv4.DateTime
					Commit      struct {
						OID string
					}
					Body string
				}{
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

	var reviews []struct {
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
	// 25 non-Copilot reviews fill the first page under the old `first: 25`
	// semantics, pushing the Copilot review out of the returned window.
	for i := 0; i < 25; i++ {
		reviews = append(reviews, makeReviewNode("some-user", "COMMENTED", headSHA, ""))
	}
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
