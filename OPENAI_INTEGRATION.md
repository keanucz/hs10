# OpenAI Integration Guide

ReplyChat now integrates with OpenAI's GPT API for real AI agent responses.

## What Changed

**Before:** Simulated agent responses with hardcoded text
**After:** Real AI responses from OpenAI GPT-4o-mini

## Configuration

Your API key is already configured in `.env`:
```bash
OPENAI_API_KEY=sk-proj-***
```

## How It Works

1. **User sends message** containing trigger keywords
2. **System detects agent type** based on keywords:
   - "requirement", "feature", "need", "want", "build" → Product Manager
   - "api", "backend", "database", "design" → Backend Architect
   - "ui", "frontend", "component", "implement" → Frontend Developer

3. **Agent calls OpenAI API** with:
   - Model: GPT-4o-mini (cost-effective)
   - System prompt: Agent role and personality
   - User message: Your chat message
   - Max tokens: 300 (keeps responses concise)
   - Temperature: 0.7 (balanced creativity)

4. **Response displayed** in chat interface
5. **Fallback** to simulated response if API fails

## Testing

### Start the server:
```bash
make dev
```

### Open browser:
```
http://localhost:8080
```

### Test messages:

**Trigger Product Manager:**
```
I need to build a user authentication feature
```

**Trigger Backend Architect:**
```
How should I design the API for this?
```

**Trigger Frontend Developer:**
```
Help me create a dashboard UI component
```

## Features

**System Prompts:**
Each agent has a specialized system prompt that defines:
- Role and responsibilities
- Response style
- Focus areas
- Length constraints (under 200 words)

**Error Handling:**
- 30-second timeout per API call
- Automatic fallback to simulated responses
- Graceful error logging

**Cost Optimization:**
- Uses GPT-4o-mini (cheapest model)
- Limits responses to 300 tokens
- No unnecessary API calls

## Cost Breakdown

**GPT-4o-mini pricing:**
- Input: $0.15 per million tokens
- Output: $0.60 per million tokens

**Per message estimate:**
- Input: ~100 tokens (system prompt + user message)
- Output: ~200 tokens (agent response)
- Cost: ~$0.00014 per message

**Usage scenarios:**
- 100 messages: ~$0.014 (~1.4 cents)
- 1,000 messages: ~$0.14 (~14 cents)
- 10,000 messages: ~$1.40

Your free API key should handle thousands of messages.

## Agent System Prompts

**Product Manager:**
```
You are a Product Manager AI agent in a collaborative team workspace.
Your role is to gather requirements, create user stories, and define project scope.
Be concise and helpful. Ask clarifying questions when needed.
Keep responses under 200 words.
```

**Backend Architect:**
```
You are a Backend Architect AI agent in a collaborative team workspace.
Your role is to design APIs, database schemas, and server architecture.
Be technical but clear. Provide concrete suggestions.
Keep responses under 200 words.
```

**Frontend Developer:**
```
You are a Frontend Developer AI agent in a collaborative team workspace.
Your role is to build UI components, handle state management, and ensure responsive design.
Be practical and focus on implementation. Share best practices.
Keep responses under 200 words.
```

## Monitoring

**Logs show:**
- API call success/failure
- Fallback triggers
- Response processing

**Example log output:**
```
2025/01/01 12:00:00 agent: processing message for product_manager
2025/01/01 12:00:02 agent: OpenAI response received (245 tokens)
2025/01/01 12:00:02 agent: saved response to database
```

If API key is invalid:
```
2025/01/01 12:00:00 agent: OpenAI API error: invalid api key
2025/01/01 12:00:00 agent: using fallback response
```

## Switching Models

To use a different OpenAI model, edit `src/agents/processor.go`:

```go
resp, err := p.aiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
    Model: openai.GPT4oMini,  // Change this line
    // ...
})
```

Available models:
- `openai.GPT4oMini` - Cheapest, fast (current)
- `openai.GPT4o` - More capable, higher cost
- `openai.GPT4Turbo` - Balanced performance
- `openai.GPT35Turbo` - Older, cheaper

## Troubleshooting

**No agent response:**
- Check `.env` file has OPENAI_API_KEY set
- Verify API key is valid at https://platform.openai.com/api-keys
- Check logs for error messages
- Ensure message contains trigger keywords

**API rate limit:**
- Free tier has rate limits
- Responses fallback to simulated if limit hit
- Consider upgrading to paid tier for production

**Slow responses:**
- Normal latency: 1-3 seconds
- Timeout set to 30 seconds
- Check internet connection
- Monitor OpenAI status page

**Cost concerns:**
- Current limits: 300 tokens per response
- Monitor usage at https://platform.openai.com/usage
- Set up billing alerts
- Consider caching common responses

## Advanced: Context Building

To enhance responses with conversation history, edit processor.go:

```go
// Fetch recent messages
var recentMessages []string
rows, _ := p.db.Query(`
    SELECT content FROM messages
    WHERE project_id = ?
    ORDER BY timestamp DESC
    LIMIT 5
`, projectID)

for rows.Next() {
    var msg string
    rows.Scan(&msg)
    recentMessages = append(recentMessages, msg)
}

// Add to API call
messages := []openai.ChatCompletionMessage{
    {Role: openai.ChatMessageRoleSystem, Content: systemPrompts[agentType]},
}

// Add conversation history
for _, msg := range recentMessages {
    messages = append(messages, openai.ChatCompletionMessage{
        Role: openai.ChatMessageRoleUser,
        Content: msg,
    })
}

// Add current message
messages = append(messages, openai.ChatCompletionMessage{
    Role: openai.ChatMessageRoleUser,
    Content: originalMessage,
})
```

## Security Notes

- API key stored in .env (gitignored)
- Never commit .env to version control
- Rotate API keys regularly
- Monitor for unusual usage
- Set spending limits on OpenAI account

## Next Steps

1. Test the integration with real messages
2. Monitor costs and response quality
3. Adjust system prompts as needed
4. Add conversation history for better context
5. Implement streaming responses for real-time output
