# snare

Mutation testing for Go, powered by Claude. snare analyzes your git changes,
generates realistic mutations scoped to the modified code, then runs tests to
see which mutations get caught. The result is a **mutation coverage** score that
tells you how well your tests guard against real bugs.

## Requirements

- Go 1.25+
- git
- An [Anthropic API key](https://console.anthropic.com/)

## Install

```bash
go install github.com/yiyuanh/snare@latest
```

Or build from source:

```bash
git clone https://github.com/yiyuanh/snare.git
cd snare
go build -o snare .
```

## Quick start

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# Test unstaged changes in the current repo
snare run

# Test only staged changes
snare run --staged

# Test a specific commit
snare run --commit abc1234
```

## Usage

```
snare run [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--staged` | `false` | Only analyze staged changes |
| `--commit <sha>` | | Analyze changes from a specific commit |
| `--dir <path>` | `.` | Working directory |
| `--model <name>` | `claude-sonnet-4-5-20250929` | Claude model to use |
| `--max-tests <n>` | `0` (unlimited) | Cap the number of generated tests |
| `-v`, `--verbose` | `false` | Show detailed output |
| `--dry-run` | `false` | Generate mutants and tests without executing |
| `--timeout <dur>` | `30s` | Timeout per test execution |

## How it works

1. **Diff extraction** -- reads `git diff` to find changed `.go` files (excluding tests).
2. **AST analysis** -- parses each file to identify functions whose bodies overlap with the diff.
3. **LLM generation** -- sends each changed function plus its diff context to Claude, which produces 2-3 realistic mutations and a catching test for each.
4. **Test execution** -- for every test/mutant pair, runs the test against the original code (must pass) then against the mutated code (must fail to be "catching").
5. **Assessment** -- scores each result for confidence, filtering out compilation failures, trivial mutants, and tests that fail on original code.

## Reading the report

```
═══════════════════════════════════════════════
  snare — Mutation Testing Report
═══════════════════════════════════════════════

  Mutation coverage:  2/5 caught (40%)
  ──────────────────────────────────
  Files analyzed:     3
  Functions analyzed: 4
  Mutants generated:  5
  Tests generated:    8
  Tests executed:     8
  Duration:           4.231s

── UNCAUGHT MUTATIONS (3) ─────────────────────

  These mutations could be introduced without any test catching them.

  1. [parseConfig] Off-by-one in loop bound
     - original:  i < len(items)
     + mutated:   i <= len(items)

── CAUGHT MUTATIONS (2) ───────────────────────

  1. [parseConfig] Wrong default value (92% confidence)
  2. [handleRequest] Dropped error return (100% confidence)
```

- **Uncaught mutations** are the actionable part -- they represent changes that could be introduced to your code without any test noticing.
- **Caught mutations** confirm that your tests already guard against those kinds of bugs.
- Use `--verbose` to see which tests were attempted for uncaught mutations and full test code for caught ones.
- Use `--dry-run` to inspect the generated mutants and tests without executing anything.

## License

MIT
