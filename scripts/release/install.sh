#!/bin/bash

# Linux startup installer for Necoconeco
# Supports systemd (most modern distros) and ~/.profile fallback

echo "Installing Necoconeco startup for Linux..."

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_SCRIPT="$SCRIPT_DIR/run.sh"

# Check if run.sh exists
if [ ! -f "$RUN_SCRIPT" ]; then
    echo "✗ Error: run.sh not found in $SCRIPT_DIR"
    echo "Please make sure run.sh exists in the same directory as this installer."
    exit 1
fi

# Make run.sh executable
chmod +x "$RUN_SCRIPT"

# Function to install via systemd user service
install_systemd() {
    echo "Installing via systemd user service..."
    
    # Create systemd user directory if it doesn't exist
    mkdir -p ~/.config/systemd/user
    
    # Create service file
    cat > ~/.config/systemd/user/necoconeco.service << EOF
[Unit]
Description=Necoconeco Client Service
After=graphical-session.target

[Service]
Type=forking
ExecStart=$RUN_SCRIPT
WorkingDirectory=$SCRIPT_DIR
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

    # Reload systemd and enable service
    systemctl --user daemon-reload
    systemctl --user enable necoconeco.service
    
    if [ $? -eq 0 ]; then
        echo "✓ Successfully installed via systemd!"
        echo "Service will start on next login."
        echo "To start now: systemctl --user start necoconeco.service"
        return 0
    else
        echo "✗ Failed to enable systemd service"
        return 1
    fi
}

# Function to install via autostart desktop file (XDG)
install_autostart() {
    echo "Installing via XDG autostart..."
    
    # Create autostart directory if it doesn't exist
    mkdir -p ~/.config/autostart
    
    # Create desktop file
    cat > ~/.config/autostart/necoconeco.desktop << EOF
[Desktop Entry]
Type=Application
Name=Necoconeco
Comment=Necoconeco Client Auto-start
Exec=$RUN_SCRIPT
Path=$SCRIPT_DIR
Terminal=false
StartupNotify=false
Hidden=false
EOF

    if [ -f ~/.config/autostart/necoconeco.desktop ]; then
        echo "✓ Successfully installed via XDG autostart!"
        echo "Application will start on next desktop login."
        return 0
    else
        echo "✗ Failed to create autostart entry"
        return 1
    fi
}

# Function to install via .profile
install_profile() {
    echo "Installing via ~/.profile..."
    
    # Check if entry already exists
    if grep -q "necoconeco" ~/.profile 2>/dev/null; then
        echo "Entry already exists in ~/.profile"
        return 0
    fi
    
    # Add to .profile
    echo "" >> ~/.profile
    echo "# Necoconeco auto-start" >> ~/.profile
    echo "if [ -f \"$RUN_SCRIPT\" ]; then" >> ~/.profile
    echo "    nohup \"$RUN_SCRIPT\" >/dev/null 2>&1 &" >> ~/.profile
    echo "fi" >> ~/.profile
    
    echo "✓ Successfully added to ~/.profile!"
    echo "Application will start on next shell login."
    return 0
}

# Detect the best installation method
echo "Detecting system configuration..."

# Check if we're in a desktop environment
if [ -n "$DISPLAY" ] || [ -n "$WAYLAND_DISPLAY" ]; then
    DESKTOP_ENV=true
    echo "Desktop environment detected."
else
    DESKTOP_ENV=false
    echo "No desktop environment detected (server/headless)."
fi

# Check if systemd is available
if command -v systemctl >/dev/null 2>&1 && systemctl --user status >/dev/null 2>&1; then
    SYSTEMD_AVAILABLE=true
    echo "systemd user services available."
else
    SYSTEMD_AVAILABLE=false
    echo "systemd user services not available."
fi

# Try installation methods in order of preference
SUCCESS=false

# Method 1: systemd (preferred for modern systems)
if [ "$SYSTEMD_AVAILABLE" = true ]; then
    if install_systemd; then
        SUCCESS=true
    fi
fi

# Method 2: XDG autostart (for desktop environments)
if [ "$SUCCESS" = false ] && [ "$DESKTOP_ENV" = true ]; then
    if install_autostart; then
        SUCCESS=true
    fi
fi

# Method 3: .profile fallback
if [ "$SUCCESS" = false ]; then
    if install_profile; then
        SUCCESS=true
    fi
fi

echo ""
if [ "$SUCCESS" = true ]; then
    echo "✓ Installation complete!"
    echo ""
    echo "Necoconeco will start automatically on next login/boot."
    echo ""
    echo "Manual control commands:"
    if [ "$SYSTEMD_AVAILABLE" = true ]; then
        echo "  Start now:    systemctl --user start necoconeco.service"
        echo "  Stop:         systemctl --user stop necoconeco.service"
        echo "  Check status: systemctl --user status necoconeco.service"
    fi
    echo "  Uninstall:    ./uninstall.sh"
else
    echo "✗ Installation failed!"
    echo "You may need to manually add the startup command to your system."
    echo "Manual command: $RUN_SCRIPT"
    exit 1
fi