# Agent Role Prompts

This directory contains role-specific prompts for AI agents working on this codebase.

## Roles

### Developer (`developer.md`)
The **Developer** role is responsible for:
- Implementing features and fixes
- Writing code according to specifications
- Following existing patterns and conventions
- Minimizing token usage while maintaining quality

### Reviewer (`reviewer.md`)
The **Reviewer** role is responsible for:
- Reviewing code for correctness and quality
- Ensuring adherence to project standards
- Providing actionable feedback
- Catching bugs and issues early

### Tester (`tester.md`)
The **Tester** role is responsible for:
- Writing unit and integration tests
- Ensuring code correctness through testing
- Testing edge cases and error handling
- Verifying concurrency safety

## How to Use

### For Single-Agent Workflows
Use the **Developer** prompt when:
- Implementing new features
- Fixing bugs
- Making code changes
- Refactoring

### For Multi-Agent Workflows

**Standard Workflow:**
1. **Developer** implements changes using `developer.md` prompt
2. **Reviewer** reviews changes using `reviewer.md` prompt
3. **Developer** addresses feedback
4. **Reviewer** approves code
5. **Tester** writes tests using `tester.md` prompt
6. **Reviewer** reviews tests (optional)
7. All tests pass → Complete

**Fast Workflow (for simple changes):**
1. **Developer** implements changes
2. **Reviewer** reviews and approves
3. **Tester** adds tests
4. Complete

### Token Optimization
All roles are designed to minimize token usage:
- Focused file reading (line ranges, not full files)
- Concise communication
- Batch operations
- Quick approvals when code is correct
- Reusable test utilities

## Workflow Example

```
1. User: "Add a -timeout flag to the bank tool"

2. Developer (using developer.md):
   - Reads main.go (flag section)
   - Reads bank.go (Config struct)
   - Implements the change
   - Submits: "Added -timeout flag. Updated main.go and bank.go."

3. Reviewer (using reviewer.md):
   - Reviews changed sections
   - Checks integration
   - Approves or requests fixes

4. Developer:
   - Addresses any feedback
   - Resubmits if needed

5. Reviewer:
   - Approves final version

6. Tester (using tester.md):
   - Reads implemented code
   - Writes tests for flag parsing and usage
   - Runs tests and verifies
   - Submits: "Added tests for -timeout flag. Tests flag parsing, 
     context timeout, and expiration. Files: main_test.go, bank_test.go"

7. Reviewer (optional):
   - Reviews tests
   - Approves if tests are comprehensive

8. All tests pass → Complete
```

## Benefits

- **Reduced Token Cost**: Focused reviews and concise communication
- **Better Code Quality**: Systematic review process catches issues early
- **Faster Iteration**: Clear roles and workflows
- **Consistency**: Both roles follow project standards

## Integration with agents.md

These role prompts complement the `agents.md` file:
- `agents.md`: Technical documentation about the codebase
- `prompts/developer.md`: How to write code for this project
- `prompts/reviewer.md`: How to review code for this project
- `prompts/tester.md`: How to write tests for this project