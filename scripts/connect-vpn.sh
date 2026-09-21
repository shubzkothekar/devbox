#!/usr/bin/env bash
set -e

# Helper script to connect to OpenVPN inside the container
VPN_DIR="/workspace/vpn"
LOG_FILE="/var/log/openvpn.log"

echo "=== DevBox OpenVPN Connection Helper ==="

# 1. Verify TUN device
if [ ! -c /dev/net/tun ]; then
    echo "Creating /dev/net/tun device node..."
    sudo mkdir -p /dev/net
    sudo mknod /dev/net/tun c 10 200
    sudo chmod 600 /dev/net/tun
fi

# 2. Check if OpenVPN is already active
if pgrep -x "openvpn" > /dev/null; then
    echo "⚠️ OpenVPN process is already running."
    echo "Current status of tun0:"
    ip addr show tun0 2>/dev/null || echo "tun0 interface not up yet."
    echo "To stop it, run: sudo killall openvpn"
    exit 0
fi

# 3. Locate OVPN configuration file
CONFIG_FILE="$1"

if [ -z "$CONFIG_FILE" ]; then
    # Try finding an .ovpn file in VPN_DIR
    CONFIG_FILE=$(find "$VPN_DIR" -maxdepth 1 -name "*.ovpn" | head -n 1)
fi

if [ -z "$CONFIG_FILE" ] || [ ! -f "$CONFIG_FILE" ]; then
    echo "❌ Error: No .ovpn configuration file found."
    echo "Please place your .ovpn profile in $VPN_DIR (or run: connect-vpn /path/to/profile.ovpn)"
    exit 1
fi

echo "Using OpenVPN config: $CONFIG_FILE"

# 4. Check for auth-user-pass file if present
AUTH_ARGS=""
if [ -f "$VPN_DIR/auth.txt" ]; then
    echo "Found $VPN_DIR/auth.txt for automated credentials."
    AUTH_ARGS="--auth-user-pass $VPN_DIR/auth.txt"
fi

# 5. Connect
echo "Starting OpenVPN in background..."
sudo openvpn --config "$CONFIG_FILE" $AUTH_ARGS --log "$LOG_FILE" --daemon

echo "Waiting for VPN connection to establish (checking tun0)..."
CONNECTED=0
for i in {1..15}; do
    if ip addr show dev tun0 2>/dev/null | grep -q "inet"; then
        CONNECTED=1
        break
    fi
    sleep 1
    echo -n "."
done
echo ""

if [ $CONNECTED -eq 1 ]; then
    echo "✅ OpenVPN connected successfully!"
    echo "--- TUN0 Interface Info ---"
    ip addr show tun0
    echo "---------------------------"
else
    echo "⚠️ OpenVPN did not bring up tun0 within 15 seconds."
    echo "Check logs with: tail -n 30 $LOG_FILE"
fi
