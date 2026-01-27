# bank
Simulate the transfer business. You can uses it to test [TiDB](https://github.com/pingcap/tidb) or MySQL.

## Build

### Local Build

```bash
# build binary for current platform
make build
```

The above command will compile a binary named `bank` and place it in the `bin/` directory.

### Cross-Compilation for Linux

The Makefile supports cross-compilation for various Linux architectures:

```bash
# Build for Linux amd64 (x86_64)
make linux

# Build for Linux ARM64
make linux-arm64

# Build for Linux ARM (32-bit)
# Note: ARM 32-bit compilation may fail due to dependency compatibility issues
make linux-arm

# Build for all Linux architectures (amd64 and ARM64)
make linux-all
```

The compiled binaries will be placed in the `bin/` directory:
- `bin/bank-linux` - Linux amd64 (x86_64)
- `bin/bank-linux-arm64` - Linux ARM64
- `bin/bank-linux-arm` - Linux ARM (32-bit, may have dependency compatibility issues)

**Note**: 
- ARM64 compilation is fully supported and recommended for ARM-based systems
- ARM 32-bit compilation may encounter issues with some dependencies due to integer overflow in dependency packages
- The `linux-all` target builds amd64 and ARM64 only

## Usage

```bash
Usage of ./bin/bank:
  -accounts int
        the number of accounts (default 1000000)
  -addr string
        the address of db
  -concurrency int
        concurrency worker count (default 200)
  -db string
        database name (default "test")
  -interval duration
        the interval (default 2s)
  -long-conn
        use long connection mode (each goroutine maintains its own connection)
  -long-txn
        enable long-term transactions (default true)
  -pessimistic
        use pessimistic transaction
  -pw string
        database password
  -retry-limit int
        retry count (default 200)
  -tables int
        the number of the tables (default 1)
  -user string
        database user (default "root")
```

### Connection Modes

The tool supports two connection modes:

- **Short Connection Mode (default)**: Uses a connection pool where connections are shared among goroutines. Connections expire after 5 minutes to ensure freshness. This mode is suitable for most scenarios and provides better resource utilization.

- **Long Connection Mode (`-long-conn`)**: Each goroutine maintains its own dedicated database connection throughout its lifetime. Connections are kept alive for up to 1 hour. This mode is useful for:
  - Testing connection-related features
  - Simulating scenarios where each worker maintains a persistent connection
  - Debugging connection pool behavior

### Examples

Basic usage with short connection mode (default):

```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root 
```

Using long connection mode:

```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root -long-conn
```

With custom concurrency and accounts:

```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root -concurrency 500 -accounts 2000000
```