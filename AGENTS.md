# Agent Guide for Bank Transfer Simulation Tool

## Overview

This is a Go-based bank transfer simulation tool designed to test database systems (TiDB or MySQL) under concurrent load. It simulates a banking system where multiple accounts transfer money to each other while maintaining data consistency and verifying invariants.

## Purpose

The tool performs the following operations:
1. **Initialization**: Creates a specified number of bank accounts with initial balances
2. **Concurrent Transfers**: Continuously performs random money transfers between accounts
3. **Invariant Verification**: Periodically verifies that the total balance across all accounts remains constant (should equal `num_accounts * 1000`)
4. **Transaction Testing**: Tests both optimistic and pessimistic transaction modes
5. **Connection Testing**: Supports both short and long connection modes

## Architecture

### Core Components

- **`main.go`**: Entry point, handles command-line flags, database connection setup, and signal handling
- **`bank.go`**: Core business logic including:
  - `BankCase`: Main struct that orchestrates the simulation
  - Account initialization and data setup
  - Concurrent money transfer operations
  - Balance verification logic
- **`util.go`**: Database connection utilities and retry mechanisms
- **`error.go`**: Error handling and MySQL error code checking

### Key Data Structures

#### `BankCase`
The main orchestrator for the bank simulation:
- Manages concurrent workers
- Handles account initialization
- Executes transfer operations
- Performs balance verification

#### `Config`
Configuration for the bank test:
- `NumAccounts`: Total number of accounts to create
- `Interval`: Interval between verification checks
- `TableNum`: Number of account tables (for sharding/testing)
- `Concurrency`: Number of concurrent worker goroutines
- `EnableLongTxn`: Enable long-term transactions (with delays)
- `UseLongConn`: Use long connection mode (each goroutine has its own connection)
- `RetryLimit`: Maximum retry count for operations

#### `dbConn` Interface
Abstracts database connections to support both:
- `*sql.DB` (shared connection pool)
- `*sql.Conn` (dedicated per-goroutine connections)

## Usage

### Basic Command Structure

```bash
./bin/bank [flags]
```

### Required Flags

- `-addr`: Database address (e.g., `127.0.0.1:4000`)

### Optional Flags

- `-db`: Database name (default: `test`)
- `-user`: Database user (default: `root`)
- `-pw`: Database password (default: empty)
- `-accounts`: Number of accounts (default: `1000000`)
- `-concurrency`: Worker goroutine count (default: `200`)
- `-tables`: Number of account tables (default: `1`)
- `-interval`: Verification interval (default: `2s`)
- `-retry-limit`: Retry count for operations (default: `200`)
- `-long-txn`: Enable long-term transactions (default: `true`)
- `-pessimistic`: Use pessimistic transaction mode (default: `false`)
- `-long-conn`: Use long connection mode (default: `false`)

### Example Commands

**Basic usage:**
```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root
```

**High concurrency test:**
```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root -concurrency 500 -accounts 2000000
```

**Pessimistic transaction mode:**
```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root -pessimistic
```

**Long connection mode:**
```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root -long-conn
```

## How It Works

### Initialization Phase

1. **Database Connection**: Opens connection to the database
2. **TiDB Detection**: Checks if the database is TiDB by querying `tidb_version()`
3. **Transaction Mode Setup**: If TiDB and pessimistic mode is enabled, sets global transaction mode
4. **Table Creation**: Creates `accounts` table(s) and `record` table
5. **Account Population**: Inserts accounts in batches (100 accounts per batch) with:
   - Initial balance: 1000
   - Random remark strings
6. **Verification Start**: Begins periodic balance verification

### Execution Phase

1. **Concurrent Workers**: Spawns multiple goroutines (based on `concurrency` flag)
2. **Transfer Operations**: Each worker continuously:
   - Selects two random accounts (from ≠ to)
   - Selects a random table index
   - Transfers a random amount (0-999) from one account to another
   - Records the transaction in the `record` table
3. **Transaction Types**:
   - **Normal transactions**: Immediate read and commit
   - **Long transactions** (if enabled): Delays before read or commit (10 minutes ± 10 seconds)
4. **Balance Verification**: Periodically verifies that `SUM(balance) == num_accounts * 1000`

### Verification Logic

The tool maintains an invariant: **Total balance across all accounts must always equal `num_accounts * 1000`**

- Verification runs at the specified `interval` (default: 2 seconds)
- If verification fails, the tool sets a stop flag and logs an error
- After 6 hours of continuous verification failures, the tool exits with a fatal error

### Transaction Flow

Each transfer operation follows this pattern:

1. Begin transaction
2. SELECT accounts with `FOR UPDATE` (pessimistic locking)
3. Check if `from_balance >= amount`
4. If sufficient:
   - UPDATE both accounts' balances
   - INSERT transaction record
   - Get TSO (for TiDB) or timestamp
5. Commit transaction

## Connection Modes

### Short Connection Mode (Default)

- Uses shared connection pool (`*sql.DB`)
- Connections expire after 5 minutes
- Better resource utilization
- Suitable for most testing scenarios

### Long Connection Mode (`-long-conn`)

- Each goroutine maintains its own dedicated connection (`*sql.Conn`)
- Connections kept alive for up to 1 hour
- Useful for:
  - Testing connection-related features
  - Simulating persistent connections
  - Debugging connection pool behavior

## Transaction Modes

### Optimistic Mode (Default)

- Uses optimistic locking
- Suitable for most TiDB scenarios

### Pessimistic Mode (`-pessimistic`)

- Sets TiDB global transaction mode to pessimistic
- Uses `FOR UPDATE` for row locking
- Better for high-contention scenarios

## Database Schema

### `accounts` Table(s)

```sql
CREATE TABLE accounts[0-N] (
    id BIGINT PRIMARY KEY,
    balance BIGINT NOT NULL,
    remark VARCHAR(128)
)
```

- Multiple tables can be created (for sharding/testing) with suffixes `accounts`, `accounts1`, `accounts2`, etc.
- Each account starts with balance 1000

### `record` Table

```sql
CREATE TABLE record (
    id BIGINT AUTO_INCREMENT,
    from_id BIGINT NOT NULL,
    to_id BIGINT NOT NULL,
    from_balance BIGINT NOT NULL,
    to_balance BIGINT NOT NULL,
    amount BIGINT NOT NULL,
    tso BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY(id)
)
```

- Records all successful transfers
- Includes TSO (Timestamp Oracle) for TiDB or Unix timestamp for MySQL

## Error Handling

The tool includes robust error handling:

- **Retry Logic**: Operations retry up to `retry-limit` times with 5-second intervals
- **Duplicate Entry Handling**: Ignores duplicate key errors during initialization
- **Table Existence Checks**: Automatically recreates tables if data is inconsistent
- **Context Cancellation**: Properly handles graceful shutdown on SIGINT/SIGTERM

## Important Patterns

### Context Usage

- All operations respect `context.Context` for cancellation
- Graceful shutdown on interrupt signals (SIGHUP, SIGINT, SIGTERM, SIGQUIT)

### Concurrency Safety

- Uses `sync.WaitGroup` for goroutine coordination
- Atomic operations for stop flag (`atomic.LoadInt32`, `atomic.StoreInt32`)
- Read-write mutex for configuration access

### Performance Optimizations

- Pre-generated table index strings (avoiding repeated `fmt.Sprintf`)
- `strings.Builder` for efficient query construction
- Local random number generators (avoiding global `rand` lock contention)
- Batch inserts during initialization

## For AI Agents

### When to Use This Tool

- Testing database transaction consistency
- Load testing database systems
- Verifying ACID properties
- Testing concurrent transaction handling
- Benchmarking database performance
- Testing connection pool behavior

### Common Tasks

1. **Run a basic test:**
   ```bash
   ./bin/bank -addr <host>:<port> -db <database> -user <user> -pw <password>
   ```

2. **Test with high concurrency:**
   ```bash
   ./bin/bank -addr <host>:<port> -concurrency 1000 -accounts 5000000
   ```

3. **Test pessimistic transactions:**
   ```bash
   ./bin/bank -addr <host>:<port> -pessimistic
   ```

4. **Test long connections:**
   ```bash
   ./bin/bank -addr <host>:<port> -long-conn
   ```

### Expected Behavior

- Tool runs continuously until interrupted
- Logs transfer operations and verification results
- Exits with error if balance verification fails for 6+ hours
- Gracefully shuts down on SIGINT/SIGTERM

### Troubleshooting

- **Connection errors**: Check database address, credentials, and network connectivity
- **Verification failures**: Indicates data inconsistency - check database logs
- **High memory usage**: Reduce `concurrency` or `accounts` count
- **Slow performance**: Adjust `interval` or reduce `concurrency`

## Building

See `README.md` for build instructions. The tool can be built for:
- Current platform: `make build`
- Linux amd64: `make linux`
- Linux ARM64: `make linux-arm64`
- All Linux platforms: `make linux-all`

## Dependencies

- Go 1.13+
- MySQL driver: `github.com/go-sql-driver/mysql`
- TiDB parser: `github.com/pingcap/parser`
- Logging: `github.com/ngaut/log`
- Error handling: `github.com/juju/errors`

## Agent Roles and Workflows

This project includes role-specific prompts for efficient multi-agent collaboration:

### Available Roles

- **Developer** (`prompts/developer.md`): Implements features, writes code, and follows project patterns
- **Reviewer** (`prompts/reviewer.md`): Reviews code for correctness, quality, and adherence to standards
- **Tester** (`prompts/tester.md`): Writes unit and integration tests to ensure code correctness

### Recommended Workflow

**Standard Workflow:**
1. **Developer** implements changes using the developer prompt
2. **Reviewer** reviews changes using the reviewer prompt
3. **Developer** addresses feedback
4. **Reviewer** approves code
5. **Tester** writes tests using the tester prompt
6. **Reviewer** reviews tests (optional)
7. All tests pass → Complete

**Fast Workflow (for simple changes):**
1. **Developer** implements changes
2. **Reviewer** reviews and approves
3. **Tester** adds tests
4. Complete

All roles are optimized for:
- **Token efficiency**: Focused reviews, concise communication, targeted file reading, reusable test utilities
- **Code quality**: Systematic review process catches issues early
- **Test coverage**: Comprehensive testing ensures reliability
- **Fast iteration**: Clear roles and streamlined workflows

See `prompts/README.md` for detailed usage instructions.
