# memctx

Personal memory context layer for LLM conversations.

Your AI chats don't talk to each other. memctx makes them.

## What it does

1. Store conversations from any LLM (copy/paste to file, then upload)
2. Semantic search with embeddings
3. Synthesize minimal context when starting a new chat

## Requirements

- Go 1.21+
- [Ollama](https://ollama.ai) running locally with:
  - `nomic-embed-text` (embeddings)
  - `llama3.2` (synthesis)

```bash
ollama pull nomic-embed-text
ollama pull llama3.2
```

## Install

```bash
go install github.com/prash2512/memctx@latest
```

Or build from source:

```bash
git clone https://github.com/prash2512/memctx.git
cd memctx
make build
```

## Usage

### Upload a conversation

```bash
memctx upload chat.txt
```

### Prime a new conversation

```bash
memctx prime "building worker pools for my eBPF project"
```

Output:
```
[Paste this at the start of your conversation]
────────────────────────────────────────────────────────
- Building an eBPF networking tool in Go
- Prefer channels over mutexes for concurrency
- Use context.Context for graceful shutdown
────────────────────────────────────────────────────────
```

### List stored conversations

```bash
memctx list
```

## How it works

1. **Upload**: Stores conversation text + generates embedding via Ollama
2. **Prime**: Embeds your intent → vector search → retrieves relevant conversations → LLM synthesizes minimal context
3. **Output**: Ready-to-paste context block for your new chat

## Config

| Flag | Default | Description |
|------|---------|-------------|
| `--db` | `~/.memctx.db` | SQLite database path |
| `--ollama` | `http://localhost:11434` | Ollama API URL |

## License

MIT

---

A personal project by [@prash2512](https://github.com/prash2512)

