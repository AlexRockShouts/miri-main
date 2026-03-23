.PHONY: all server clean test install ts-sdk ts-sdk-generate ts-sdk-install ts-sdk-build ts-sdk-publish run-server build dashboard-build

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/miri-server

# TypeScript SDK paths and settings
TS_SDK_DIR := api/sdk/typescript
OPENAPI_SPEC := api/openapi.yaml

all: server
build: all

server: dashboard-build
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '-s -w' -o $(SERVER_BIN) ./src/cmd/server/main.go

clean:
	rm -rf $(BIN_DIR)

test:
	go test -race -bench=. ./...

install: server
	cp $(SERVER_BIN) /usr/local/bin/

run-server: server
	./$(SERVER_BIN)


# --- Dashboard tasks ---
# Build the dashboard and copy it to the location for embedding
DASHBOARD_SRC_DIR := ../miri-dashboard
DASHBOARD_EMBED_DIR := src/cmd/server/dashboard

dashboard-build:
	@mkdir -p $(DASHBOARD_EMBED_DIR)
	@if [ -d "$(DASHBOARD_SRC_DIR)" ]; then \
		echo "Building dashboard from $(DASHBOARD_SRC_DIR)..."; \
		cd $(DASHBOARD_SRC_DIR) && npm install && npm run build; \
		cp -r $(DASHBOARD_SRC_DIR)/build/* $(DASHBOARD_EMBED_DIR)/; \
	else \
		echo "Dashboard source not found at $(DASHBOARD_SRC_DIR). Skipping embed. Ensuring directory exists for build."; \
		if [ ! -f "$(DASHBOARD_EMBED_DIR)/.gitkeep" ] && [ -z "$$(ls -A $(DASHBOARD_EMBED_DIR))" ]; then \
			touch $(DASHBOARD_EMBED_DIR)/.gitkeep; \
		fi \
	fi

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

# --- Local Development Linking ---
.PHONY: ts-sdk-build-link ts-sdk-unlink dashboard-link dashboard-unlink

# Build SDK and create global npm link
ts-sdk-build-link:
	cd $(TS_SDK_DIR) && npm run build && npm link

# Link dashboard to local SDK (assumes ../miri-dashboard)
dashboard-link:
	@if [ -d "../miri-dashboard" ]; then \
		cd ../miri-dashboard && \
		npm link @alexrockshouts/miri-sdk && \
		npm install; \
		echo "✅ Dashboard linked to local SDK!"; \
	else \
		echo "❌ Clone https://github.com/AlexRockShouts/miri-dashboard to ../miri-dashboard first."; \
		exit 1; \
	fi

# Unlink dashboard from local SDK
dashboard-unlink:
	@if [ -d "../miri-dashboard" ]; then \
		cd ../miri-dashboard && \
		npm unlink @alexrockshouts/miri-sdk && \
		npm install; \
		echo "✅ Dashboard unlinked (uses npm version)."; \
	else \
		echo "No ../miri-dashboard found."; \
	fi

# Remove global SDK link
ts-sdk-unlink:
	cd $(TS_SDK_DIR) && npm unlink

shadow:
	mkdir -p tmp
	go build -trimpath -ldflags '-s -w' -o tmp/miri-server-shadow ./src/cmd/server/main.go

deploy:
	make test && cp bin/miri-server bin/miri-server.bak && mv tmp/miri-server-shadow bin/miri-server

.PHONY += shadow deploy