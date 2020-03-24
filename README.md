# bank
Simulate the transfer business. You can uses it to test [TiDB](https://github.com/pingcap/tidb) or MySQL.

## Build

```bash
# build binary 
make build
```

The above command will compile a binary named `bank` and place it in the directory

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

example: 

```bash
./bin/bank -addr 127.0.0.1:4000 -db test -user root 
```