#!/bin/bash

# A script to install necoconeco and set it up as a systemd service to run on startup.

# --- Configuration ---
# The directory where the application will be installed. /opt is standard for optional software.
INSTALL_DIR="/opt/necoconeco"
# The name of the systemd service.
SERVICE_NAME="necoconeco"

# --- Run as root check ---
if [ "$EUID" -ne 0 ]; then
  echo "Please run this script with sudo: sudo ./install.sh"
  exit
fi

echo "--- Necoconeco Linux Installer ---"

# --- Create Installation Directory ---
echo "Creating installation directory at $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

# --- Copy Application Files ---
echo "Copying application files..."
# This assumes the script is in the same folder as the other files.
cp necoconeco-client necoconeco-clientsync run.sh config.json "$INSTALL_DIR/"

# --- Make run script executable ---
chmod +x "$INSTALL_DIR/run.sh"

# --- Create systemd Service File ---
echo "Creating systemd service file..."

cat > /etc/systemd/system/${SERVICE_NAME}.service << EOL
[Unit]
Description=Necoconeco File Sync Client
After=network.target

[Service]
Type=simple
User=${SUDO_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/run.sh
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOL

echo "Service file created at /etc/systemd/system/${SERVICE_NAME}.service"

# --- Enable and Start the Service ---
echo "Reloading systemd, enabling and starting the necoconeco service..."
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}.service
systemctl start ${SERVICE_NAME}.service

echo ""
echo "--- Installation Complete! ---"
echo "Necoconeco is now running and will start automatically on boot."
echo "You can check its status with: sudo systemctl status ${SERVICE_NAME}"
echo "You can view its logs with: sudo journalctl -u ${SERVICE_NAME} -f"