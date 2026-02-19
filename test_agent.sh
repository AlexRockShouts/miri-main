#!/bin/bash

# Start the agent in the background
./miri &
PID=$!
sleep 2

echo "Testing GET /config..."
curl -s -H "X-Server-Key: local-dev-key" http://localhost:8080/config
echo -e "\n"

echo "Testing POST /human..."
curl -s -X POST http://localhost:8080/human -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{
  "id": "user123",
  "data": {"name": "Alice", "preference": "coffee"},
  "notes": "Loves dark roast"
}'
echo -e "\n"

echo "Testing GET /human..."
curl -s -H "X-Server-Key: local-dev-key" http://localhost:8080/human
echo -e "\n"

echo "Testing POST /config (updating config)..."
curl -s -X POST http://localhost:8080/config -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{
  "xai": {"api_key": "test_key", "model": "grok-beta"},
  "storage_dir": "'$HOME'/.miri"
}'
echo -e "\n"

echo "Checking if config file was created in $HOME/.miri/config.yaml..."
ls -l $HOME/.miri/config.yaml

echo "Testing POST /prompt..."
curl -s -X POST http://localhost:8080/prompt -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"prompt": "test"}' | head -c 200
echo -e "\n"

echo "Testing POST /prompt with session_id..."
curl -s -X POST http://localhost:8080/prompt -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"prompt": "test session", "session_id": "mysession"}' | head -c 200
echo -e "\n"

echo "Testing POST /interaction new..."
curl -s -X POST http://localhost:8080/interaction -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"action":"new","client_id":"testclient"}'
echo -e "\n"

echo "Testing POST /interaction status..."
 curl -s -X POST http://localhost:8080/interaction -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"action":"status"}'
 echo -e "\n"

echo "Testing POST /channels whatsapp actions (expect channel not found or 400)..."
 curl -s -X POST http://localhost:8080/channels -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"channel":"whatsapp","action":"status"}'
 echo -e "\n"
 curl -s -X POST http://localhost:8080/channels -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"channel":"whatsapp","action":"enroll"}'
 echo -e "\n"
 curl -s -X POST http://localhost:8080/channels -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"channel":"whatsapp","action":"send","device":"test@s.whatsapp.net","message":"test"}'
 echo -e "\n"
 curl -s -X POST http://localhost:8080/channels -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"channel":"whatsapp","action":"devices"}'
 echo -e "\n"
 curl -s -X POST http://localhost:8080/channels -H "Content-Type: application/json" -H "X-Server-Key: local-dev-key" -d '{"channel":"whatsapp","action":"chat","device":"test","prompt":"test"}'
 echo -e "\n"

# Kill the agent
 kill $PID
