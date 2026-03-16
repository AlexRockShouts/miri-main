# Stage 1: Build TypeScript SDK
FROM node:20-alpine AS sdk-builder
WORKDIR /app
COPY miri-main/api/sdk/typescript ./api/sdk/typescript
WORKDIR /app/api/sdk/typescript
RUN npm install && npm run build

# Stage 2: Build Svelte Dashboard
FROM node:20-alpine AS dashboard-builder
WORKDIR /app
# Copy the built SDK
COPY --from=sdk-builder /app/api/sdk/typescript ./api/sdk/typescript
# Copy dashboard
COPY miri-dashboard ./dashboard
WORKDIR /app/dashboard
# Adjust the local SDK path in package.json to the one in the container
RUN sed -i 's|"@miri/sdk": "file:[^"]*"|"@miri/sdk": "file:../api/sdk/typescript"|' package.json
RUN npm install && npm run build

# Stage 3: Build Go Backend
FROM golang:1.24-alpine AS builder
WORKDIR /app
# Install build tools
RUN apk add --no-cache make git gcc musl-dev
# Copy go.mod and go.sum and download dependencies
COPY miri-main/go.mod miri-main/go.sum ./
RUN go mod download
# Copy the source code
COPY miri-main/. .
# Copy the built dashboard from the dashboard-builder to the correct location for embedding
COPY --from=dashboard-builder /app/dashboard/build ./src/cmd/server/dashboard
# Build the server using the Makefile
RUN make server

# Final image
FROM alpine:latest
WORKDIR /app
# Install common utilities
RUN apk add --no-cache ca-certificates libc6-compat
# Copy the binary from the builder
COPY --from=builder /app/bin/miri-server /app/miri-server
# Copy default templates and config if needed (adjust as necessary)
COPY miri-main/templates /app/templates
COPY miri-main/config.yaml /app/config.yaml
COPY miri-main/api/openapi.yaml /app/api/openapi.yaml
# Expose port (default for Gin is 8080)
EXPOSE 8080
# Command to run the server
ENTRYPOINT ["/app/miri-server"]
