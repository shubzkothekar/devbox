# OpenVPN Configuration Directory

Place your `.ovpn` configuration file and credentials in this directory.

## Instructions

1. Copy your `.ovpn` file into this folder:
   ```bash
   cp /path/to/your/client.ovpn ./vpn/
   ```

2. (Optional) If your VPN requires username/password authentication without prompting:
   Create a file named `auth.txt` in this directory:
   ```
   your_username
   your_password
   ```
   And ensure your `.ovpn` has `auth-user-pass auth.txt` or use `connect-vpn` which detects `auth.txt` automatically.

3. Inside the container (or via SSH), connect anytime:
   ```bash
   connect-vpn
   ```
   Or specify a file directly:
   ```bash
   connect-vpn /workspace/vpn/my-custom-client.ovpn
   ```

4. Verify VPN connection inside container:
   ```bash
   ip a show tun0
   ping <internal-database-ip>
   ```
