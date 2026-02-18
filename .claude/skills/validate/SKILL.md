---
name: validate
description: Run all project validation checks — tests, linter, vet, and build. Use after making code changes to verify correctness.
allowed-tools: Bash
argument-hint: [package-pattern]
---

Run all four checks below, capturing output and exit codes separately. Do not stop on the first failure — always run all checks.

Run them in this order:

1. **Build**: `go build ./...`
2. **Vet**: `go vet ./...`
3. **Lint**: `golangci-lint run ./...`
4. **Test**: `go test -race -count=1 ./...`
   - If `$ARGUMENTS` is provided, use it as the package pattern instead of `./...` (e.g., `go test -race -count=1 $ARGUMENTS`)

After all checks complete, print a summary table showing pass/fail for each step.

If any check failed, show the relevant error output below the summary.
