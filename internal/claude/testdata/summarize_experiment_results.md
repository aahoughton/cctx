# Summarization Experiment Results

Date: 2026-03-26

## Setup
- LM Studio on localhost:1234
- 3 real conversations tested across different project types
- 4 system prompts + 1 inline (no system message) variant
- 3 models: ministral-3-14b-reasoning, gpt-oss-20b, qwen3-4

## Results: ministral-3-14b-reasoning (~16s for 3 conversations)

### CLI tool build session (2432 bytes compact)
| Prompt | Result |
|--------|--------|
| current | meta tool claude context analysis |
| structured | claude context analyzer |
| last-message-weight | context analysis tool |
| deliverable-focus | meta tool development |
| inline | (empty) |

### Game session (1231 bytes compact)
| Prompt | Result |
|--------|--------|
| current | game character collision fixes |
| structured | game physics adjustments |
| last-message-weight | game speed control and collision fix |
| deliverable-focus | game physics adjustments |
| inline | (empty) |

### Browser extension session (1418 bytes compact)
| Prompt | Result |
|--------|--------|
| current | claudebot plugin ui issues |
| structured | web ui classification fixes |
| last-message-weight | web ui classification integration |
| deliverable-focus | recents classification update |
| inline | (empty) |

## Results: qwen3-4 (~6s for 3 conversations)

### CLI tool build session
| Prompt | Result |
|--------|--------|
| current | context renaming tool |
| structured | context tool builder |
| last-message-weight | claude context rename tool |
| deliverable-focus | context command implementation |
| inline | context rename tool |

### Game session
| Prompt | Result |
|--------|--------|
| current | speed multiplier and collision fix |
| structured | speed multiplier and collision fix |
| last-message-weight | add speed multiplier and fix collision bounds |
| deliverable-focus | collision speed adjustment script |
| inline | collision speed fix |

### Browser extension session
| Prompt | Result |
|--------|--------|
| current | ui selector generation |
| structured | web ui selector library |
| last-message-weight | add colored marks to recents list items |
| deliverable-focus | ui selector collection for chat items |
| inline | fix sidebar item coloring |

## Results: gpt-oss-20b

All empty or control tokens (`<|channel|>`). This model does not follow
instructions for this task. Not viable for summarization.

## Analysis

### Model comparison

**qwen3-4** is the standout:
- 3x faster than ministral (6s vs 16s)
- Inline mode works (no system message needed) — better model compatibility
- More action-oriented names ("add speed multiplier and fix collision bounds")
- Sometimes too specific/literal at the expense of readability

**ministral-3-14b** is solid:
- More natural-sounding names ("game character collision fixes")
- Requires system messages (inline returns empty)
- Slower but still usable

### Best prompts

**"last-message-weight"** is the best overall strategy across both models.
It captures what actually happened rather than what was asked first:
- qwen3-4: "claude context rename tool", "add speed multiplier and fix collision bounds"
- ministral: "game speed control and collision fix", "web ui classification integration"

**"deliverable-focus"** is good but sometimes produces generic names
("meta tool development", "collision speed adjustment script").

**"current"** (original prompt) is reasonable but doesn't distinguish itself.

**"inline"** is important for model compatibility — works on qwen3-4 but
not ministral. Could be used as a fallback if system messages fail.

### Recommendations

1. **Default prompt**: Use the hybrid "deliverable-focus + last-message-weight"
   prompt now in production (merged both strategies into the name prompt).

2. **Model validation**: Added — rejects empty strings and control tokens
   (`<|`) with a clear error message.

3. **Recommended models** (for config.toml documentation):
   - qwen3-4: fast, accurate, good instruction following
   - ministral-3-14b-reasoning: reliable, natural-sounding

4. **gpt-oss-20b**: Not viable for instruction following.

5. **Compact representation**: 1-2KB is sufficient. No need to send more data.
