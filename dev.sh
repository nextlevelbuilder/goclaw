#!/bin/bash
# Dev mode with hot reload for both backend and frontend
# Frontend proxies API requests to backend

set -e

echo "Starting GoClaw in DEV mode..."
echo ""

# Check if we're in the right directory
if [ ! -f "docker-compose.yml" ]; then
    echo "Error: Run this script from goclaw root directory"
    exit 1
fi

# Function to cleanup on exit
cleanup() {
    echo ""
    echo "Shutting down..."
    kill $BACKEND_PID $FRONTEND_PID 2>/dev/null || true
    wait
}
trap cleanup EXIT

# Start backend (Go) in the background
echo "[1/2] Starting backend (Go)..."
go run . server &
BACKEND_PID=$!

# Wait a bit for backend to start
sleep 2

# Start frontend (Vite dev server) in the background
echo "[2/2] Starting frontend (Vite dev server)..."
cd ui/web
pnpm dev &
FRONTEND_PID=$!

echo ""
echo "=========================================="
echo "DEV servers running!"
echo "Frontend: http://localhost:5173"
echo "Backend:  http://localhost:9600"
echo ""
echo "Frontend proxies /v1, /ws, /health to backend"
echo "Press Ctrl+C to stop both"
echo "=========================================="

# Wait for both processes
wait
