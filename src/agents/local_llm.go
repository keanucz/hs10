//go:build local_llm

package agents

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	llama "github.com/go-skynet/go-llama.cpp"
)

type LocalLLM struct {
	model       *llama.LLama
	mu          sync.Mutex
	predictOpts []llama.PredictOption
	cfg         localLLMConfig
}

type localLLMConfig struct {
	modelPath       string
	contextSize     int
	threads         int
	maxTokens       int
	topK            int
	topP            float64
	temperature     float64
	repeatPenalty   float64
	repeatLastN     int
	batch           int
	stopWords       []string
	timeout         time.Duration
	debug           bool
	seed            int
	gpuLayers       int
	useF16Memory    bool
	useF16KV        bool
	useLowVRAM      bool
	useMLock        bool
	useNUMA         bool
	useMMap         bool
	mainGPU         string
	tensorSplit     string
	freqPenalty     float64
	presencePenalty float64
	promptCachePath string
}

var (
	localLLMOnce sync.Once
	localLLMInst *LocalLLM
	localLLMErr  error
)

func getLocalLLM() (*LocalLLM, error) {
	localLLMOnce.Do(func() {
		localLLMInst, localLLMErr = newLocalLLMFromEnv()
	})
	return localLLMInst, localLLMErr
}

func newLocalLLMFromEnv() (*LocalLLM, error) {
	rawModelPath := strings.TrimSpace(os.Getenv("LOCAL_LLM_MODEL"))
	if rawModelPath == "" {
		return nil, nil
	}

	modelPath, err := resolveModelPath(rawModelPath)
	if err != nil {
		return nil, err
	}

	cfg := localLLMConfig{
		modelPath:       modelPath,
		contextSize:     envInt("LOCAL_LLM_CONTEXT", 4096),
		threads:         envInt("LOCAL_LLM_THREADS", runtime.NumCPU()),
		maxTokens:       envInt("LOCAL_LLM_MAX_TOKENS", 1024),
		topK:            envInt("LOCAL_LLM_TOP_K", 40),
		topP:            envFloat("LOCAL_LLM_TOP_P", 0.9),
		temperature:     envFloat("LOCAL_LLM_TEMPERATURE", 0.7),
		repeatPenalty:   envFloat("LOCAL_LLM_REPEAT_PENALTY", 1.1),
		repeatLastN:     envInt("LOCAL_LLM_REPEAT_LAST_N", 128),
		batch:           envInt("LOCAL_LLM_BATCH", 256),
		timeout:         time.Duration(envInt("LOCAL_LLM_TIMEOUT_SECONDS", 90)) * time.Second,
		debug:           envBool("LOCAL_LLM_DEBUG", false),
		seed:            envInt("LOCAL_LLM_SEED", -1),
		gpuLayers:       envInt("LOCAL_LLM_GPU_LAYERS", 0),
		useF16Memory:    envBool("LOCAL_LLM_F16_MEMORY", false),
		useF16KV:        envBool("LOCAL_LLM_F16_KV", true),
		useLowVRAM:      envBool("LOCAL_LLM_LOW_VRAM", false),
		useMLock:        envBool("LOCAL_LLM_MLOCK", false),
		useNUMA:         envBool("LOCAL_LLM_NUMA", false),
		useMMap:         envBool("LOCAL_LLM_MMAP", true),
		mainGPU:         strings.TrimSpace(os.Getenv("LOCAL_LLM_MAIN_GPU")),
		tensorSplit:     strings.TrimSpace(os.Getenv("LOCAL_LLM_TENSOR_SPLIT")),
		freqPenalty:     envFloat("LOCAL_LLM_FREQUENCY_PENALTY", 0),
		presencePenalty: envFloat("LOCAL_LLM_PRESENCE_PENALTY", 0),
		promptCachePath: strings.TrimSpace(os.Getenv("LOCAL_LLM_PROMPT_CACHE")),
	}

	if cfg.timeout <= 0 {
		cfg.timeout = 90 * time.Second
	}

	stops := parseStopWords(os.Getenv("LOCAL_LLM_STOP"))
	if len(stops) == 0 {
		stops = []string{"\nUser:", "\nSystem:"}
	}
	cfg.stopWords = stops

	modelOpts := []llama.ModelOption{llama.SetContext(cfg.contextSize), llama.SetNBatch(cfg.batch), llama.SetMMap(cfg.useMMap)}
	if cfg.useF16Memory {
		modelOpts = append(modelOpts, llama.EnableF16Memory)
	}
	if cfg.useLowVRAM {
		modelOpts = append(modelOpts, llama.EnabelLowVRAM)
	}
	if cfg.useNUMA {
		modelOpts = append(modelOpts, llama.EnableNUMA)
	}
	if cfg.useMLock {
		modelOpts = append(modelOpts, llama.EnableMLock)
	}
	if cfg.gpuLayers > 0 {
		modelOpts = append(modelOpts, llama.SetGPULayers(cfg.gpuLayers))
	}
	if cfg.mainGPU != "" {
		modelOpts = append(modelOpts, llama.SetMainGPU(cfg.mainGPU))
	}
	if cfg.tensorSplit != "" {
		modelOpts = append(modelOpts, llama.SetTensorSplit(cfg.tensorSplit))
	}
	if cfg.seed >= 0 {
		modelOpts = append(modelOpts, llama.SetModelSeed(cfg.seed))
	}

	model, err := llama.New(modelPath, modelOpts...)
	if err != nil {
		return nil, fmt.Errorf("local-llm: load model: %w", err)
	}

	predictOpts := []llama.PredictOption{
		llama.SetThreads(cfg.threads),
		llama.SetTokens(cfg.maxTokens),
		llama.SetTopK(cfg.topK),
		llama.SetTopP(float32(cfg.topP)),
		llama.SetTemperature(float32(cfg.temperature)),
		llama.SetPenalty(float32(cfg.repeatPenalty)),
		llama.SetRepeat(cfg.repeatLastN),
		llama.SetNKeep(cfg.repeatLastN),
		llama.SetBatch(cfg.batch),
	}
	if cfg.seed >= 0 {
		predictOpts = append(predictOpts, llama.SetSeed(cfg.seed))
	}
	if cfg.useF16KV {
		predictOpts = append(predictOpts, llama.EnableF16KV)
	}
	if cfg.freqPenalty != 0 {
		predictOpts = append(predictOpts, llama.SetFrequencyPenalty(float32(cfg.freqPenalty)))
	}
	if cfg.presencePenalty != 0 {
		predictOpts = append(predictOpts, llama.SetPresencePenalty(float32(cfg.presencePenalty)))
	}
	if cfg.promptCachePath != "" {
		predictOpts = append(predictOpts, llama.SetPathPromptCache(cfg.promptCachePath))
	}
	if len(cfg.stopWords) > 0 {
		predictOpts = append(predictOpts, llama.SetStopWords(cfg.stopWords...))
	}
	if cfg.debug {
		predictOpts = append(predictOpts, llama.Debug)
	}

	log.Printf("local-llm: initialized model %s (ctx=%d, threads=%d, tokens=%d)",
		modelPath, cfg.contextSize, cfg.threads, cfg.maxTokens)

	return &LocalLLM{
		model:       model,
		predictOpts: predictOpts,
		cfg:         cfg,
	}, nil
}

func (l *LocalLLM) Generate(ctx context.Context, systemPrompt, workspaceHint, userMessage string) (string, error) {
	if l == nil {
		return "", fmt.Errorf("local-llm: not configured")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if l.cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.cfg.timeout)
		defer cancel()
	}

	prompt := buildLocalPrompt(systemPrompt, workspaceHint, userMessage)
	type result struct {
		text string
		err  error
	}
	resultCh := make(chan result, 1)

	go func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		text, err := l.model.Predict(prompt, l.predictOpts...)
		resultCh <- result{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resultCh:
		return strings.TrimSpace(res.text), res.err
	}
}

func buildLocalPrompt(systemPrompt, workspaceHint, userMessage string) string {
	var b strings.Builder
	sys := strings.TrimSpace(systemPrompt)
	if sys != "" {
		b.WriteString("System:\n")
		b.WriteString(sys)
		b.WriteString("\n\n")
	}

	workspace := strings.TrimSpace(workspaceHint)
	if workspace != "" {
		b.WriteString("Additional context:\n")
		b.WriteString(workspace)
		b.WriteString("\n\n")
	}

	b.WriteString("User:\n")
	b.WriteString(strings.TrimSpace(userMessage))
	b.WriteString("\n\nAssistant:")
	return b.String()
}

func parseStopWords(raw string) []string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "|", ","))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func envInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("local-llm: invalid integer %s=%q: %v", key, val, err)
		return def
	}
	return parsed
}

func envFloat(key string, def float64) float64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(val, 64)
	if err != nil {
		log.Printf("local-llm: invalid float %s=%q: %v", key, val, err)
		return def
	}
	return parsed
}

func envBool(key string, def bool) bool {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if val == "" {
		return def
	}
	switch val {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		log.Printf("local-llm: invalid bool %s=%q, using default %v", key, val, def)
		return def
	}
}

func resolveModelPath(input string) (string, error) {
	if input == "" {
		return "", errors.New("local-llm: LOCAL_LLM_MODEL is empty")
	}

	// absolute path given
	if filepath.IsAbs(input) {
		return input, nil
	}

	// try relative to current working directory
	if abs, err := filepath.Abs(input); err == nil {
		if fileExists(abs) {
			return abs, nil
		}
	}

	// try under repo models directory
	root := projectRoot()
	if root != "" {
		candidate := filepath.Join(root, "go-llama.cpp", "llama.cpp", "models", input)
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("local-llm: model file %q not found (also checked go-llama.cpp/llama.cpp/models/%s)", input, input)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var (
	projectRootOnce sync.Once
	projectRootPath string
)

func projectRoot() string {
	projectRootOnce.Do(func() {
		if root, err := findProjectRoot(); err == nil {
			projectRootPath = root
		} else {
			log.Printf("local-llm: unable to locate project root: %v", err)
		}
	})
	return projectRootPath
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("go.mod not found in parent directories")
}
