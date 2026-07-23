package orchestrator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Conformance tests for contracts/review-gating.md §2 (hard rules), §3 (tier
// table + greenfield modifier), §7.1/§7.4 (table coverage + determinism).

func gateProfile(mutate func(*TaskProfile)) TaskProfile {
	p := TaskProfile{
		Class:        ClassStandard,
		FilesChanged: []string{"pkg/service.go", "pkg/helper.go", "pkg/model.go"},
		LinesAdded:   80, LinesDeleted: 20,
		TestOutcome: "passed",
		RepoBucket:  RepoBucketMedium,
		GatingMode:  ReviewGatingDefault,
	}
	if mutate != nil {
		mutate(&p)
	}
	return p
}

func TestDecideHardRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TaskProfile)
		want   ReviewTier
	}{
		{"H1 explicit request forces focused even for docs", func(p *TaskProfile) {
			p.ExplicitRequest = true
			p.FilesChanged = []string{"README.md"}
		}, ReviewTierFocused},
		{"H2 gating off skips even a risky change", func(p *TaskProfile) {
			p.GatingMode = ReviewGatingOff
			p.FilesChanged = []string{"internal/auth/middleware.go"}
		}, ReviewTierSkip},
		{"H3 zero files skips", func(p *TaskProfile) { p.FilesChanged = nil }, ReviewTierSkip},
		{"H4 one-line auth change reviews despite tiny size", func(p *TaskProfile) {
			p.FilesChanged = []string{"internal/auth/middleware.go"}
			p.LinesAdded, p.LinesDeleted = 1, 1
		}, ReviewTierFocused},
		{"H5 failed tests force focused on a small change", func(p *TaskProfile) {
			p.FilesChanged = []string{"pkg/service.go"}
			p.LinesAdded, p.LinesDeleted = 5, 2
			p.TestOutcome = "failed"
		}, ReviewTierFocused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(gateProfile(tc.mutate)); got.Tier != tc.want {
				t.Fatalf("tier = %s (%s), want %s", got.Tier, got.Rationale, tc.want)
			}
		})
	}
}

func TestDecideTierTable(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(*TaskProfile)
		want         ReviewTier
		conservative ReviewTier // expected tier in conservative mode
	}{
		{"docs-only skips in both modes", func(p *TaskProfile) {
			p.FilesChanged = []string{"README.md", "docs/guide.md"}
		}, ReviewTierSkip, ReviewTierSkip},
		{"trivial-intent (rename/comment scale) skips in both modes", func(p *TaskProfile) {
			p.TrivialIntent = true
			p.Class = ClassTiny
			p.FilesChanged = []string{"pkg/service.go"}
			p.LinesAdded, p.LinesDeleted = 2, 2
		}, ReviewTierSkip, ReviewTierSkip},
		{"small config: default skip, conservative focused", func(p *TaskProfile) {
			p.FilesChanged = []string{"app.yaml"}
			p.LinesAdded, p.LinesDeleted = 3, 1
		}, ReviewTierSkip, ReviewTierFocused},
		{"small logic <=2 files <=40 lines: default skip, conservative focused", func(p *TaskProfile) {
			p.FilesChanged = []string{"pkg/service.go", "pkg/service_test.go"}
			p.LinesAdded, p.LinesDeleted = 25, 10
		}, ReviewTierSkip, ReviewTierFocused},
		{"logic over 40 lines: focused in both", func(p *TaskProfile) {
			p.FilesChanged = []string{"pkg/service.go"}
			p.LinesAdded, p.LinesDeleted = 60, 10
		}, ReviewTierFocused, ReviewTierFocused},
		{"3-file logic: focused", nil, ReviewTierFocused, ReviewTierFocused},
		{"unknown line counts with 2 files does NOT qualify for the size skip", func(p *TaskProfile) {
			p.FilesChanged = []string{"pkg/a.go", "pkg/b.go"}
			p.LinesAdded, p.LinesDeleted = 0, 0
		}, ReviewTierFocused, ReviewTierFocused},
		{"unknown line counts with 1 file qualifies", func(p *TaskProfile) {
			p.FilesChanged = []string{"pkg/a.go"}
			p.LinesAdded, p.LinesDeleted = 0, 0
		}, ReviewTierSkip, ReviewTierFocused},
		{"Large class: deep", func(p *TaskProfile) { p.Class = ClassLarge }, ReviewTierDeep, ReviewTierDeep},
		{"over 8 files: deep", func(p *TaskProfile) {
			p.FilesChanged = []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go"}
		}, ReviewTierDeep, ReviewTierDeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(gateProfile(tc.mutate)); got.Tier != tc.want {
				t.Fatalf("default tier = %s (%s), want %s", got.Tier, got.Rationale, tc.want)
			}
			p := gateProfile(tc.mutate)
			p.GatingMode = ReviewGatingConservative
			if got := Decide(p); got.Tier != tc.conservative {
				t.Fatalf("conservative tier = %s (%s), want %s", got.Tier, got.Rationale, tc.conservative)
			}
		})
	}
}

func TestDecideGreenfieldCap(t *testing.T) {
	// Scaffolding in a near-empty repo is size-deep by the table but must cap
	// at focused (spec edge case).
	p := gateProfile(func(p *TaskProfile) {
		p.RepoBucket = RepoBucketTiny
		p.FilesChanged = []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go", "j.go"}
	})
	got := Decide(p)
	if got.Tier != ReviewTierFocused {
		t.Fatalf("greenfield scaffold tier = %s, want focused (capped)", got.Tier)
	}
	// A scaffold that writes an auth module overrides the cap.
	p.FilesChanged = append(p.FilesChanged, "internal/auth/login.go")
	if got := Decide(p); got.Tier != ReviewTierDeep {
		t.Fatalf("risky greenfield tier = %s, want deep (risk overrides cap)", got.Tier)
	}
}

func TestRiskAreaKeywordTables(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"internal/auth/session.go", "auth"},
		{"billing/invoice.go", "billing"},
		{"db/migrations/021_x.sql", "migration"},
		{"certs/server.pem", "security-config"},
	}
	for _, tc := range cases {
		areas := MatchRiskAreas([]string{tc.path})
		if len(areas) == 0 || areas[0] != tc.want {
			t.Fatalf("MatchRiskAreas(%s) = %v, want [%s]", tc.path, areas, tc.want)
		}
	}
	// Content samples match too (diff hunks / prompt).
	if areas := MatchRiskAreas([]string{"pkg/util.go"}, "guard with mutex around the map"); len(areas) != 1 || areas[0] != "concurrency" {
		t.Fatalf("content-sample risk match = %v, want [concurrency]", areas)
	}
	if areas := MatchRiskAreas([]string{"pkg/util.go"}); len(areas) != 0 {
		t.Fatalf("clean path matched risk areas: %v", areas)
	}
}

func TestDecideDeterministicAcrossShuffledRuns(t *testing.T) {
	base := Decide(gateProfile(nil))
	for i := 0; i < 100; i++ {
		// Vary file ORDER only — the decision and rationale must be identical.
		p := gateProfile(func(p *TaskProfile) {
			p.FilesChanged = []string{
				p.FilesChanged[(i+1)%3], p.FilesChanged[(i+2)%3], p.FilesChanged[i%3],
			}
		})
		got := Decide(p)
		if got.Tier != base.Tier || !reflect.DeepEqual(got.RiskAreas, base.RiskAreas) {
			t.Fatalf("run %d diverged: %+v vs %+v", i, got, base)
		}
	}
}

func TestDecideRationaleLineFormat(t *testing.T) {
	d := Decide(gateProfile(func(p *TaskProfile) { p.FilesChanged = []string{"README.md"} }))
	line := d.Line()
	if line != "review: skip — docs-only change" {
		t.Fatalf("rationale line = %q", line)
	}
}

func TestDecideCeilings(t *testing.T) {
	d := Decide(gateProfile(nil)) // 3-file logic → focused, medium bucket
	if d.ProportionalCapPct != 20 {
		t.Fatalf("focused proportional cap = %d, want 20", d.ProportionalCapPct)
	}
	deep := Decide(gateProfile(func(p *TaskProfile) { p.Class = ClassLarge }))
	if deep.ProportionalCapPct != 35 {
		t.Fatalf("deep proportional cap = %d, want 35", deep.ProportionalCapPct)
	}
	// Absolute caps ship at 0 (uncapped) until T021 derives baseline values.
	if d.AbsoluteCapTokens != 0 || deep.AbsoluteCapTokens != 0 {
		t.Fatalf("absolute caps must be 0 pre-baseline, got %d/%d", d.AbsoluteCapTokens, deep.AbsoluteCapTokens)
	}
	skip := Decide(gateProfile(func(p *TaskProfile) { p.FilesChanged = nil }))
	if skip.ProportionalCapPct != 0 || skip.AbsoluteCapTokens != 0 {
		t.Fatalf("skip tier must carry zero ceilings")
	}
}

func TestNormalizeReviewGating(t *testing.T) {
	for value, want := range map[string]ReviewGatingMode{
		"": ReviewGatingDefault, "default": ReviewGatingDefault,
		"conservative": ReviewGatingConservative, "OFF": ReviewGatingOff,
	} {
		got, ok := NormalizeReviewGating(value)
		if !ok || got != want {
			t.Fatalf("NormalizeReviewGating(%q) = %v/%v, want %v", value, got, ok, want)
		}
	}
	if _, ok := NormalizeReviewGating("aggressive"); ok {
		t.Fatal("invalid mode must be rejected")
	}
}

// MUHIYA_BENCH_LEGACY_REVIEW=1 restores pre-011 verdicts (unconditional
// review) with identical measurement paths — the honest-baseline switch.
func TestLegacyModeRestoresUnconditionalReview(t *testing.T) {
	reviewLegacyMode = true
	defer func() { reviewLegacyMode = false }()
	d := Decide(gateProfile(func(p *TaskProfile) { p.FilesChanged = []string{"README.md"} }))
	if d.Tier != ReviewTierFocused {
		t.Fatalf("legacy mode tier = %s, want focused (pre-011 unconditional)", d.Tier)
	}
	if !strings.Contains(d.Rationale, "legacy baseline") {
		t.Fatalf("legacy rationale = %q", d.Rationale)
	}
}

// Legacy mode also restores single-signal Large classification so baseline
// runs escalate tasks to the heaviest class exactly as the old build did.
func TestLegacyModeRestoresSingleSignalLarge(t *testing.T) {
	reviewLegacyMode = true
	defer func() { reviewLegacyMode = false }()
	legacy := Classify("audit the project files for problems", "")
	if legacy.Class != ClassLarge {
		t.Fatalf("legacy single-signal class = %s, want large", legacy.Class)
	}
	reviewLegacyMode = false
	current := Classify("audit the project files for problems", "")
	if current.Class == ClassLarge {
		t.Fatalf("current single-signal class = %s, must NOT be large (D2 corroboration)", current.Class)
	}
}

// T022/D6: post-outcome stamping — ceiling-hit detection from the subagent
// status and the lenient Coverage-line parse.
func TestUpdateTaskReviewOutcome(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "cov", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.setTaskReviewDecision(ReviewDecision{Tier: ReviewTierFocused, Rationale: "r"})
	engine.updateTaskReviewOutcome("Subagent \"review\" report [status: partial (token ceiling)] (2 turns...)\nFindings: none.\nCoverage: covered=a.go, b.go; skipped=none")
	d := engine.takeTaskReviewDecision()
	if d == nil || !d.CeilingHit {
		t.Fatalf("ceiling hit not stamped: %+v", d)
	}
	if d.Coverage != "covered=a.go, b.go; skipped=none" {
		t.Fatalf("coverage = %q", d.Coverage)
	}
}
