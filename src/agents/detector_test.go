package agents

import "testing"

func TestDetectAgentMentionOverridesKeywords(t *testing.T) {
	agent := DetectAgent("Please @backend take a look at this API idea")
	if agent != "backend_architect" {
		t.Fatalf("expected backend_architect, got %s", agent)
	}
}

func TestDetectAgentPrefersSpecificBackendKeywords(t *testing.T) {
	agent := DetectAgent("Let's build the backend and database layer next")
	if agent != "backend_architect" {
		t.Fatalf("expected backend_architect, got %s", agent)
	}
}

func TestDetectAgentFrontendKeywords(t *testing.T) {
	agent := DetectAgent("Need help polishing the UI components")
	if agent != "frontend_developer" {
		t.Fatalf("expected frontend_developer, got %s", agent)
	}
}

func TestDetectAgentProductManagerFallback(t *testing.T) {
	agent := DetectAgent("We need to build a plan for the next feature")
	if agent != "product_manager" {
		t.Fatalf("expected product_manager, got %s", agent)
	}
}

func TestDetectAgentReturnsEmptyWhenNoMatch(t *testing.T) {
	agent := DetectAgent("random chatter with no cues")
	if agent != "" {
		t.Fatalf("expected empty agent match, got %s", agent)
	}
}
