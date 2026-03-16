
# Build dashboard
FROM node:24-alpine AS dashboard-builder
RUN apk add --no-cache git make wget
RUN git clone https://github.com/AlexRockShouts/miri-dashboard.git /tmp/miri-dashboard
RUN cd /tmp/miri-dashboard
WORKDIR /tmp/miri-dashboard
RUN npm install
run npm run build

# Build Go Backend
FROM golang:1.25-alpine AS builder
WORKDIR /app
# Install build tools
RUN apk add --no-cache make git gcc musl-dev
# Copy the source code
COPY . .

# Copy built dashboard from node stage
COPY --from=dashboard-builder /tmp/miri-dashboard/build/* src/cmd/server/dashboard/


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
COPY templates /app/templates
COPY config.yaml /app/config.yaml
# Expose port (default for Gin is 8080)
EXPOSE 8080
# Command to run the server
ENTRYPOINT ["/app/miri-server"]
