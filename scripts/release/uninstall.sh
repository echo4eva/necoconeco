#!/bin/bash
# A script to uninstall necoconeco and remove its systemd service.

# --- Configuration ---
INSTALL_DIR="/opt/necoconeco"
SERVICE_NAME="necoconeco"

# --- Run as root check ---
if [ "$EUID" -ne 0 ]; then
  echo "Please run this script with sudo: sudo ./uninstall.sh"
  exit
fi

echo "--- Necoconeco Linux Uninstaller ---"

# --- Stop and Disable the Service ---
echo "Stopping and disabling systemd service..."
systemctl stop ${SERVICE_NAME}.service
systemctl disable ${SERVICE_NAME}.service

# --- Remove Service File ---
echo "Removing systemd service file..."
rm /etc/systemd/system/${SERVICE_NAME}.service
systemctl daemon-reload

# --- Remove Application Directory ---
echo "Removing application directory at $INSTALL_DIR..."
rm -rf "$INSTALL_DIR"

echo "--- Uninstallation Complete ---"