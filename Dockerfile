
# Build dashboard
FROM node:24-alpine AS dashboard-builder
RUN apk add --no-cache git
RUN git clone https://github.com/AlexRockShouts/miri-dashboard.git /tmp/miri-dashboard
WORKDIR /tmp/miri-dashboard
RUN npm install
RUN npm run build

# Build Go Backend
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Copy built dashboard from node stage
COPY --from=dashboard-builder /tmp/miri-dashboard/build/* src/cmd/server/dashboard/

RUN ls -al src/cmd/server/dashboard/

# Build the server binary explicitly (matches GitHub release.yaml)
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o bin/miri-server ./src/cmd/server/main.go

# Final image
FROM alpine:latest
WORKDIR /app
# Install common utilities
RUN apk add --no-cache ca-certificates libc6-compat
#trivy patch
RUN apk add --no-cache zlib=1.3.2-r0
# Copy the binary from the builder
COPY --from=builder /app/bin/miri-server /app/miri-server
# Copy default templates and config from builder stage
COPY --from=builder /app/templates /app/templates
COPY --from=builder /app/config.yaml /app/config.yaml
# Expose port (default for Gin is 8080)
EXPOSE 8080
# Command to run the server
ENTRYPOINT ["/app/miri-server", "-config", "config.yaml"]
