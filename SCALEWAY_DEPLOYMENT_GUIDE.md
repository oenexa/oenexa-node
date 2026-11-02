# 🏛️ OENEXA (OEN) — Scaleway Cloud Deployment Guide
### *Cloud-init VERSION 22.1+ Automation for Scaleway Elastic Metal & Instances*

This guide details the complete deployment process for running the **OENEXA Layer-1 Quantum-Safe Blockchain & Explorer** on Scaleway bare-metal or cloud infrastructure using automated Cloud-init.

---

## 📋 Table of Contents
1. [Overview & Architecture](#1-overview--architecture)
2. [Method A: Deploy via Scaleway Console (Web UI)](#2-method-a-deploy-via-scaleway-console-web-ui)
3. [Method B: Deploy via Scaleway CLI (`scw`)](#3-method-b-deploy-via-scaleway-cli-scw)
4. [Method C: Deploy via Terraform](#4-method-c-deploy-via-terraform)
5. [Firewall & Network Security Architecture](#5-firewall--network-security-architecture)
6. [Post-Boot Verification & `oenexa-ctl` Cheatsheet](#6-post-boot-verification--oenexa-ctl-cheatsheet)
7. [Keystore Backup & Security Checklist](#7-keystore-backup--security-checklist)

---

## 1. Overview & Architecture

When deployed with `scaleway-cloud-init.yaml`, the server configures:

```
[ Internet / Peers ]
         │
         ├─── Port 8765 (TCP) ───► [ OENEXA L1 Node ] (P2P GossipSub & ML-KEM Handshake)
         │
         └─── Port 80 / 443 ─────► [ OENEXA Explorer / Nginx ]
                                        │
                                        ├─── /       ───► React 18 Web Explorer Dashboard
                                        └─── /rpc    ───► Proxied to JSON-RPC Engine (:8545)
                                        └─── POST /  ───► Transparent 405 RPC Web3 Fallback
```

* **No direct public exposure** of sensitive JSON-RPC (:8545) or metrics (:9090).
* **NIST PQC Security**: Hardened system limits disable core dumps (`* hard core 0`) to prevent memory extraction of private signing keys.
* **Automatic Wallet & Keystore**: Uses Argon2id + AES-256-GCM encryption with an auto-generated or user-supplied password.

---

## 2. Method A: Deploy via Scaleway Console (Web UI)

This is the fastest method if you are creating a server through [console.scaleway.com](https://console.scaleway.com).

### Step 1: Create Server
1. Log in to the Scaleway Console.
2. Navigate to **Elastic Metal** (for dedicated bare-metal servers) or **Instances** (for cloud VMs).
3. Click **Create Elastic Metal server** (or **Create an Instance**).

### Step 2: Select Server Type & OS
1. Choose your plan:
   - **Testnet / Devnet:** e.g., `DEV1-M` or `PLAY2-NANO` / `EM-A210R-HDD`.
   - **Mainnet Validator:** e.g., `PRO2-M` or `EM-B1110X-SATA` / `EM-T110X-NVME` (4+ vCPUs, 8GB+ RAM, NVMe).
2. Choose OS Image: **Ubuntu 22.04 LTS** or **Ubuntu 24.04 LTS** (or Debian 12).

### Step 3: Paste Cloud-init Script
1. Scroll down to **Advanced Options**.
2. Locate the text area labeled:
   > **Cloud-init configuration \***
3. Open [`scaleway-cloud-init.yaml`](file:///c:/workspace/repo/oenexa-node/scaleway-cloud-init.yaml) from this repository.
4. Copy the entire file contents and paste them directly into the **Cloud-init configuration \*** field.
5. *(Optional)* Modify any variables at lines 73–95 in the embedded `/opt/oenexa/.env` block:
   - `OEN_PASSWORD`: Custom keystore password (or leave blank to auto-generate a 48-char random secret).
   - `NETWORK_MODE`: `standalone-miner` (produces blocks immediately) or `mainnet`.
   - `GITHUB_TOKEN`: Add a token if your repositories are private.

### Step 4: Launch
1. Click **Create server**.
2. Scaleway will provision the hardware and execute Cloud-init on the first boot. Within 2–3 minutes, your blockchain node and Web Explorer will be fully operational.

---

## 3. Method B: Deploy via Scaleway CLI (`scw`)

If you have installed and configured the [Scaleway CLI](https://github.com/scaleway/scaleway-cli):

### Deploying an Elastic Metal Bare-Metal Server:
```bash
scw baremetal server create \
  zone=fr-par-2 \
  name=oenexa-validator-01 \
  type=EM-B1110X-SATA \
  os-id=ubuntu_jammy \
  cloud-init=@scaleway-cloud-init.yaml
```

### Deploying a Cloud Instance VM:
```bash
scw instance server create \
  zone=fr-par-1 \
  name=oenexa-node-01 \
  type=PRO2-M \
  image=ubuntu_jammy \
  cloud-init=@scaleway-cloud-init.yaml \
  ip=new
```

---

## 4. Method C: Deploy via Terraform

If you use Terraform to manage your Scaleway infrastructure:

```hcl
resource "scaleway_instance_server" "oenexa_node" {
  name  = "oenexa-production-node"
  type  = "PRO2-M"
  image = "ubuntu_jammy"

  user_data = {
    cloud-init = file("${path.module}/scaleway-cloud-init.yaml")
  }

  tags = ["blockchain", "oenexa", "layer1"]
}

output "oenexa_public_ip" {
  value = scaleway_instance_server.oenexa_node.public_ip
}
```

---

## 5. Firewall & Network Security Architecture

Cloud-init automatically configures `ufw` with the following strict ingress rules:

| Port | Protocol | Scope | Purpose |
| :--- | :--- | :--- | :--- |
| **22** | TCP | Public | SSH Remote System Administration |
| **8765** | TCP | **Public** | **OENEXA P2P Consensus & GossipSub Mesh** |
| **80** | TCP | Public | Web Explorer Dashboard & Reverse Proxied JSON-RPC |
| **443** | TCP | Public | Secure HTTPS (via Caddy or Nginx) |
| **8545** | TCP | **Localhost Only** | Core JSON-RPC 2.0 Engine (denied publicly) |
| **9090** | TCP | **Localhost Only** | Prometheus Metrics / Healthz (denied publicly) |

---

## 6. Post-Boot Verification & `oenexa-ctl` Cheatsheet

Once your Scaleway server is ready, SSH into the machine:

```bash
ssh oenexa@<YOUR_SERVER_PUBLIC_IP>
```

You will be greeted by the custom OENEXA Motd login banner.

### Monitor Cloud-init Logs
```bash
# Watch the live boot provisioning log
sudo tail -f /var/log/oenexa-deploy.log

# Or check system cloud-init output
sudo tail -f /var/log/cloud-init-output.log
```

### Stack Management Utility (`oenexa-ctl`)
We have built an operator CLI tool located at `/usr/local/bin/oenexa-ctl`:

```bash
# 1. Check container and blockchain node health
oenexa-ctl status

# 2. View live logs from all containers
oenexa-ctl logs

# 3. View live logs specifically from the blockchain node
oenexa-ctl logs oenexa-node

# 4. Check current block height
oenexa-ctl height

# 5. Dispatch a direct JSON-RPC query
oenexa-ctl rpc oen_chainInfo

# 6. Restart the stack
oenexa-ctl restart
```

### Accessing the Web Explorer:
Open your browser and navigate to:
```
http://<YOUR_SERVER_PUBLIC_IP>
```
You will see the live OENEXA Decoupled Web Explorer dashboard displaying blocks, peer counts, and mempool activity.

### Testing the Web3 JSON-RPC Endpoint:
From any external machine:
```bash
curl -X POST http://<YOUR_SERVER_PUBLIC_IP>/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"oen_chainInfo","params":null}'
```

---

## 7. Keystore Backup & Security Checklist

> ⚠️ **CRITICAL REQUIREMENT:** The node's NIST PQC signing keys are encrypted at rest in `/opt/oenexa/backups` or the container volume. You **must** back up your keystore off-server immediately.

### 1. Perform Keystore Backup:
```bash
sudo oenexa-ctl backup-keystore
```
The encrypted keystore JSON is saved to `/opt/oenexa/backups/keystore_backup_<timestamp>.json`.

### 2. Copy the Keystore to Your Local Machine:
```bash
scp oenexa@<YOUR_SERVER_PUBLIC_IP>:/opt/oenexa/backups/keystore_backup_*.json ~/oen-keystore-backup.json
```

### 3. Check Decryption Password:
Your password was either specified by you or securely auto-generated during cloud-init. You can view it in the locked environment file:
```bash
sudo grep OEN_PASSWORD /opt/oenexa/.env
```
Store both the password and the keystore in a secure password manager or offline cold vault.
