# Agents Package

This package handles AI agent message processing and responses.

## Current Implementation

The current implementation is a **simulation** that demonstrates the architecture. It uses keyword matching to trigger agent responses.

## Files

**processor.go:**
- `ProcessMessage()` - Entry point for processing user messages
- `analyzeAndRespond()` - Keyword detection and agent selection
- `simulateAgentResponse()` - Generates simulated agent responses
- `proposeTask()` - Creates task proposals

## Agent Types

Three built-in agents:

1. **Product Manager** (`product_manager`)
   - Keywords: requirement, feature, user story, scope, plan, need, want, build, create
   - Focus: Requirements gathering, feature planning

2. **Backend Architect** (`backend_architect`)
   - Keywords: api, backend, database, server, endpoint, design
   - Focus: API design, database architecture

3. **Frontend Developer** (`frontend_developer`)
   - Keywords: ui, frontend, component, react, interface, implement
   - Focus: UI components, responsive design

## How It Works

1. User sends message via WebSocket
2. Message saved to database
3. `ProcessMessage()` called asynchronously
4. Keywords analyzed to determine agent type
5. Agent response generated (currently simulated)
6. Response saved to database
7. Response broadcast to all connected clients
8. Optional: Task proposal created

## Adding New Agents

Edit `processor.go`:

```go
// 1. Add keywords
keywords := map[string]string{
    "test": "qa_engineer",
    "bug": "qa_engineer",
    // ... existing keywords
}

// 2. Add response template
responses := map[string]string{
    "qa_engineer": `I can help with testing and quality assurance.

I'll focus on:
- Writing test cases
- Automated testing setup
- Bug tracking and verification

Would you like me to create a testing plan?`,
}

// 3. Add display name
agentNames := map[string]string{
    "qa_engineer": "QA Engineer",
}

// 4. Add task template (optional)
taskTitles := map[string]string{
    "qa_engineer": "Create comprehensive test suite",
}

taskDescriptions := map[string]string{
    "qa_engineer": "Develop unit, integration, and e2e tests with coverage reporting",
}
```

## OpenAI GPT Integration

The system is now integrated with OpenAI GPT API.

### Current Implementation

The processor.go file includes:
- OpenAI SDK integration
- GPT-4o-mini model usage
- System prompts for each agent type
- Fallback responses when API is unavailable
- 30-second timeout for API calls
- 300 token limit for responses

### Configuration

Set environment variable:
```bash
export OPENAI_API_KEY=your_key_here
```

Or add to .env file:
```bash
OPENAI_API_KEY=your_key_here
```

### How It Works

```go
func (p *MessageProcessor) generateAgentResponse(projectID, agentType, originalMessage string) {
    // System prompts define agent personality
    systemPrompts := map[string]string{
        "product_manager": "You are a Product Manager AI...",
        "backend_architect": "You are a Backend Architect AI...",
        "frontend_developer": "You are a Frontend Developer AI...",
    }

    // Call OpenAI API
    resp, err := p.aiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: openai.GPT4oMini,
        Messages: []openai.ChatCompletionMessage{
            {Role: openai.ChatMessageRoleSystem, Content: systemPrompts[agentType]},
            {Role: openai.ChatMessageRoleUser, Content: originalMessage},
        },
        MaxTokens: 300,
        Temperature: 0.7,
    })

    // Use response or fallback
    if err != nil {
        responseText = p.getFallbackResponse(agentType)
    } else {
        responseText = resp.Choices[0].Message.Content
    }
}
```

### 4. Add Structured Output Parsing

```go
type Block struct {
    Type    string
    Content map[string]string
}

func (p *MessageProcessor) parseStructuredOutput(content string) []Block {
    var blocks []Block

    // Parse @message, @issue, @artifact blocks
    // Format:
    // @type
    // key: value
    // ---

    blockRegex := regexp.MustCompile(`@(\w+)\n([\s\S]*?)---`)
    matches := blockRegex.FindAllStringSubmatch(content, -1)

    for _, match := range matches {
        blockType := match[1]
        blockContent := match[2]

        parsed := parseYAML(blockContent)
        blocks = append(blocks, Block{
            Type: blockType,
            Content: parsed,
        })
    }

    return blocks
}

func (p *MessageProcessor) executeBlock(block Block, projectID, agentID string) {
    switch block.Type {
    case "message":
        p.postMessage(block.Content, projectID, agentID)
    case "issue":
        p.createIssue(block.Content, projectID, agentID)
    case "artifact":
        p.saveArtifact(block.Content, projectID, agentID)
    case "mention":
        p.notifyAgent(block.Content, projectID)
    case "dialog":
        p.createDialog(block.Content, projectID, agentID)
    }
}
```

## Message Flow

```
User Message
    ↓
WebSocket → handleChatMessage()
    ↓
Save to DB
    ↓
Broadcast to clients
    ↓
ProcessMessage() (async)
    ↓
analyzeAndRespond()
    ↓
[CURRENT] simulateAgentResponse()
[FUTURE]  callAnthropicAPI()
    ↓
Parse structured output
    ↓
Execute blocks (@issue, @artifact, etc.)
    ↓
Broadcast agent response
```

## Context Building

For real AI integration, build context with:

```go
type Context struct {
    ProjectInfo      string
    RecentMessages   []Message
    RelatedArtifacts []Artifact
    CurrentTask      *Issue
}

func (p *MessageProcessor) buildContext(projectID, content string) Context {
    // Fetch recent messages
    messages := p.fetchRecentMessages(projectID, 30)

    // Fetch relevant artifacts
    artifacts := p.fetchRelevantArtifacts(projectID, content)

    // Get project description
    projectInfo := p.fetchProjectInfo(projectID)

    return Context{
        ProjectInfo:      projectInfo,
        RecentMessages:   messages,
        RelatedArtifacts: artifacts,
    }
}
```

## Task Queue Management

For autonomous agent execution:

```go
func (p *MessageProcessor) startTaskExecution(agentID string) {
    for {
        task := p.getNextTask(agentID)
        if task == nil {
            break
        }

        p.updateTaskStatus(task.ID, "inProgress")
        p.updateAgentStatus(agentID, "working")

        context := p.buildTaskContext(task)
        result := p.callAnthropicAPI(agentID, task, context)

        p.processResult(task, result)
        p.updateTaskStatus(task.ID, "review")
        p.updateAgentStatus(agentID, "idle")
    }
}
```

## Testing

Create test file `processor_test.go`:

```go
package agents

import (
    "testing"
)

func TestKeywordDetection(t *testing.T) {
    tests := []struct {
        message string
        expected string
    }{
        {"need to build a feature", "product_manager"},
        {"design the API", "backend_architect"},
        {"create UI component", "frontend_developer"},
    }

    for _, tt := range tests {
        // Test keyword matching logic
    }
}
```

Run tests:
```bash
go test ./src/agents/...
```

## Future Enhancements

- Streaming responses for real-time output
- Multi-agent collaboration with @mention
- Agent memory and learning
- Tool use (code execution, API calls)
- Agent performance metrics
- Cost tracking per agent
- Custom agent creation by users
