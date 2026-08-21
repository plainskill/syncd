package fanout

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name       string
		fj, in     string
		fjIn, inFj bool
		want       Decision
	}{
		{"empty fj copies in", "", "aaa", false, false, DecFF},
		{"equal", "aaa", "aaa", true, true, DecFF},
		{"fj ancestor of in", "aaa", "bbb", true, false, DecFF},
		{"source stale", "bbb", "aaa", false, true, DecFanout},
		{"diverge", "aaa", "ccc", false, false, DecMerge},
		{"empty in", "aaa", "", false, false, DecSkip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.fj, tc.in, tc.fjIn, tc.inFj)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestPushOrderGitHubThenFjThenGitLawb(t *testing.T) {
	got := PushOrder("github", false)
	want := []string{"forgejo", "gitlawb"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("%v", got)
	}
	got = PushOrder("forgejo", false)
	if got[0] != "github" || got[1] != "gitlawb" {
		t.Fatalf("fj origin should hit github first for Origin: %v", got)
	}
}

func TestConflictBranch(t *testing.T) {
	if ConflictBranch("github", "abcdef123") != "sync/github/abcdef1" {
		t.Fatalf("%s", ConflictBranch("github", "abcdef123"))
	}
}
