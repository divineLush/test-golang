BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/calculator_server
GENERATOR_BIN := $(BIN_DIR)/generator

HOST ?= 0.0.0.0
PORT ?= 8080
C_LIB ?= ./libcalculator.so
RUST_LIB ?= ./libcalculator_rust.so

PROMETHEUS_IMAGE ?= prom/prometheus
PROMETHEUS_YML := .prometheus/prometheus.yml
PROMETHEUS_DATA := .prometheus/data

.PHONY: help libs build test vet server generator clean prometheus prometheus-stop

help:
	@echo "Targets:"
	@echo "  make libs       build native shared libraries (build.sh)"
	@echo "  make build      compile server and generator binaries"
	@echo "  make test       run unit tests with the race detector"
	@echo "  make vet        run go vet static analysis"
	@echo "  make server     run the calculator server"
	@echo "  make generator  run the load generator against the server"
	@echo "  make prometheus     run Prometheus (docker) to scrape the server"
	@echo "  make prometheus-stop stop the Prometheus container"
	@echo "  make clean      remove build artifacts"
	@echo ""
	@echo "Variables:"
	@echo "  HOST=$(HOST) PORT=$(PORT)"
	@echo "  C_LIB=$(C_LIB) RUST_LIB=$(RUST_LIB)"

libs:
	bash ./build.sh

test:
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

SERVER_SRC := $(wildcard calculator_server/*.go internal/server/*.go internal/native/*.go internal/metrics/*.go)
GENERATOR_SRC := $(wildcard generator/*.go)

build: $(SERVER_BIN) $(GENERATOR_BIN)

$(BIN_DIR):
	mkdir -p $@

$(SERVER_BIN): $(SERVER_SRC) | $(BIN_DIR)
	CGO_ENABLED=1 go build -o $@ ./calculator_server

$(GENERATOR_BIN): $(GENERATOR_SRC) | $(BIN_DIR)
	go build -o $@ ./generator

server: $(SERVER_BIN)
	./$(SERVER_BIN) --host $(HOST) --port $(PORT) --c-lib $(C_LIB) --rust-lib $(RUST_LIB)

generator: $(GENERATOR_BIN)
	./$(GENERATOR_BIN) --url http://localhost:$(PORT)/calc

prometheus: $(PROMETHEUS_YML)
	docker run --rm --network=host \
		--user "$$(id -u):$$(id -g)" \
		-v "$(CURDIR)/$(PROMETHEUS_YML):/etc/prometheus/prometheus.yml" \
		-v "$(CURDIR)/$(PROMETHEUS_DATA):/prometheus" \
		--name calculator-prometheus \
		$(PROMETHEUS_IMAGE) \
		--config.file=/etc/prometheus/prometheus.yml

$(PROMETHEUS_YML): prometheus.yml.tmpl
	mkdir -p $(PROMETHEUS_DATA)
	sed 's/__PORT__/$(PORT)/' prometheus.yml.tmpl > $@

prometheus-stop:
	-docker rm -f calculator-prometheus

clean:
	rm -rf $(BIN_DIR)