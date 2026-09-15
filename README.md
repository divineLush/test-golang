# calculator

A load-testing playground: a high-throughput HTTP server backed by native C and
Rust shared libraries, plus a concurrent load generator. Written in Go with
cgo (shared libraries are loaded at runtime via `dlopen`/`dlsym`).

## Prerequisites

- Go 1.22+
- A C compiler (`gcc`)
- Rust toolchain (`cargo`) — only needed to build the Rust library

## Quick start

```sh
make libs       # build libcalculator.so (C) + libcalculator_rust.so (Rust)
make build      # compile server + generator binaries into bin/
make test       # run unit tests with the race detector
make server     # run the server        (terminal 1)
make generator  # run the load generator (terminal 2)
```

`make clean` removes the `bin/` binaries.

## Make commands

| Target       | Description                                                     |
| ------------ | --------------------------------------------------------------- |
| `make libs`  | Build the native shared libraries into the project root (`.so`) |
| `make build` | Compile `bin/calculator_server` and `bin/generator`             |
| `make test`  | Run unit tests with the race detector (`go test -race -count=1 ./...`) |
| `make vet`   | Static analysis with `go vet`                                   |
| `make server`    | Run the calculator server                                       |
| `make generator` | Run the load generator against the server                       |
| `make prometheus`     | Run Prometheus (Docker) scaping the server at `localhost:9090`  |
| `make prometheus-stop` | Stop the Prometheus container                                   |
| `make clean`     | Remove the compiled binaries                                    |
| `make help`      | Print targets and variables                                     |

### Make variables

| Variable    | Default                  | Description                              |
| ----------- | ------------------------ | ---------------------------------------- |
| `HOST`      | `0.0.0.0`                | Bind address of the server               |
| `PORT`      | `8080`                   | Port of the server                       |
| `C_LIB`     | `./libcalculator.so`     | Path to the C shared library             |
| `RUST_LIB`  | `./libcalculator_rust.so`| Path to the Rust shared library          |

Examples:

```sh
make server PORT=9090 HOST=127.0.0.1
make generator PORT=9090
```

### Running the binaries directly

```sh
bin/calculator_server [--host 0.0.0.0] [--port 8080] \
  [--c-lib ./libcalculator.so] [--rust-lib ./libcalculator_rust.so] \
  [--interval 5s]

bin/generator --url http://localhost:8080/calc [-n 10] \
  [--interval 0.1] [--timeout 5.0]
```

Generator flags:

- `--url` — calculator endpoint (default `http://localhost:8080/calc`)
- `-n`, `--threads` — number of worker goroutines (default `10`)
- `--interval` — seconds between requests per worker, `0` = as fast as
  possible (default `0.1`)
- `--timeout` — HTTP request timeout in seconds (default `5.0`)

## HTTP endpoints

### `POST /calc?num=<int>`

Applies `num` as both an addition (via the C library `add`) and a subtraction
(via the Rust library `sub`) to running totals.

| Response | Condition                              |
| -------- | -------------------------------------- |
| `200 ok` | `num` is a valid integer               |
| `400`    | `num` missing or not an integer        |
| `404`    | unknown path                           |
| `501`    | method other than `POST`               |

The server also prints the running totals every `--interval` and once more on
shutdown:

```
[periodic] sum=123 sub=-123
[final]    sum=123 sub=-123
```

### `GET /metrics`

Prometheus text format (scraped by the standard `/metrics` handler):

| Metric                                   | Type    | Meaning                                   |
| ---------------------------------------- | ------- | ----------------------------------------- |
| `calculator_requests_total`              | counter | Successful `/calc` requests handled       |
| `calculator_requests_per_second{sec}`    | gauge   | Request count per unix second, last 60s   |
| `calculator_c_duration_seconds`          | summary | C `add` call latency (p95, p99, sum, count) |
| `calculator_rust_duration_seconds`       | summary | Rust `sub` call latency (p95, p99, sum, count) |

Latency percentiles are computed over calls from the last 60 seconds; `_sum` and
`_count` are cumulative over the process lifetime. Units are seconds.

## Viewing metrics in Prometheus (Docker)

With the server running (`make server`), scrape it from a real Prometheus:

```sh
make prometheus        # runs Prometheus in a docker container (localhost:9090)
make prometheus-stop   # stop that container
```

Open `http://localhost:9090` and try:

```
calculator_c_duration_seconds{quantile="0.99"}
calculator_rust_duration_seconds{quantile="0.95"}
rate(calculator_requests_total[1m])
rate(calculator_requests_per_second[1m])
```

Notes:

- The container uses `--network=host`, so Prometheus reaches the server on
  `localhost:<PORT>` (default `8080`) — use `make prometheus PORT=9090` if you
  ran the server on a different port.
- Scrape config comes from `prometheus.yml.tmpl` (generated into `.prometheus/`,
  gitignored); server on macOS/Windows may need `host.docker.internal` instead
  of `localhost` in `targets`.

## Project layout

```
calculator_server/   server entrypoint (wiring + lifecycle)
generator/           load generator entrypoint
internal/native/     cgo: dlopen/dlsym of the shared libraries
internal/server/     HTTP handler state + /calc + /metrics
internal/metrics/    Prometheus metrics (request rate, latency percentiles)
c_lib/               C library source (add)
rust_lib/            Rust crate source (sub)
build.sh             builds the native shared libraries
prometheus.yml.tmpl  Prometheus scrape-config template (docker)
```