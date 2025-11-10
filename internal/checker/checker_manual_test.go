package checker

import (
	"testing"
)

func TestManualChecker_ValidCondition_Filtering(t *testing.T) {
	c := &manualChecker{}

	condInvalid := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers: []Matcher{
			{MatchType: "<", Key: "unused", Value: "foo"},
			{MatchType: ">", Key: "unused", Value: "bar"},
		},
	}
	if err := c.ValidCondition(condInvalid); err == nil {
		t.Fatalf("expected error for invalid matchers, got nil")
	}

	condValid := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers: []Matcher{
			{MatchType: "=", Key: "unused", Value: "foo"},
			{MatchType: "!=", Key: "unused", Value: "bar"},
			{MatchType: "<", Key: "unused", Value: "baz"},
		},
	}
	if err := c.ValidCondition(condValid); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(condValid.Matchers) != 2 {
		t.Fatalf("expected 2 valid matchers after filtering, got %d", len(condValid.Matchers))
	}
}

func TestManualChecker_Check_EqualMatch(t *testing.T) {
	c := &manualChecker{}
	urlGood := "http://foo:8545"
	urlBad := "http://bar:8545"
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers:      []Matcher{{MatchType: "=", Key: "unused", Value: "foo"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{urlGood, urlBad}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[urlGood] {
		t.Fatalf("expected urlGood to be valid, got %v", ret[urlGood])
	}
	if ret[urlBad] {
		t.Fatalf("expected urlBad to be invalid, got %v", ret[urlBad])
	}
}

func TestManualChecker_Check_NotEqualMatch(t *testing.T) {
	c := &manualChecker{}
	urlHasBar := "http://bar:8545"
	urlNoBar := "http://foo:8545"
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers:      []Matcher{{MatchType: "!=", Key: "unused", Value: "bar"}},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{urlHasBar, urlNoBar}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret[urlHasBar] {
		t.Fatalf("expected false for '!=' when regex matches, got %v", ret[urlHasBar])
	}
	if !ret[urlNoBar] {
		t.Fatalf("expected true for '!=' when regex does not match, got %v", ret[urlNoBar])
	}
}

func TestManualChecker_Check_MultipleMatchers_AllMustPass(t *testing.T) {
	c := &manualChecker{}
	urlPass := "http://foo:8545"
	urlFailFoo := "http://bar:8545"
	urlFailNotEqual := "http://foo.bar:8545"

	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers: []Matcher{
			{MatchType: "=", Key: "unused", Value: "foo"},
			{MatchType: "!=", Key: "unused", Value: "bar"},
		},
	}
	caches := CheckCaches{}

	ret, err := c.Check("1", []string{urlPass, urlFailFoo, urlFailNotEqual}, cond, caches)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ret[urlPass] {
		t.Fatalf("expected true for urlPass, got %v", ret[urlPass])
	}
	if ret[urlFailFoo] {
		t.Fatalf("expected false for urlFailFoo, got %v", ret[urlFailFoo])
	}
	if ret[urlFailNotEqual] {
		t.Fatalf("expected false for urlFailNotEqual, got %v", ret[urlFailNotEqual])
	}
}

func TestManualChecker_Check_InvalidRegex_Panics(t *testing.T) {
	c := &manualChecker{}
	cond := &HealthCheckCondition{
		CheckStrategy: CHECK_STRATEGY_MANUAL,
		Matchers:      []Matcher{{MatchType: "=", Key: "unused", Value: "("}},
	}
	caches := CheckCaches{}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for invalid regex, got none")
		}
	}()
	_, _ = c.Check("1", []string{"http://foo:8545"}, cond, caches)
}
