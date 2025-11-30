# OpenAI Integration Guide

ReplyChat uses OpenAI's GPT-4o-mini through the official Go SDK (`github.com/openai/openai-go/v3`). Agents now return real code changes that are applied to each project's workspace. Prefer a fully offline flow? Configure the bundled llama.cpp backend described in `LOCAL_LLM.md`.

## What Changed

- **Before:** Simulated agent responses with hardcoded placeholder text.
- **After:** Real Responses API calls that generate executable action plans for the filesystem.

## Configuration

Create or update your `.env` file:

```bash
OPENAI_API_KEY=sk-proj-***
```

Restart the server after changing API keys so the new value is picked up.

## How It Works

1. **User sends a message.** Keyword and mention parsing determines which agent (PM, Backend, Frontend) should respond.
2. **Workspace reminder injected.** If the project already has a workspace on disk, we inject `Project workspace root: ...` as a system message so the model never escapes that directory.
3. **Responses API call.** `agents/processor.go` invokes `aiClient.Responses.New` with GPT-4o-mini, a role-specific system prompt, and the action-plan contract (`planFormatInstructions`).
4. **Action plan parsed.** We attempt to unmarshal the output into `{files, mutations, notes}`. If parsing fails, we fall back to displaying the raw text.
5. **Filesystem updated.** Valid plans are applied to `data/projects/<project-id>` using native `os` and `filepath` helpers to create files, edit text, and protect against directory traversal.
6. **Summary broadcast.** A short message summarizing how many files/mutations were applied is emitted back into the chat stream along with any plan notes.
7. **Fallback path.** If the API is unavailable or produces invalid JSON, we log the error and send the original conversational response so the UI never stalls.

## Workspace Action Plan Schema

Agents are instructed to emit minified JSON that matches the `AgentActionPlan` struct:

```json
{
  "files": [
    {"path": "src/foo.go", "content": "package foo", "overwrite": true}
  ],
  "mutations": [
    {"path": "README.md", "find": "TODO", "replace": "Done"}
  ],
  "notes": [
    "Summaries or follow-up actions"
  ]
}
```

- **files** – Full-file writes. We create parent directories, optionally skip if the file already exists (`overwrite=false`), and write contents with `os.WriteFile`.
- **mutations** – Targeted string replacements performed via `strings.Replace` on the first match to avoid over-editing.
- **notes** – Short strings that surface in the UI so users know what changed.

See `planFormatInstructions` in `agents/processor.go` for the exact contract we send to the model.

## Filesystem Application Flow

1. `projectfs.SetupProjectWorkspace` provisions `data/projects/<id>` using either `git init` or `git clone` depending on the user's selection.
2. `MessageProcessor.ensureWorkspace` loads persisted settings and creates the directory on-demand if the agent runs later.
3. `applyActionPlan` iterates over `files` and `mutations`, calling `secureJoin` to guarantee the relative paths never escape the workspace root.
4. Any error is logged and reflected back to the chat so the team can retry or adjust the prompt.

Because agents write directly to disk with Go's standard library, you can open the workspace in your editor, run tests, or commit changes immediately after an agent responds.

## Testing the Integration

1. Start the dev server.

   ```bash
   make dev
   ```

2. Open the app at `http://localhost:8080` and create or select a project.
3. Mention an agent (for example `@pm help me define the onboarding flow`).
4. Inspect the workspace under `data/projects/<project-id>` to see the files created by the plan.

## Agent Prompts

Each agent gets a role-specific system message plus the shared plan contract.

### Product Manager

```text
You are a Product Manager AI agent in a collaborative team workspace.
Your role is to gather requirements, create user stories, and define project scope.
Be concise and helpful. Ask clarifying questions when needed.
Keep responses under 200 words.
```

### Backend Architect

```text
You are a Backend Architect AI agent in a collaborative team workspace.
Your role is to design APIs, database schemas, and server architecture.
Be technical but clear. Provide concrete suggestions.
Keep responses under 200 words.
```

### Frontend Developer

```text
You are a Frontend Developer AI agent in a collaborative team workspace.
Your role is to build UI components, handle state management, and ensure responsive design.
Be practical and focus on implementation. Share best practices.
Keep responses under 200 words.
```

## Monitoring & Logging

Logs include high-level tracing so you can verify each phase:

```text
2025/01/01 12:00:00 agent: processing message for product_manager
2025/01/01 12:00:01 workspace: ensured data/projects/1234
2025/01/01 12:00:02 agent: OpenAI response received (245 tokens)
2025/01/01 12:00:02 agent: backend_architect updated workspace data/projects/1234 (files=1, mutations=2)
```

Errors are similarly reported:

```text
2025/01/01 12:05:00 agent: failed to apply plan for project 1234: path ../escape attempts to escape workspace
```

## Switching Models or Parameters

We now call the Responses API directly. To switch models or tweak parameters, edit the `Responses.New` invocation:

```go
resp, err := p.aiClient.Responses.New(ctx, responses.ResponseNewParams{
    Model:           openai.ResponsesModel(openai.ChatModelGPT4oMini),
    Input:           inputMessages,
    MaxOutputTokens: openai.Int(300),
    Temperature:     openai.Float(0.7),
})
```

You can replace `openai.ChatModelGPT4oMini` with `openai.ChatModelGPT4o`, adjust `MaxOutputTokens`, or lower `Temperature` for more deterministic plans.

## Using the OpenAI Agents SDK

The new OpenAI Agents SDK provides a higher-level abstraction over the Responses API. To migrate:

1. Instantiate an Agent with the same system prompt and plan instructions.
2. Register a **File System tool** that mirrors the `{files, mutations}` contract (or use the built-in File Output capability).
3. Call `client.Agents.Runs.Create` with the latest conversation message and poll until completion.
4. Reuse `applyActionPlan` by parsing the tool outputs, or switch to the SDK's native file-stream APIs if you prefer uploads/downloads over direct disk writes.

The current implementation keeps full control over the workspace, but the contract is intentionally aligned with the Agents SDK so future migrations are straightforward.

## Troubleshooting

- **No response:** Confirm `OPENAI_API_KEY` is set, the server restarted, and the message contains a supported keyword or mention.
- **Plan parsing failed:** Inspect logs for `unable to parse agent plan output`; usually the model returned prose. Re-phrase the request or tighten the prompt.
- **Workspace errors:** Look for `workspace:` logs (missing git binary, clone failure, or path escape attempts) and resolve the underlying filesystem issue.
- **API rate limits:** The fallback path sends a short error message. Consider adding exponential backoff or upgrading the OpenAI plan for production workloads.

## Security Notes

- `.env` stays git-ignored. Never commit your API keys.
- Rotate keys periodically and set spending limits in the OpenAI dashboard.
- The workspace guardrail message plus `secureJoin` prevents agents from escaping `data/projects/<id>`.

## Next Steps

1. Monitor costs and response quality via the OpenAI dashboard.
2. Layer in streaming responses for incremental UI updates.
3. Capture conversation history or files as additional context for richer plans.
4. Experiment with the Agents SDK's tool calling once you need multi-step workflows.

## Cost Snapshot

- GPT-4o-mini input: ~$0.15 per million tokens.
- GPT-4o-mini output: ~$0.60 per million tokens.
- Average plan (≈300 tokens total) costs ~$0.00014, so 1,000 agent responses land around $0.14.

Your free trial quota easily covers typical hackathon usage.
