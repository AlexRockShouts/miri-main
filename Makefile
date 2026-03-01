.PHONY: all server clean test install ts-sdk ts-sdk-generate ts-sdk-install ts-sdk-build ts-sdk-publish run-server build

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/miri-server

# TypeScript SDK paths and settings
TS_SDK_DIR := api/sdk/typescript
OPENAPI_SPEC := api/openapi.yaml

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


# --- TypeScript SDK tasks ---
# Generate the TypeScript SDK from the OpenAPI spec into a dedicated "generated" folder
# Requires Node.js. If Java is installed, the generator will use it; otherwise it uses the embedded runner.
ts-sdk-generate:
	npx --yes @openapitools/openapi-generator-cli generate \
		-i $(OPENAPI_SPEC) \
		-g typescript-axios \
		-o $(TS_SDK_DIR)/generated \
		--skip-validate-spec || true

# Install npm dependencies for the TypeScript SDK
# Use CI-friendly install to get exact lockfile versions
ts-sdk-install:
	cd $(TS_SDK_DIR) && npm ci || npm install

# Build/compile the TypeScript SDK (outputs to ./dist per package.json)
ts-sdk-build:
	cd $(TS_SDK_DIR) && npm run build

# Publish the TypeScript SDK to npm. Provide NPM_TOKEN for auth when running in CI.
# Example: make ts-sdk-publish NPM_TAG=next
NPM_TAG ?=

ts-sdk-publish: ts-sdk-build
	@if [ -n "$$NPM_TOKEN" ]; then \
		echo "Publishing using provided NPM_TOKEN..."; \
		TOKEN_FILE=$$(mktemp); \
		printf "//registry.npmjs.org/:_authToken=%s\n" "$$NPM_TOKEN" > "$$TOKEN_FILE"; \
		printf "access=public\n" >> "$$TOKEN_FILE"; \
		printf "always-auth=true\n" >> "$$TOKEN_FILE"; \
		cd $(TS_SDK_DIR) && NPM_CONFIG_USERCONFIG="$$TOKEN_FILE" npm publish $(if $(NPM_TAG),--tag $(NPM_TAG),); \
		STATUS=$$?; \
		rm -f "$$TOKEN_FILE"; \
		exit $$STATUS; \
	else \
		echo "No NPM_TOKEN provided. Ensure you are logged in via 'npm login'."; \
		cd $(TS_SDK_DIR) && npm publish --access public $(if $(NPM_TAG),--tag $(NPM_TAG),); \
	fi

# Convenience meta-targets
# Generate + install + build
ts-sdk: ts-sdk-generate ts-sdk-install ts-sdk-build

# Full release: generate, install, build, and publish
ts-sdk-release: ts-sdk ts-sdk-publish
