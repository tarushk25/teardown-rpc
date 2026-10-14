An ephemeral JSON-RPC proxy for Ethereum. Ships as a tiny container, serves a fixed number of calls, then exits.

Build: `docker build -t teardown-rpc .`

Run: `docker run --rm -e UPSTREAM_RPC=https://... -e MAX_CALLS=50 -p 8545:8545 teardown-rpc`

Example calls:
```
curl -X POST localhost:8545 -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
curl localhost:8545/health
curl localhost:8545/budget
```

Use cases: CI, ephemeral devnets, time-boxed access, demos.
Dev mode (no upstream): `docker run --rm -e DEV=1 -e MAX_CALLS=5 -p 8545:8545 teardown-rpc`.
Config via env: `UPSTREAM_RPC` (required), `PORT`, `MAX_CALLS`, `GRACE_SECONDS`.
