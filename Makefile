.PHONY: all server clean test install

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/miri-server

all: server
build: all

server:
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '-s -w' -o $(SERVER_BIN) ./src/cmd/server/main.go

clean:
	rm -rf $(BIN_DIR)

test:
	go test ./...

install: server
	cp $(SERVER_BIN) /usr/local/bin/

run-server: server
	./$(SERVER_BIN)