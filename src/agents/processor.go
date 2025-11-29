package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type MessageProcessor struct {
	db        *sql.DB
	broadcast chan<- []byte
	aiClient  *openai.Client
}

type AgentResponse struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

func ProcessMessage(db *sql.DB, broadcast chan<- []byte, projectID, content, userID string) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	var client *openai.Client
	if apiKey != "" {
		newClient := openai.NewClient(option.WithAPIKey(apiKey))
		client = &newClient
	}

	processor := &MessageProcessor{
		db:        db,
		broadcast: broadcast,
		aiClient:  client,
	}

	processor.analyzeAndRespond(projectID, content, userID)
}

func (p *MessageProcessor) analyzeAndRespond(projectID, content, userID string) {
	contentLower := strings.ToLower(content)

	mentions := map[string]string{
		"@pm":       "product_manager",
		"@backend":  "backend_architect",
		"@frontend": "frontend_developer",
	}

	var triggeredAgent string

	for mention, agent := range mentions {
		if strings.Contains(contentLower, mention) {
			triggeredAgent = agent
			break
		}
	}

	if triggeredAgent == "" {
		keywords := map[string]string{
			"requirement": "product_manager",
			"feature":     "product_manager",
			"api":         "backend_architect",
			"backend":     "backend_architect",
			"database":    "backend_architect",
			"ui":          "frontend_developer",
			"frontend":    "frontend_developer",
			"component":   "frontend_developer",
			"need":        "product_manager",
			"want":        "product_manager",
			"build":       "product_manager",
			"create":      "product_manager",
			"design":      "backend_architect",
			"implement":   "frontend_developer",
		}

		for keyword, agent := range keywords {
			if strings.Contains(contentLower, keyword) {
				triggeredAgent = agent
				break
			}
		}
	}

	if triggeredAgent == "" {
		return
	}

	go p.generateAgentResponse(projectID, triggeredAgent, content)
}

func (p *MessageProcessor) generateAgentResponse(projectID, agentType, originalMessage string) {
	agentNames := map[string]string{
		"product_manager":    "Product Manager",
		"backend_architect":  "Backend Architect",
		"frontend_developer": "Frontend Developer",
	}

	var responseText string

	if p.aiClient != nil {
		systemPrompts := map[string]string{
			"product_manager": `You are a Product Manager AI agent in a collaborative team workspace.
	Your role is to gather requirements, create user stories, and define project scope.
	Be concise and helpful. Ask clarifying questions when needed.
	Keep responses under 200 words.`,

			"backend_architect": `You are a Backend Architect AI agent in a collaborative team workspace.
Your role is to design APIs, database schemas, and server architecture.
Be technical but clear. Provide concrete suggestions.
Keep responses under 200 words.`,

			"frontend_developer": `You are a Frontend Developer AI agent in a collaborative team workspace.
Your role is to build UI components, handle state management, and ensure responsive design.
Be practical and focus on implementation. Share best practices.
Keep responses under 200 words.`,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		inputMessages := responses.ResponseInputParam{
			responses.ResponseInputItemParamOfMessage(systemPrompts[agentType], responses.EasyInputMessageRoleSystem),
			responses.ResponseInputItemParamOfMessage(originalMessage, responses.EasyInputMessageRoleUser),
		}

		resp, err := p.aiClient.Responses.New(ctx, responses.ResponseNewParams{
			Model:           openai.ResponsesModel(openai.ChatModelGPT4oMini),
			Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: inputMessages},
			MaxOutputTokens: openai.Int(300),
			Temperature:     openai.Float(0.7),
		})

		if err != nil {
			log.Printf("agent: OpenAI API error: %v", err)
			responseText = p.getFallbackResponse(agentType)
		} else if output := resp.OutputText(); output != "" {
			responseText = output
		} else {
			responseText = p.getFallbackResponse(agentType)
		}
	} else {
		log.Printf("agent: No OpenAI API key configured, using fallback")
		responseText = p.getFallbackResponse(agentType)
	}

	messageID := uuid.New().String()
	timestamp := time.Now()

	_, err := p.db.Exec(`
		INSERT INTO messages (id, project_id, sender_id, sender_type, content, message_type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, messageID, projectID, agentType, "agent", responseText, "chat", timestamp)

	if err != nil {
		log.Printf("agent: failed to save response: %v", err)
		return
	}

	response := AgentResponse{
		Type: "message.received",
		Payload: map[string]interface{}{
			"message": map[string]interface{}{
				"id":          messageID,
				"projectId":   projectID,
				"senderId":    agentType,
				"senderType":  "agent",
				"senderName":  agentNames[agentType],
				"content":     responseText,
				"messageType": "chat",
				"timestamp":   timestamp,
			},
		},
	}

	responseJSON, _ := json.Marshal(response)
	p.broadcast <- responseJSON

	if strings.Contains(strings.ToLower(originalMessage), "create task") ||
		strings.Contains(strings.ToLower(originalMessage), "add task") {
		p.proposeTask(projectID, agentType)
	}
}

func (p *MessageProcessor) getFallbackResponse(agentType string) string {
	responses := map[string]string{
		"product_manager": `I understand you need help with requirements. Let me analyze what you're asking for.

Based on your message, I can help break this down into actionable tasks. Would you like me to create some initial user stories and features?`,

		"backend_architect": `I can help with the backend architecture for this feature.

Here's what I'm thinking:
- Design the database schema
- Create REST API endpoints
- Set up proper error handling and validation

Should I create tasks for these items?`,

		"frontend_developer": `I can help build the frontend components for this.

I'll focus on:
- Creating reusable UI components
- Implementing responsive design
- Ensuring good UX patterns

Let me know if you'd like me to start on any specific part.`,
	}

	if response, ok := responses[agentType]; ok {
		return response
	}
	return "I'm ready to help with this task."
}

func (p *MessageProcessor) proposeTask(projectID, agentType string) {
	time.Sleep(1 * time.Second)

	taskID := uuid.New().String()
	timestamp := time.Now()

	taskTitles := map[string]string{
		"product_manager":    "Define user requirements and acceptance criteria",
		"backend_architect":  "Design API endpoints and database schema",
		"frontend_developer": "Create responsive UI components",
	}

	taskDescriptions := map[string]string{
		"product_manager":    "Gather and document user requirements, create user stories with clear acceptance criteria",
		"backend_architect":  "Design RESTful API structure and database schema with proper relationships",
		"frontend_developer": "Build reusable React components with responsive design and accessibility",
	}

	_, err := p.db.Exec(`
		INSERT INTO issues (id, project_id, title, description, priority, status, created_by, created_by_type, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, taskID, projectID, taskTitles[agentType], taskDescriptions[agentType], "medium", "proposed", agentType, "agent", timestamp)

	if err != nil {
		log.Printf("agent: failed to create task: %v", err)
		return
	}

	response := AgentResponse{
		Type: "issue.created",
		Payload: map[string]interface{}{
			"issue": map[string]interface{}{
				"id":          taskID,
				"projectId":   projectID,
				"title":       taskTitles[agentType],
				"description": taskDescriptions[agentType],
				"priority":    "medium",
				"status":      "proposed",
				"createdBy":   agentType,
			},
			"requiresApproval": true,
		},
	}

	responseJSON, _ := json.Marshal(response)
	p.broadcast <- responseJSON
}
