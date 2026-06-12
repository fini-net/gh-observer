package tui

import (
	"context"
	"sort"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/google/go-github/v86/github"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

type PRViewData struct {
	Title          string
	CheckRuns      []ghclient.CheckRunInfo
	HeadCommitTime time.Time
}

type RepoModel struct {
	ctx      context.Context
	token    string
	client   *github.Client
	owner    string
	repo     string
	prs      map[int]PRViewData

	standaloneRuns []ghclient.BranchRunData
	defaultBranch  string
	allBranches    bool
	showBranchRuns bool

	rateLimitRemaining int

	spinner         spinner.Model
	startTime       time.Time
	lastUpdate      time.Time
	refreshInterval time.Duration
	styles          Styles

	fadeSuccess time.Duration
	fadeFailure time.Duration

	exitCode int
	quitting bool

	err error

	enableLinks bool
}

func NewRepoModel(ctx context.Context, token string, client *github.Client, owner, repo string, refreshInterval time.Duration, styles Styles, enableLinks bool, fadeSuccess, fadeFailure time.Duration, showBranchRuns bool, allBranches bool) RepoModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return RepoModel{
		ctx:             ctx,
		token:           token,
		client:          client,
		owner:           owner,
		repo:            repo,
		prs:             make(map[int]PRViewData),
		spinner:         s,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
		refreshInterval: refreshInterval,
		styles:          styles,
		enableLinks:     enableLinks,
		fadeSuccess:     fadeSuccess,
		fadeFailure:     fadeFailure,
		showBranchRuns:  showBranchRuns,
		allBranches:     allBranches,
	}
}

func (m RepoModel) ExitCode() int {
	return m.exitCode
}

func (m RepoModel) sortedPRNumbers() []int {
	nums := make([]int, 0, len(m.prs))
	for n := range m.prs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}