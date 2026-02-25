# Miri TypeScript SDK

This is the official TypeScript SDK for the Miri Autonomous Agent.

## Installation

```bash
npm install @miri/sdk
```

## Usage

```typescript
import { Api } from "@miri/sdk";

const api = new Api({
  baseURL: "http://localhost:8080",
  headers: {
    "X-Server-Key": "your-secret-key"
  }
});

// Example: Send a prompt
api.api.v1PromptCreate({
  prompt: "Hello, Miri!"
}).then(response => {
  console.log(response.data.response);
});
```

## Authentication
Miri uses two types of authentication:
1. **Server Key Authentication**: Standard API endpoints (`/api/v1/*`) require the `X-Server-Key` header.
2. **Basic Authentication**: Administrative endpoints (`/api/admin/v1/*`) require HTTP Basic Auth.

Default admin credentials:
- **Username**: `admin`
- **Password**: `admin-password`

### Example: Administrative Access
```typescript
import { Api } from "@miri/sdk";

const api = new Api({
  baseURL: "http://localhost:8080",
  // Standard auth
  headers: {
    "X-Server-Key": "your-secret-key"
  },
  // Admin auth (Basic)
  auth: {
    username: "admin",
    password: "admin-password"
  }
});

// Get configuration
api.api.adminV1ConfigList().then(response => {
  console.log(response.data);
});
```

## Features

- Full support for standard and administrative API endpoints.
- Axios-based HTTP client.
- Complete TypeScript type definitions.

## Publishing to NPM

To publish a new version of this SDK:

1. Update the version in `package.json`.
2. Login to NPM: `npm login`.
3. Publish: `npm publish --access public`.
