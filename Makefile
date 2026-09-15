BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/calculator_server
GENERATOR_BIN := $(BIN_DIR)/generator

HOST ?= 0.0.0.0
PORT ?= 8080
C_LIB ?= ./libcalculator.so
RUST_LIB ?= ./libcalculator_rust.so

.PHONY: help libs build run server generator clean

help:
	@echo "Targets:"
	@echo "  make libs       build native shared libraries (build.sh)"
	@echo "  make build      compile server and generator binaries"
	@echo "  make server     run the calculator server"
	@echo "  make generator  run the load generator against the server"
	@echo "  make clean      remove build artifacts"
	@echo ""
	@echo "Variables:"
	@echo "  HOST=$(HOST) PORT=$(PORT)"
	@echo "  C_LIB=$(C_LIB) RUST_LIB=$(RUST_LIB)"

libs:
	bash ./build.sh

build: $(SERVER_BIN) $(GENERATOR_BIN)

$(BIN_DIR):
	mkdir -p $@

$(SERVER_BIN): | $(BIN_DIR)
	CGO_ENABLED=1 go build -o $@ ./calculator_server

$(GENERATOR_BIN): | $(BIN_DIR)
	go build -o $@ ./generator

server: $(SERVER_BIN)
	./$(SERVER_BIN) --host $(HOST) --port $(PORT) --c-lib $(C_LIB) --rust-lib $(RUST_LIB)

generator: $(GENERATOR_BIN)
	./$(GENERATOR_BIN) --url http://localhost:$(PORT)/calc

clean:
	rm -rf $(BIN_DIR)