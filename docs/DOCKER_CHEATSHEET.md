# Oenexa Docker Commands Cheat Sheet

This guide contains all the essential Docker commands specifically tailored for running and managing the Oenexa Node and Frontend in your local environment.

## 🚀 1. Starting the Project

### Start the standard stack (Node + Frontend)
This is the most common command you will use. It builds the latest local code and starts the backend and frontend in the background.
```bash
docker compose up --build -d
```

### Start with Monitoring (Node + Frontend + Prometheus + Grafana)
If you need to analyze performance, this command includes the monitoring tools.
```bash
docker compose --profile monitoring up --build -d
```

---

## 🔍 2. Viewing Logs

### View Live Node Logs
Use this to see blockchain sync status, new blocks, and P2P connections in real-time.
```bash
docker logs -f oenexa-node
```

### View Frontend Logs
```bash
docker logs -f oenexa-frontend
```

*(Press `Ctrl + C` to stop watching the logs. This will not stop the containers).*

---

## 🛑 3. Stopping and Cleaning Up

### Stop the Network gracefully
This stops all containers but **preserves** your blockchain data.
```bash
docker compose down
```

### ⚠️ Stop and WIPE all Blockchain Data
If you need to start from a completely clean slate (Genesis block), this command stops the containers and **deletes the `ONXC-data` persistent volume**.
```bash
docker compose down -v
```

### Fix "Container Name Conflict" Errors
If you ever see a conflict error because an old ghost container is stuck, force remove it:
```bash
docker rm -f oenexa-node oenexa-frontend
```

---

## 🛠️ 4. Advanced Management

### Enter the Running Node Container
If you need to explore the files inside the running backend container:
```bash
docker exec -it oenexa-node /bin/sh
```

### View all running Oenexa containers
```bash
docker ps --filter "name=ONXC"
```

---

## 🔗 Port Mappings Reference
* **Frontend Dashboard**: `http://localhost:8080`
* **JSON-RPC API**: `http://localhost:8545`
* **Metrics (Prometheus format)**: `http://localhost:9090/metrics`
* **Grafana Dashboard (if enabled)**: `http://localhost:3000`
* **P2P Network**: `8765` (TCP)
