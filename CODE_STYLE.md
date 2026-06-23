# Code Style Guide

This document outlines coding standards and development practices for AviationWX.org Bridge. All contributors should follow these guidelines.

**Important**: This is a **safety-critical application** used by pilots for flight decisions. Code quality, reliability, and graceful degradation are paramount.

## Table of Contents

- [Safety & Reliability](#safety--reliability)
- [Go Standards](#go-standards)
- [Comments and Documentation](#comments-and-documentation)
- [Testing Requirements](#testing-requirements)
- [Error Handling](#error-handling)
- [Configuration Management](#configuration-management)
- [Project Identifiers](#project-identifiers)
- [Logging](#logging)
- [Dependencies](#dependencies)
- [Git Workflow](#git-workflow)

---

## Safety & Reliability

### Freshness Requirement

**Critical**: This application is used for flight safety decisions. Image freshness must be guaranteed.

- **Always capture fresh**: Every upload attempt must start with a new capture
- **Never queue stale images**: No local queuing of older images; never "catch up" by uploading stale frames
- **Fail closed**: If capture fails, don't upload old data

### Per-Camera Degradation

- **Each camera degrades independently** - One camera's failure doesn't affect others
- **Never fail silently** - Always log failures with context
- **Automatic recovery** - Use exponential backoff, but ensure at least one attempt per day
- **Circuit breaker pattern** - Prevent hammering failing cameras

### Camera Reliability

- **Design for unreliable cameras** - Cameras may have poor network connectivity
- **Strict timeouts** - Configurable timeouts for all operations (capture, upload)
- **Graceful degradation** - Continue operating other cameras when one fails
- **Recovery logging** - Log recovery events for operational visibility

### Time Health

- **SNTP validation** - Periodically check time accuracy
- **Conditional EXIF** - Only stamp EXIF when time is healthy
- **Fail gracefully** - Continue operation even if time check fails (server uses mtime)

---

## Go Standards

### Formatting

- **Always use `gofmt`**: Run `gofmt -s -w .` before committing
- **Use `go vet`**: Run `go vet ./...` to catch common mistakes
- **Follow standard Go conventions**: See [Effective Go](https://go.dev/doc/effective_go)

### Code Organization

- **Package structure**: Organize by domain (camera, config, upload, scheduler, etc.)
- **Internal packages**: Use `internal/` for private code
- **Public packages**: Use `pkg/` only when code should be importable
- **File naming**: Use `snake_case.go` for files (e.g., `http.go`, `scheduler.go`)

### Naming Conventions

- **Packages**: lowercase, single word (e.g., `camera`, `config`)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Variables**: camelCase
- **Constants**: UPPER_SNAKE_CASE
- **Types**: PascalCase

### Type Hints and Interfaces

- **Use interfaces**: Define interfaces for testability and flexibility
- **Accept interfaces, return structs**: Make functions flexible
- **Type assertions**: Handle errors explicitly with `ok` checks

**Example:**
```go
type Camera interface {
    Capture(ctx context.Context) ([]byte, error)
}

type HTTPCamera struct {
    URL string
}

func (c *HTTPCamera) Capture(ctx context.Context) ([]byte, error) {
    // Implementation
}
```

### Function Design

- **Keep functions focused**: Small to medium sized functions
- **Single responsibility**: One function, one purpose
- **Return errors explicitly**: Don't ignore errors
- **Use context.Context**: For cancellation and timeouts

**Example:**
```go
func CaptureSnapshot(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }
    // ...
}
```

---

## Comments and Documentation

### Comment Philosophy

**Keep comments concise and focused on critical logic.**

- ✅ **DO** comment:
  - Complex business logic or algorithms
  - Non-obvious behavior or edge cases
  - Safety-critical logic (time validation, error handling, freshness requirements)
  - Race conditions, concurrency issues, or file locking logic
  - The "why" behind a decision, not the "what"
  - Error suppression rationale
  - Public APIs (use godoc)

- ❌ **DON'T** comment:
  - Self-explanatory code
  - Obvious operations
  - Code that is clear from function/variable names
  - Verbose explanations of simple logic
  - Transitory comments explaining code changes (e.g., "Changed X to Y")
  - Comments that explain what the code does (code should be self-documenting)

### Comment Maintenance When Modifying Code

**When modifying existing code, refactor comments rather than just appending new ones.**

- ✅ **DO** refactor existing comments to reflect current code behavior
- ✅ **DO** remove outdated comments that no longer apply
- ✅ **DO** update comments when logic changes significantly
- ❌ **DON'T** append new comments without reviewing existing ones
- ❌ **DON'T** leave transitory comments like "Changed X to fix Y"

### Godoc Standards

- All exported functions, types, and packages must have godoc comments
- Start with the name of the function/type
- Keep descriptions concise (one line when possible)
- Use complete sentences

**Example:**
```go
// CaptureSnapshot fetches a snapshot from the given HTTP URL.
// It uses cache-busting headers and respects the provided timeout.
// Returns an error if the request fails or times out.
func CaptureSnapshot(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
    // ...
}
```

---

## Testing Requirements

### Test Coverage Policy

**Critical paths, complex logic, and integration tests are required.**

1. **New Features**: Must include tests for critical paths and complex logic
2. **Bug Fixes**: Must include tests that verify the fix
3. **Refactoring Tests**: Encouraged, but don't cheat positive results when bugs exist
4. **Test Organization**: Use `*_test.go` files alongside source files

### Test Naming

Use descriptive test function names:
```go
func TestCaptureSnapshot_Timeout_ReturnsError(t *testing.T) { }
func TestUploadSFTP_Success_ReturnsNil(t *testing.T) { }
func TestBackoff_Exponential_CalculatesCorrectly(t *testing.T) { }
```

### Test Quality Standards

- ✅ Tests should be independent (no execution order dependencies)
- ✅ Use table-driven tests when appropriate
- ✅ Use appropriate mocks and test fixtures
- ✅ Tests should be fast and reliable
- ✅ Focus on behavior, not implementation details
- ✅ Test error cases and edge cases

**Example:**
```go
func TestCaptureSnapshot_Timeout(t *testing.T) {
    tests := []struct {
        name    string
        timeout time.Duration
        wantErr bool
    }{
        {"valid timeout", 5 * time.Second, false},
        {"zero timeout", 0, true},
        {"negative timeout", -1 * time.Second, true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

---

## Error Handling

### Explicit Error Handling

**Critical**: This is a safety-critical application. Errors must be handled explicitly.

- **Never ignore errors**: Always check and handle errors
- **Wrap errors with context**: Use `fmt.Errorf("context: %w", err)`
- **Return errors early**: Fail fast, don't continue with invalid state
- **Log errors with context**: Include relevant information in error logs

**Example:**
```go
func UploadImage(ctx context.Context, data []byte, path string) error {
    conn, err := ftp.Dial(host)
    if err != nil {
        return fmt.Errorf("connect to FTP server: %w", err)
    }
    
    err = conn.Login(username, password)
    if err != nil {
        conn.Quit()
        return fmt.Errorf("FTP login: %w", err)
    }
    
    // ...
}
```

### Error Types

- Use `errors.New()` for simple errors
- Use `fmt.Errorf()` with `%w` to wrap errors
- Create custom error types for specific error categories (if needed)

---

## Configuration Management

### Config File Structure

- **Single config file**: `config.json` (read-only, no database)
- **Schema validation**: Validate schema on load, fail fast on bad config
- **Default values**: Provide sensible defaults for optional fields
- **Atomic writes**: Write to temp file, then rename (for web console edits)

### Config Error Handling

- **Fail fast**: Bad config should prevent startup
- **Clear error messages**: Errors should clearly indicate what's wrong
- **Validation**: Validate all required fields and types

---

## Project Identifiers

Use the correct form depending on context:

### `aviationwx.org-bridge` (with period)

**Use only for GitHub URLs—the repo name cannot be changed.**

- **GitHub repo path**: `alexwitherspoon/aviationwx.org-bridge`, `github.com/.../aviationwx.org-bridge`
- **API URLs**: `api.github.com/repos/alexwitherspoon/aviationwx.org-bridge/...`
- **Raw content URLs**: `raw.githubusercontent.com/.../aviationwx.org-bridge/...`

### `aviationwx-org-bridge` (with hyphen)

**Use everywhere else.** Hyphens avoid case-sensitivity and portability issues.

- **GHCR image path**: `ghcr.io/alexwitherspoon/aviationwx-org-bridge`
- **Container name**: `aviationwx-org-bridge`
- **Binary artifacts**: `aviationwx-org-bridge-linux-amd64`, etc.
- **Local Docker image tags**: `aviationwx-org-bridge:latest`, `aviationwx-org-bridge:local-test`
- **Package name** (package.json): `aviationwx-org-bridge`
- **User-Agent header**: `aviationwx-org-bridge/1.0.0`
- **.gitignore patterns**: `/aviationwx-org-bridge`
- **docker-compose service/container_name**: `aviationwx-org-bridge`

### Quick reference

| Context | Form |
|---------|------|
| GitHub URL, repo path | `aviationwx.org-bridge` |
| GHCR image path | `aviationwx-org-bridge` |
| Container name | `aviationwx-org-bridge` |
| Binary name | `aviationwx-org-bridge-{arch}` |
| Package name | `aviationwx-org-bridge` |

---

## Logging

### Structured Logging

- Use structured logging with context fields
- Include relevant context (camera ID, operation, etc.)
- Use appropriate log levels (debug, info, warn, error)
- Never log sensitive data (passwords, tokens)
- Log recovery events for operational visibility

**Example:**
```go
logger.Info("capture started",
    "camera_id", cameraID,
    "type", cameraType,
    "url", url,
)

logger.Error("capture failed",
    "camera_id", cameraID,
    "error", err,
    "retry_after", backoffSeconds,
)
```

### Logging Categories

Consider separate logging for:
1. **Internal system operations** - System health, recovery events, circuit breaker state
2. **Data acquisition activity** - Camera captures, upload results
3. **User activity** - Web console access, config changes

---

## Dependencies

### Minimize Dependencies

**Preference**: Minimize dependencies to keep project robust long-term.

- **Prefer Go stdlib** - Use standard library when possible
- **Use dependencies when complexity is high** - Don't rebuild mature software (e.g., ffmpeg)
- **Avoid simple dependencies** - Don't use a dependency for simple JSON parsing (stdlib has `encoding/json`)
- **Review with maintainer** - Discuss new dependencies before adding
- **Document rationale** - Document why each dependency is needed

**Examples:**
- ✅ **Use**: ffmpeg (complex, mature software)
- ❌ **Avoid**: Simple utility libraries (implement in stdlib)
- ✅ **Use**: go-onvif (ONVIF is complex)
- ✅ **Use**: SFTP only (pkg/sftp)

---

## Git Workflow

### Commit Messages

Follow this format:

```
Short summary (50 chars or less)

More detailed explanation if needed. Wrap at 72 characters.
Explain what and why vs. how.
```

**Examples:**
```
Fix SFTP upload timeout handling

The upload timeout was not being respected when network
conditions were poor. Added explicit timeout handling
to prevent hanging uploads.

Fixes #123
```

```
Add ONVIF camera support

Implements ONVIF snapshot capture using go-onvif library.
Supports basic authentication and profile token selection.

Breaking change: Requires ONVIF credentials in config.
```

### Branch Naming

- `feature/` - New features (e.g., `feature/onvif-support`)
- `fix/` - Bug fixes (e.g., `fix/sftp-timeout`)
- `refactor/` - Code refactoring (e.g., `refactor/scheduler`)
- `test/` - Test improvements (e.g., `test/add-integration-tests`)
- `docs/` - Documentation updates (e.g., `docs/update-readme`)

### Prose and PRs

Write commit messages, PR descriptions, and review replies in plain technical English. Match existing repo tone: direct, specific, no filler.

- Use commas or separate sentences instead of em dashes or en dashes in prose.
- Do not use Unicode symbols or emoji in new docs, PR text, or script output (no checkmarks, crosses, warning signs, arrows). ASCII is fine for code and tables.
- Avoid filler openers and padding ("Additionally", "Furthermore", "It's worth noting", "comprehensive", "leverage", "delve into", "ensure that").
- State what changed and why; skip meta commentary about the change process.

**Exception:** GitHub Copilot is used for automated PR review. Keep [.github/copilot-instructions.md](.github/copilot-instructions.md) current. Replying to Copilot review threads on PRs is expected; use the same plain voice and cite commit SHAs. Do not reference other AI or agent tooling in committed text.

**PR shape:** Summary or Description, bullet Changes, Validation or Testing checklist, `Closes #N` when applicable. Dependency batches may use a Risk/Notes table. Breaking changes get their own section when config or behavior shifts.

### Breaking Changes

- **Breaking changes are OK** - Project is young, large improvements are welcome
- **Full PR process** - Always use PRs for breaking changes
- **Document breaking changes** - Clearly document what changed and migration path
- **Update examples** - Ensure `config.json.example` and docs are updated

---

## Development Checklist

Before submitting code:

- [ ] Code follows Go standards (`gofmt`, `go vet`)
- [ ] Critical paths and complex logic have tests
- [ ] All tests pass (`go test ./...`)
- [ ] Comments are concise and only where needed
- [ ] Godoc comments added for exported functions/types
- [ ] No sensitive data committed
- [ ] Errors handled explicitly
- [ ] Config validation passes
- [ ] Breaking changes documented
- [ ] Examples and docs updated

---

## Additional Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Contributing Guide](CONTRIBUTING.md)
- [Architecture Documentation](docs/CONFIG_SCHEMA.md)

---

**Remember**: This is a safety-critical application. Code quality, reliability, and graceful degradation are paramount. When in doubt, prioritize safety and clarity over cleverness.

