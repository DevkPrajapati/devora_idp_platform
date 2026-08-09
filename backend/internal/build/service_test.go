package build

import (
	"regexp"
	"strings"
	"testing"

	"github.com/idp/platform/backend/internal/kubernetes"
)

// dockerTagValid is Docker's own rule: a tag starts with an alphanumeric or
// underscore and is at most 128 characters.
var dockerTagValid = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)

// Branch names routinely contain characters a Docker tag may not. Pushing an
// invalid tag fails inside the build with a registry error that says nothing
// about the branch that caused it.
func TestBuildImageTagIsAlwaysValid(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		commit string
		number int64
	}{
		{"simple branch", "main", "abc1234def", 1},
		{"slash in branch", "feature/new-thing", "abc1234def", 2},
		{"deep slashes", "user/jane/fix/JIRA-123", "abc1234def", 3},
		{"underscores and dots", "release_1.2.3", "abc1234def", 4},
		{"leading separator", "-weird-branch", "abc1234def", 5},
		{"unicode", "feature/café-ünïcode", "abc1234def", 6},
		{"empty branch", "", "abc1234def", 7},
		{"no commit", "main", "", 8},
		{"short commit", "main", "abc", 9},
		{"very long branch", strings.Repeat("long-branch-name-", 20), "abc1234def", 10},
		{"only separators", "///", "", 11},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tag := BuildImageTag(tt.branch, tt.commit, tt.number)
			if !dockerTagValid.MatchString(tag) {
				t.Errorf("BuildImageTag(%q, %q, %d) = %q, which is not a valid Docker tag",
					tt.branch, tt.commit, tt.number, tag)
			}
		})
	}
}

func TestBuildImageTagUsesCommitWhenAvailable(t *testing.T) {
	tag := BuildImageTag("main", "abc1234def5678", 42)
	if tag != "main-abc1234" {
		t.Errorf("tag = %q, want main-abc1234 (7-char short sha)", tag)
	}
}

// A manual build has no commit yet. Falling back to the build number keeps tags
// unique; without it every manual build would overwrite the same tag.
func TestBuildImageTagFallsBackToBuildNumber(t *testing.T) {
	first := BuildImageTag("main", "", 1)
	second := BuildImageTag("main", "", 2)

	if first == second {
		t.Fatalf("two manual builds produced the same tag %q", first)
	}
	if first != "main-b1" {
		t.Errorf("tag = %q, want main-b1", first)
	}
}

// Job names are DNS labels, capped at 63 characters. Exceeding it makes the
// API server reject the Job with a message about the name, not the repository.
func TestBuildJobNameFitsDNSLabel(t *testing.T) {
	cases := []struct {
		repository string
		number     int64
	}{
		{"api", 1},
		{"a-very-long-repository-name-that-goes-on", 999},
		{strings.Repeat("x", 60), 1234},
		{strings.Repeat("repo-name-", 10), 7},
	}

	dnsLabel := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	for _, tt := range cases {
		name := BuildJobName(tt.repository, tt.number)
		if len(name) > 63 {
			t.Errorf("BuildJobName(%q, %d) is %d chars, over the 63 limit", tt.repository, tt.number, len(name))
		}
		if !dnsLabel.MatchString(name) {
			t.Errorf("BuildJobName(%q, %d) = %q, not a valid DNS label", tt.repository, tt.number, name)
		}
	}
}

func TestBuildJobNameIsUniquePerBuild(t *testing.T) {
	long := strings.Repeat("x", 60)
	if BuildJobName(long, 1) == BuildJobName(long, 2) {
		t.Error("truncation dropped the build number, making job names collide")
	}
}

// The token must reach the build only through a Secret. A clone URL carrying
// credentials in the Job spec would be visible to anyone with `get jobs`.
func TestBuildCloneURLInjectsToken(t *testing.T) {
	withToken, err := kubernetes.BuildCloneURL("https://github.com/acme/api.git", "ghp_secret", "github")
	if err != nil {
		t.Fatalf("BuildCloneURL: %v", err)
	}
	if !strings.Contains(withToken, "x-access-token:ghp_secret@") {
		t.Errorf("github clone url should use x-access-token:<pat>, got %q", withToken)
	}

	gitlabURL, err := kubernetes.BuildCloneURL("https://gitlab.com/acme/api.git", "glpat_secret", "gitlab")
	if err != nil {
		t.Fatalf("BuildCloneURL gitlab: %v", err)
	}
	if !strings.Contains(gitlabURL, "oauth2:glpat_secret") {
		t.Errorf("gitlab clone url should use oauth2, got %q", gitlabURL)
	}

	public, err := kubernetes.BuildCloneURL("https://github.com/acme/api.git", "", "github")
	if err != nil {
		t.Fatalf("BuildCloneURL: %v", err)
	}
	if strings.Contains(public, "@") {
		t.Errorf("public clone url gained credentials: %q", public)
	}
}

func TestBuildCloneURLRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":                "",
		"unsupported scheme":   "git@github.com:acme/api.git",
		"ssh scheme":           "ssh://git@github.com/acme/api.git",
		"no host":              "https://",
		"embedded credentials": "https://user:pass@github.com/acme/api.git",
		"embedded user":        "https://token@github.com/acme/api.git",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := kubernetes.BuildCloneURL(raw, "tok", "github"); err == nil {
				t.Errorf("accepted %q -> %q", raw, got)
			}
		})
	}
}

func TestNormalizeGitRef(t *testing.T) {
	cases := map[string]string{
		"main":              "refs/heads/main",
		"feature/x":         "refs/heads/feature/x",
		"":                  "refs/heads/main",
		"  develop  ":       "refs/heads/develop",
		"refs/heads/custom": "refs/heads/custom",
		"refs/tags/v1":      "refs/tags/v1",
	}
	for input, want := range cases {
		if got := kubernetes.NormalizeGitRef(input); got != want {
			t.Errorf("NormalizeGitRef(%q) = %q, want %q", input, got, want)
		}
	}
}

// A build compiles untrusted repository code. Unbounded, one repository can
// starve every other workload on the node.
func TestBuildResourceDefaultsAreBounded(t *testing.T) {
	defaults := BuildResourceDefaults()
	if defaults.Empty() {
		t.Fatal("build resources must not be unbounded")
	}
	if err := defaults.Validate(); err != nil {
		t.Errorf("default build resources are invalid: %v", err)
	}
	if defaults.CPULimit == "" || defaults.MemoryLimit == "" {
		t.Error("build resources must set both CPU and memory limits")
	}
}
