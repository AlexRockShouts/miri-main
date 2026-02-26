#!/bin/bash

# Start the agent in the background
bin/miri-server -config config.yaml &
PID=$!
sleep 2

echo "Testing GET /config..."
curl -s -u admin:admin-password http://localhost:8080/api/admin/v1/config
echo -e "\n"

echo "Testing POST /human..."
curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/human -H "Content-Type: application/json" -d '{
  "id": "user123",
  "data": {"name": "Alice", "preference": "coffee"},
  "notes": "Loves dark roast"
}'
echo -e "\n"

echo "Testing GET /human..."
curl -s -u admin:admin-password http://localhost:8080/api/admin/v1/human
echo -e "\n"

echo "Testing POST /config (updating config)..."
curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/config -H "Content-Type: application/json" -d '{
  "models": {
    "providers": {
      "xai": {"apiKey": "test_key", "baseUrl": "https://api.x.ai/v1", "api": "openai"}
    }
  },
  "server": {
    "addr": ":8080",
    "key": "local-dev-key",
    "admin_user": "admin",
    "admin_pass": "admin-password"
  },
  "storage_dir": "'$HOME'/.miri"
}'
echo -e "\n"

echo "Checking if config file was created in $HOME/.miri/config.yaml..."
ls -l $HOME/.miri/config.yaml

echo "Testing POST /prompt..."
curl -s -X POST http://localhost:8080/api/v1/prompt -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"prompt": "test"}' | head -c 200
echo -e "\n"

echo "Testing POST /prompt with session_id..."
curl -s -X POST http://localhost:8080/api/v1/prompt -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"prompt": "test session", "session_id": "mysession"}' | head -c 200
echo -e "\n"

echo "Testing GET /sessions..."
curl -s -u admin:admin-password http://localhost:8080/api/admin/v1/sessions
echo -e "\n"

echo "Testing GET /sessions/mysession..."
curl -s -u admin:admin-password http://localhost:8080/api/admin/v1/sessions/mysession
echo -e "\n"

echo "Testing POST /interaction new..."
curl -s -X POST http://localhost:8080/api/v1/interaction -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"action":"new","client_id":"testclient"}'
echo -e "\n"

echo "Testing POST /interaction status..."
curl -s -X POST http://localhost:8080/api/v1/interaction -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"action":"status"}'
echo -e "\n"

echo "Testing POST /channels whatsapp actions (expect channel not found or 400)..."
 curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/channels -H "Content-Type: application/json" -d '{"channel":"whatsapp","action":"status"}'
 echo -e "\n"
 curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/channels -H "Content-Type: application/json" -d '{"channel":"whatsapp","action":"enroll"}'
 echo -e "\n"
 curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/channels -H "Content-Type: application/json" -d '{"channel":"whatsapp","action":"send","device":"test@s.whatsapp.net","message":"test"}'
 echo -e "\n"
 curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/channels -H "Content-Type: application/json" -d '{"channel":"whatsapp","action":"devices"}'
 echo -e "\n"
 curl -s -X POST -u admin:admin-password http://localhost:8080/api/admin/v1/channels -H "Content-Type: application/json" -d '{"channel":"whatsapp","action":"chat","device":"test","prompt":"test"}'
 echo -e "\n"

# Kill the agent
 kill $PID


curl -s -X POST http://localhost:8080/api/v1/prompt -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"prompt": "im developing this is a test prompt: can you install grype"}' | head -c 200
