# Local LLM Integration (llama.cpp)

The project vendors [go-llama.cpp](https://github.com/go-skynet/go-llama.cpp), so the agents can run entirely offline on your hardware. When `OPENAI_API_KEY` is **not** set but `LOCAL_LLM_MODEL` points to a `.gguf` file, all agent replies are generated through llama.cpp instead of the OpenAI API.

> ⚠️ You must build ReplyChat with the `local_llm` build tag to enable the llama.cpp integration (e.g. `go run -tags local_llm ./src` or `make build LOCAL_LLMTAGS=-tags local_llm`). Without the tag the server skips the local bindings and continues using the hosted providers.

## 1. Build the native binding

The llama.cpp binding ships as C++ code. Build it once after cloning or whenever you update submodules:

```bash
make -C go-llama.cpp libbinding.a
```

This produces `go-llama.cpp/libbinding.a`, which is picked up automatically by CGO during `go build`/`go run`.

## 2. Download a GGUF model

Obtain any instruction-tuned `.gguf` model (e.g., Meta Llama 3 Instruct, Mistral, etc.) and place it somewhere accessible by the server (local disk, mounted volume, etc.).

Example paths:

- `/models/Meta-Llama-3-8B-Instruct.Q4_K_M.gguf`
- `~/models/mistral-7b-instruct.Q4_0.gguf`

## 3. Configure environment variables

Set `LOCAL_LLM_MODEL` (required) plus any tuning flags in `.env` or your shell:

```dotenv
# Required
LOCAL_LLM_MODEL=open_llama_3b/ggml-model-q4_0.gguf

# Common tuning knobs (all optional)
LOCAL_LLM_THREADS=8
LOCAL_LLM_CONTEXT=4096
LOCAL_LLM_MAX_TOKENS=1024
LOCAL_LLM_TOP_P=0.9
LOCAL_LLM_TOP_K=40
LOCAL_LLM_TEMPERATURE=0.7
LOCAL_LLM_STOP="\nUser:,\nSystem:"
LOCAL_LLM_TIMEOUT_SECONDS=120
```

Restart the server after editing the environment. On startup you'll see a log entry like:

```
local-llm: initialized model /models/Meta-Llama-3-8B-Instruct.Q4_K_M.gguf (ctx=4096, threads=8, tokens=1024)
```

## 4. Advanced options

| Variable | Default | Description |
| --- | --- | --- |
| `LOCAL_LLM_MODEL` | _required_ | Path to the `.gguf` file. If not absolute, it's resolved under `go-llama.cpp/llama.cpp/models/`. |
| `LOCAL_LLM_THREADS` | `runtime.NumCPU()` | Number of CPU threads used for sampling. |
| `LOCAL_LLM_CONTEXT` | `4096` | Context window passed to llama.cpp. |
| `LOCAL_LLM_MAX_TOKENS` | `1024` | Maximum tokens generated per response. |
| `LOCAL_LLM_TOP_P` | `0.9` | Nucleus sampling parameter. |
| `LOCAL_LLM_TOP_K` | `40` | Top-k sampling parameter. |
| `LOCAL_LLM_TEMPERATURE` | `0.7` | Sampling temperature. |
| `LOCAL_LLM_REPEAT_PENALTY` | `1.1` | Repetition penalty value. |
| `LOCAL_LLM_REPEAT_LAST_N` | `128` | Number of recent tokens to penalize/repeat and keep. |
| `LOCAL_LLM_BATCH` | `256` | Prompt batch size used for inference. |
| `LOCAL_LLM_GPU_LAYERS` | `0` | Number of layers to offload to the GPU (if compiled with CUDA/Metal/etc.). |
| `LOCAL_LLM_F16_MEMORY` | `false` | Enable f16 weights in memory (uses more RAM, faster). |
| `LOCAL_LLM_F16_KV` | `true` | Use f16 KV cache. Disable on extremely low-RAM systems. |
| `LOCAL_LLM_LOW_VRAM` | `false` | Toggle llama.cpp low-VRAM mode. |
| `LOCAL_LLM_MLOCK` | `false` | Ask llama.cpp to mlock weights (prevents swapping). |
| `LOCAL_LLM_NUMA` | `false` | Enable NUMA-aware allocation. |
| `LOCAL_LLM_MMAP` | `true` | Memory-map the model file. |
| `LOCAL_LLM_MAIN_GPU` | _empty_ | ID of the primary GPU. |
| `LOCAL_LLM_TENSOR_SPLIT` | _empty_ | Tensor split configuration for multi-GPU setups. |
| `LOCAL_LLM_STOP` | `"\nUser:,\nSystem:"` | Comma or pipe-delimited stop sequence list. |
| `LOCAL_LLM_TIMEOUT_SECONDS` | `90` | Per-request timeout before the server abandons the generation (model keeps running, but reply is discarded). |
| `LOCAL_LLM_SEED` | `-1` | Deterministic sampling when ≥ 0. |
| `LOCAL_LLM_PROMPT_CACHE` | _empty_ | Path to a llama.cpp prompt-cache/session file. |
| `LOCAL_LLM_FREQUENCY_PENALTY` | `0` | Frequency penalty (OpenAI-style). |
| `LOCAL_LLM_PRESENCE_PENALTY` | `0` | Presence penalty (OpenAI-style). |
| `LOCAL_LLM_DEBUG` | `false` | Enables verbose tokens from go-llama.cpp. |

> **Stop words:** Provide a comma- or pipe-separated list (e.g., `LOCAL_LLM_STOP="</s>,\nUser:"`). The default stops generation when the model starts a new `User:`/`System:` block in the prompt template.

## Provider priority

1. If `OPENAI_API_KEY` is set, the agents send prompts to the OpenAI Responses API (remote mode).
2. If no API key is present but `LOCAL_LLM_MODEL` is configured, the server uses llama.cpp (local mode).
3. Otherwise, the system falls back to deterministic template replies.

Clear `OPENAI_API_KEY` in `.env` if you want to force local inference even when you have a key stored in your shell.

## Troubleshooting

- **`make -C go-llama.cpp libbinding.a` fails:** Ensure you have a C++ compiler (`g++` or `clang++`) plus any GPU SDKs you plan to use (CUDA, ROCm, etc.).
- **`go test`/`go run` complains about `permission denied` on `~/.cache/go-build`:** Either grant write access or set `GOCACHE=$(pwd)/.gocache` before running Go commands.
- **Slow generations:** Reduce `LOCAL_LLM_MAX_TOKENS`, lower `LOCAL_LLM_CONTEXT`, or quantize the model further (Q4/Q5). Increase `LOCAL_LLM_THREADS` up to the number of physical cores.

With the llama.cpp path configured you can run the full application without any external network calls while keeping the same structured planning/commit workflow as the OpenAI-backed mode.
