#!/bin/bash

# Linux startup uninstaller for Necoconeco

echo "Uninstalling Necoconeco from Linux startup..."
echo ""

REMOVED_SOMETHING=false

# Remove systemd user service
echo "Checking systemd user service..."
if systemctl --user is-enabled necoconeco.service >/dev/null 2>&1; then
    echo "Stopping and disabling systemd service..."
    systemctl --user stop necoconeco.service 2>/dev/null
    systemctl --user disable necoconeco.service 2>/dev/null
    
    if [ -f ~/.config/systemd/user/necoconeco.service ]; then
        rm ~/.config/systemd/user/necoconeco.service
        systemctl --user daemon-reload
        echo "✓ Removed systemd service"
        REMOVED_SOMETHING=true
    fi
else
    echo "- No systemd service found"
fi

# Remove XDG autostart entry
echo "Checking XDG autostart..."
if [ -f ~/.config/autostart/necoconeco.desktop ]; then
    rm ~/.config/autostart/necoconeco.desktop
    echo "✓ Removed XDG autostart entry"
    REMOVED_SOMETHING=true
else
    echo "- No XDG autostart entry found"
fi

# Remove from .profile
echo "Checking ~/.profile..."
if [ -f ~/.profile ] && grep -q "necoconeco" ~/.profile; then
    echo "Removing entries from ~/.profile..."
    # Create backup
    cp ~/.profile ~/.profile.backup.$(date +%Y%m%d_%H%M%S)
    
    # Remove necoconeco-related lines
    grep -v "necoconeco" ~/.profile > ~/.profile.tmp
    # Remove empty lines that might be left behind
    awk 'NF > 0 || prev_nf > 0 {print} {prev_nf = NF}' ~/.profile.tmp > ~/.profile
    rm ~/.profile.tmp
    
    echo "✓ Removed entries from ~/.profile"
    echo "  (Backup saved as ~/.profile.backup.*)"
    REMOVED_SOMETHING=true
else
    echo "- No entries found in ~/.profile"
fi

# Remove from other common locations
echo "Checking other startup locations..."

# Check .bashrc
if [ -f ~/.bashrc ] && grep -q "necoconeco" ~/.bashrc; then
    echo "Found entries in ~/.bashrc - removing..."
    cp ~/.bashrc ~/.bashrc.backup.$(date +%Y%m%d_%H%M%S)
    grep -v "necoconeco" ~/.bashrc > ~/.bashrc.tmp && mv ~/.bashrc.tmp ~/.bashrc
    echo "✓ Removed entries from ~/.bashrc"
    REMOVED_SOMETHING=true
fi

# Check .bash_profile
if [ -f ~/.bash_profile ] && grep -q "necoconeco" ~/.bash_profile; then
    echo "Found entries in ~/.bash_profile - removing..."
    cp ~/.bash_profile ~/.bash_profile.backup.$(date +%Y%m%d_%H%M%S)
    grep -v "necoconeco" ~/.bash_profile > ~/.bash_profile.tmp && mv ~/.bash_profile.tmp ~/.bash_profile
    echo "✓ Removed entries from ~/.bash_profile"
    REMOVED_SOMETHING=true
fi

echo ""
if [ "$REMOVED_SOMETHING" = true ]; then
    echo "✓ Uninstallation complete! Necoconeco will no longer start automatically."
    echo ""
    echo "Note: You may need to log out and back in for changes to take full effect."
else
    echo "ℹ No startup entries found. Necoconeco was not configured to start automatically."
fi

echo ""
echo "Press Enter to exit..."
read