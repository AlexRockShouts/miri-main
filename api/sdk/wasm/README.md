# Miri WASM SDK

This directory contains the Miri SDK compiled to WebAssembly (WASM), allowing you to interact with the Miri agent directly from a web browser.

## Files

- `miri.wasm`: The compiled Go SDK.
- `wasm_exec.js`: The Go WASM bridge (from the Go distribution).
- `index.html`: Example usage.

## Usage

1. Include `wasm_exec.js` in your HTML.
2. Load and run `miri.wasm`.
3. Use the global functions:
    - `miriPrompt(baseUrl, serverKey, prompt, [sessionId])`: Returns a Promise with the agent's response.
    - `miriGetConfig(baseUrl, adminUser, adminPass)`: Returns a Promise with the server's JSON configuration.

### Example

```javascript
const go = new Go();
WebAssembly.instantiateStreaming(fetch("miri.wasm"), go.importObject).then((result) => {
    go.run(result.instance);
    
    // Standard prompt
    miriPrompt("http://localhost:8080", "local-dev-key", "Hello Miri!")
        .then(response => console.log("Miri says:", response))
        .catch(error => console.error("Error:", error));

    // Get config (Admin)
    miriGetConfig("http://localhost:8080", "admin", "admin-password")
        .then(config => console.log("Config:", JSON.parse(config)))
        .catch(error => console.error("Admin Error:", error));
});
```
