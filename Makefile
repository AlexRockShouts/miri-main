.PHONY: all server tui clean test install

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/miri-server
TUI_BIN := $(BIN_DIR)/miri-tui

all: server tui
build: all

server:
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '-s -w' -o $(SERVER_BIN) ./src/cmd/server/main.go

tui:
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '-s -w' -o $(TUI_BIN) ./src/cmd/tui/main.go

clean:
	rm -rf $(BIN_DIR)

test:
	go test ./...

install: server tui
	cp $(SERVER_BIN) $(TUI_BIN) /usr/local/bin/

run-server: server
	./$(SERVER_BIN)

run-tui: tui
	./$(TUI_BIN)