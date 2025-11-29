package agents

import "strings"

type mentionTrigger struct {
	token string
	agent string
}

type keywordRule struct {
	keyword  string
	agent    string
	priority int
	wordOnly bool
}

var mentionTriggers = []mentionTrigger{
	{token: "@pm", agent: "product_manager"},
	{token: "@backend", agent: "backend_architect"},
	{token: "@frontend", agent: "frontend_developer"},
	{token: "@qa", agent: "qa_tester"},
	{token: "@devops", agent: "devops_engineer"},
}

var keywordRules = []keywordRule{
	// Backend cues carry the highest priority so they win over generic verbs.
	{keyword: "backend", agent: "backend_architect", priority: 100},
	{keyword: "back-end", agent: "backend_architect", priority: 100},
	{keyword: "api", agent: "backend_architect", priority: 90, wordOnly: true},
	{keyword: "database", agent: "backend_architect", priority: 90},
	{keyword: "schema", agent: "backend_architect", priority: 80},
	{keyword: "server", agent: "backend_architect", priority: 75},
	{keyword: "architecture", agent: "backend_architect", priority: 70},
	{keyword: "design", agent: "backend_architect", priority: 65},

	// Frontend cues are next in priority.
	{keyword: "frontend", agent: "frontend_developer", priority: 100},
	{keyword: "front-end", agent: "frontend_developer", priority: 100},
	{keyword: "ui", agent: "frontend_developer", priority: 90, wordOnly: true},
	{keyword: "component", agent: "frontend_developer", priority: 80},
	{keyword: "interface", agent: "frontend_developer", priority: 75},
	{keyword: "implement", agent: "frontend_developer", priority: 60},

	// QA tester cues.
	{keyword: "test", agent: "qa_tester", priority: 85},
	{keyword: "qa", agent: "qa_tester", priority: 85, wordOnly: true},
	{keyword: "verify", agent: "qa_tester", priority: 70},
	{keyword: "bug", agent: "qa_tester", priority: 65},
	{keyword: "regression", agent: "qa_tester", priority: 65},
	{keyword: "automated", agent: "qa_tester", priority: 60},

	// DevOps cues.
	{keyword: "deploy", agent: "devops_engineer", priority: 90},
	{keyword: "deployment", agent: "devops_engineer", priority: 90},
	{keyword: "infrastructure", agent: "devops_engineer", priority: 85},
	{keyword: "pipeline", agent: "devops_engineer", priority: 80},
	{keyword: "ci", agent: "devops_engineer", priority: 75, wordOnly: true},
	{keyword: "cd", agent: "devops_engineer", priority: 75, wordOnly: true},
	{keyword: "docker", agent: "devops_engineer", priority: 70},

	// Product management keywords are intentionally lower priority.
	{keyword: "requirement", agent: "product_manager", priority: 60},
	{keyword: "feature", agent: "product_manager", priority: 55},
	{keyword: "need", agent: "product_manager", priority: 50},
	{keyword: "want", agent: "product_manager", priority: 45},
	{keyword: "build", agent: "product_manager", priority: 40},
	{keyword: "create", agent: "product_manager", priority: 35},
	{keyword: "plan", agent: "product_manager", priority: 30},
}

// DetectAgent inspects the message content and returns the agent that should
// respond. Mentions win immediately. Otherwise, we look for keywords and pick
// the agent with the highest priority match so that specific cues like
// "backend" outrank generic verbs like "build".
func DetectAgent(content string) string {
	contentLower := strings.ToLower(content)

	for _, trigger := range mentionTriggers {
		if strings.Contains(contentLower, trigger.token) {
			return trigger.agent
		}
	}

	selectedAgent := ""
	maxPriority := -1
	for _, rule := range keywordRules {
		if keywordMatches(contentLower, rule) {
			if rule.priority > maxPriority {
				selectedAgent = rule.agent
				maxPriority = rule.priority
			}
		}
	}

	return selectedAgent
}

func keywordMatches(content string, rule keywordRule) bool {
	if rule.wordOnly {
		return containsWholeWord(content, rule.keyword)
	}
	return strings.Contains(content, rule.keyword)
}

func containsWholeWord(content, keyword string) bool {
	index := strings.Index(content, keyword)
	for index != -1 {
		startOK := index == 0 || !isAlphaNum(content[index-1])
		endIdx := index + len(keyword)
		endOK := endIdx == len(content) || !isAlphaNum(content[endIdx])
		if startOK && endOK {
			return true
		}
		next := strings.Index(content[index+1:], keyword)
		if next == -1 {
			break
		}
		index = index + 1 + next
	}
	return false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
