# Contributing to AviationWX.org Bridge

Thank you for your interest in contributing to AviationWX.org Bridge! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project adheres to a Code of Conduct that all contributors are expected to follow. Please read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before participating.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/AviationWX.org-Bridge.git
   cd AviationWX.org-Bridge
   ```
3. **Set up local development** - See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)

## Development Setup

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). Quick start:

```bash
# Copy v2 config templates into docker/data
mkdir -p docker/data/cameras
cp configs/global.json.example docker/data/global.json
cp configs/camera-http.json.example docker/data/cameras/camera-1.json

# Edit with test credentials (never commit real credentials)
# Then start Docker
docker compose -f docker/docker-compose.yml up -d
```

## How to Contribute

### Reporting Bugs

1. **Check existing issues** to see if the bug is already reported
2. **Create a new issue** with:
   - Clear title and description
   - Steps to reproduce
   - Expected vs actual behavior
   - Environment details (Go version, Docker version, device type, etc.)
   - Error messages or logs (without sensitive data)

### Suggesting Features

1. **Check existing issues** for similar suggestions
2. **Create a feature request** with:
   - Use case and motivation
   - Proposed solution or implementation ideas
   - Any related issues

### Code Contributions

1. **Create a branch** for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/your-bug-fix
   ```

2. **Make your changes** following our coding standards:
   - Follow [CODE_STYLE.md](CODE_STYLE.md) guidelines
   - Add concise comments only for critical or unclear logic
   - **Write tests for all new functionality**
   - Update documentation for user-facing changes
   - Write clear commit messages

3. **Test your changes**:
   ```bash
   # Run tests
   go test ./...
   
   # Run linter
   go vet ./...
   
   # Check formatting
   gofmt -s -w .
   
   # Test with Docker
   docker compose -f docker/docker-compose.yml up -d
   ```

4. **Commit your changes**:
   ```bash
   git add .
   git commit -m "Description of your changes"
   ```

5. **Push and create a Pull Request**:
   ```bash
   git push origin feature/your-feature-name
   ```
   Then create a PR on GitHub with:
   - Clear title and description
   - Reference related issues
   - Testing notes

## Coding Standards

**See [CODE_STYLE.md](CODE_STYLE.md) for complete coding standards and guidelines.**

**Important**: This is a **safety-critical application** used by pilots for flight decisions. Code quality, reliability, and graceful degradation are paramount.

### Quick Reference

- Follow Go standard formatting (`gofmt`, `go vet`)
- Use meaningful variable and function names
- Keep functions focused and single-purpose
- **Comments should be concise** - only comment critical or unclear logic
- **Critical paths must have test coverage**
- **Handle errors explicitly** - Don't silently fail (safety-critical)
- Use structured logging with context

### Security Guidelines

- **Never commit sensitive data** (API keys, passwords, credentials)
- Use `configs/global.json.example` and `configs/camera-*.json.example` as templates
- Validate and sanitize all input
- Follow security best practices

### Documentation

- Update relevant documentation files for user-facing changes
- Add inline comments for complex logic
- Update README.md if adding new features
- Keep code examples in documentation accurate

## Pull Request Process

1. **Ensure your code works** and doesn't break existing functionality
2. **Update documentation** for any changes that affect users or developers
3. **Keep commits focused** - one logical change per commit
4. **Write clear commit messages**:
   ```
   Short summary (50 chars or less)
   
   More detailed explanation if needed. Wrap at 72 characters.
   Explain what and why vs. how.
   ```

5. **Respond to feedback** promptly and professionally
6. **Wait for review** before merging (even if you have write access)

## Areas for Contribution

### Code Improvements

- **Performance optimization**: Memory usage, CPU efficiency
- **Error handling**: Better error messages and logging
- **Code quality**: Refactoring, removing duplication
- **Testing**: Unit tests, integration tests

### Documentation

- **User documentation**: Clearer setup instructions
- **API documentation**: Better endpoint documentation
- **Code comments**: Clarifying complex functions
- **Examples**: More configuration examples

### Features

- **New camera types**: Additional camera protocol support
- **UI/UX improvements**: Better web console experience
- **Monitoring**: Better health checks and metrics
- **Reliability**: Improved backoff and retry logic

## Questions?

- Open an issue for questions or discussions
- Check existing documentation first
- Be respectful and constructive in all communications

Thank you for contributing to AviationWX.org Bridge! 🛩️









