package rules

import (
	"testing"
)

func TestAddRule(t *testing.T) {
	e := NewEngine()

	r, err := e.Add(TypeRemote, ".google.com", "", true)
	if err != nil {
		t.Fatalf("failed to add rule: %v", err)
	}
	if r.Type != TypeRemote {
		t.Errorf("expected type remote, got %s", r.Type)
	}
	if r.Enabled == nil || !*r.Enabled {
		t.Error("expected rule to be enabled")
	}
	if r.ID == "" {
		t.Error("expected rule to have an ID")
	}
}

func TestMatchExact(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, "google.com", "", true)

	result := e.Match("google.com")
	if !result.Matched {
		t.Error("expected exact match for google.com")
	}
	if result.RuleType != TypeRemote {
		t.Errorf("expected type remote, got %s", result.RuleType)
	}

	result = e.Match("www.google.com")
	if result.Matched {
		t.Error("expected no match for www.google.com with exact rule")
	}
}

func TestMatchSuffix(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, ".google.com", "", true)

	result := e.Match("www.google.com")
	if !result.Matched {
		t.Error("expected suffix match for www.google.com")
	}

	result = e.Match("google.com")
	if !result.Matched {
		t.Error("expected suffix match for google.com itself")
	}

	result = e.Match("notgoogle.com")
	if result.Matched {
		t.Error("expected no match for notgoogle.com")
	}
}

func TestMatchWildcard(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, "*.google.com", "", true)

	result := e.Match("www.google.com")
	if !result.Matched {
		t.Error("expected wildcard match for www.google.com")
	}

	result = e.Match("mail.google.com")
	if !result.Matched {
		t.Error("expected wildcard match for mail.google.com")
	}
}

func TestMatchKeyword(t *testing.T) {
	e := NewEngine()
	e.Add(TypeBlock, "keyword:ads", "", true)

	result := e.Match("ads.example.com")
	if !result.Matched {
		t.Error("expected keyword match for ads.example.com")
	}

	result = e.Match("example.com")
	if result.Matched {
		t.Error("expected no keyword match for example.com")
	}
}

func TestMatchRegex(t *testing.T) {
	e := NewEngine()
	_, err := e.Add(TypeBlock, "regexp:^ads.*\\.com$", "", true)
	if err != nil {
		t.Fatalf("failed to add regex rule: %v", err)
	}

	result := e.Match("ads.example.com")
	if !result.Matched {
		t.Error("expected regex match for ads.example.com")
	}

	result = e.Match("example.com")
	if result.Matched {
		t.Error("expected no regex match for example.com")
	}
}

func TestMatchDisabled(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, ".google.com", "", false)

	result := e.Match("www.google.com")
	if result.Matched {
		t.Error("expected no match for disabled rule")
	}
}

func TestBlockRule(t *testing.T) {
	e := NewEngine()
	e.Add(TypeBlock, ".ad.example.com", "", true)

	result := e.Match("www.ad.example.com")
	if !result.Matched {
		t.Error("expected match")
	}
	if result.RuleType != TypeBlock {
		t.Errorf("expected type block, got %s", result.RuleType)
	}
}

func TestRedirectRule(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRedirect, "example.local", "192.168.1.100", true)

	result := e.Match("example.local")
	if !result.Matched {
		t.Error("expected match")
	}
	if result.RuleType != TypeRedirect {
		t.Errorf("expected type redirect, got %s", result.RuleType)
	}
	if result.Target != "192.168.1.100" {
		t.Errorf("expected target 192.168.1.100, got %s", result.Target)
	}
}

func TestRulePriority(t *testing.T) {
	e := NewEngine()
	e.Add(TypeDomestic, ".baidu.com", "", true)
	e.Add(TypeRemote, ".baidu.com", "", true)

	result := e.Match("www.baidu.com")
	if !result.Matched {
		t.Error("expected match")
	}
	if result.RuleType != TypeDomestic {
		t.Errorf("expected first rule (domestic) to match, got %s", result.RuleType)
	}
}

func TestUpdateRule(t *testing.T) {
	e := NewEngine()
	r, _ := e.Add(TypeRemote, ".google.com", "", true)

	enabled := true
	updated, err := e.Update(r.ID, TypeDomestic, ".baidu.com", "", &enabled)
	if err != nil {
		t.Fatalf("failed to update rule: %v", err)
	}
	if updated.Domain != ".baidu.com" {
		t.Errorf("expected domain .baidu.com, got %s", updated.Domain)
	}
	if updated.Type != TypeDomestic {
		t.Errorf("expected type domestic, got %s", updated.Type)
	}
}

func TestDeleteRule(t *testing.T) {
	e := NewEngine()
	r, _ := e.Add(TypeRemote, ".google.com", "", true)

	err := e.Delete(r.ID)
	if err != nil {
		t.Fatalf("failed to delete rule: %v", err)
	}

	result := e.Match("www.google.com")
	if result.Matched {
		t.Error("expected no match after deletion")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	e := NewEngine()
	err := e.Delete("nonexistent")
	if err == nil {
		t.Error("expected error when deleting nonexistent rule")
	}
}

func TestReorderRules(t *testing.T) {
	e := NewEngine()
	r1, _ := e.Add(TypeDomestic, ".domestic.com", "", true)
	r2, _ := e.Add(TypeRemote, ".remote.com", "", true)

	err := e.Reorder([]string{r2.ID, r1.ID})
	if err != nil {
		t.Fatalf("failed to reorder: %v", err)
	}

	rules := e.List("", false)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != r2.ID {
		t.Errorf("expected first rule to be %s, got %s", r2.ID, rules[0].ID)
	}
}

func TestListFiltered(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, ".google.com", "", true)
	e.Add(TypeDomestic, ".baidu.com", "", true)
	e.Add(TypeBlock, ".ad.com", "", true)

	remoteRules := e.List(TypeRemote, false)
	if len(remoteRules) != 1 {
		t.Errorf("expected 1 remote rule, got %d", len(remoteRules))
	}

	allRules := e.List("", false)
	if len(allRules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(allRules))
	}
}

func TestInvalidRegex(t *testing.T) {
	e := NewEngine()
	_, err := e.Add(TypeBlock, "regexp:[invalid", "", true)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestCaseInsensitiveMatch(t *testing.T) {
	e := NewEngine()
	e.Add(TypeRemote, ".google.com", "", true)

	result := e.Match("WWW.GOOGLE.COM")
	if !result.Matched {
		t.Error("expected case-insensitive match")
	}
}
