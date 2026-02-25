package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"syscall/js"
)

// MiriClient configuration
type MiriClient struct {
	BaseURL   string
	ServerKey string
	AdminUser string
	AdminPass string
}

func (c *MiriClient) Prompt(prompt string, sessionID string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/prompt", c.BaseURL)
	reqBody, _ := json.Marshal(map[string]string{
		"prompt":     prompt,
		"session_id": sessionID,
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.ServerKey != "" {
		req.Header.Set("X-Server-Key", c.ServerKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}

	return res.Response, nil
}

func (c *MiriClient) GetConfig() (string, error) {
	url := fmt.Sprintf("%s/api/admin/v1/config", c.BaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	if c.AdminUser != "" && c.AdminPass != "" {
		req.SetBasicAuth(c.AdminUser, c.AdminPass)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func main() {
	c := make(chan struct{}, 0)

	js.Global().Set("miriPrompt", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return "Error: missing arguments (baseUrl, serverKey, prompt, [sessionId])"
		}

		client := &MiriClient{
			BaseURL:   args[0].String(),
			ServerKey: args[1].String(),
		}
		prompt := args[2].String()
		sessionID := ""
		if len(args) > 3 {
			sessionID = args[3].String()
		}

		handler := js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]

			go func() {
				res, err := client.Prompt(prompt, sessionID)
				if err != nil {
					reject.Invoke(err.Error())
				} else {
					resolve.Invoke(res)
				}
			}()

			return nil
		})

		promise := js.Global().Get("Promise")
		return promise.New(handler)
	}))

	js.Global().Set("miriGetConfig", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return "Error: missing arguments (baseUrl, adminUser, adminPass)"
		}

		client := &MiriClient{
			BaseURL:   args[0].String(),
			AdminUser: args[1].String(),
			AdminPass: args[2].String(),
		}

		handler := js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]

			go func() {
				res, err := client.GetConfig()
				if err != nil {
					reject.Invoke(err.Error())
				} else {
					resolve.Invoke(res)
				}
			}()

			return nil
		})

		promise := js.Global().Get("Promise")
		return promise.New(handler)
	}))

	fmt.Println("Miri WASM SDK initialized")
	<-c
}
