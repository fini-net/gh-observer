package github

// FailureConclusion returns true if the conclusion indicates a failed check.
func FailureConclusion(conclusion string) bool {
	return conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required"
}
