# Raspberry Pi Kiosk Display & Touchscreen Tuning

This guide covers display optimization, Chromium kiosk flags, screen blanking management, and touchscreen calibration for the 15-inch wall/refrigerator display.

---

## 1. Chromium Kiosk Flags Reference

The kiosk startup script (`deploy/pi/kiosk/familyboard-kiosk.sh`) launches Chromium with tailored flags optimized for memory-constrained SBCs:

```bash
chromium \
    --kiosk \
    --noerrdialogs \
    --disable-infobars \
    --no-first-run \
    --fast \
    --fast-start \
    --disable-session-crashed-bubble \
    --disable-features=TranslateUI \
    --disable-pinch \
    --disable-sync \
    --disable-background-networking \
    --disable-default-apps \
    --hide-scrollbars \
    --overscroll-history-navigation=0 \
    --check-for-update-interval=31536000 \
    --disk-cache-dir=/tmp/chromium-cache \
    --disk-cache-size=33554432 \
    "http://127.0.0.1:8080/kiosk"
```

### Key Flag Explanations:
- `--kiosk`: Enforces true borderless fullscreen without browser navigation bars.
- `--noerrdialogs` & `--disable-session-crashed-bubble`: Prevents popups if power is suddenly cut.
- `--disk-cache-dir=/tmp/chromium-cache`: Directs browser cache to RAM (`tmpfs`), avoiding microSD wear.
- `--disk-cache-size=33554432`: Restricts browser disk cache to 32 MB.

---

## 2. Display Power Management (DPMS) & Screen Blanking

By default, Raspberry Pi OS blanks HDMI displays after 10 minutes of inactivity. For an always-on kitchen glanceable display:

### Disable Screen Blanking:
In `familyboard-kiosk.sh`:
```bash
xset s off          # Disable screen saver
xset -dpms          # Disable Display Power Management Signaling
xset s noblank      # Do not blank video device
```

### Night Schedule (Optional Display Sleep):
To conserve power and turn the display off between 11:00 PM and 6:30 AM:
```bash
# /etc/cron.d/familyboard-screen
0 23 * * * root vcgencmd display_power 0
30 6 * * * root vcgencmd display_power 1
```

---

## 3. Touchscreen Calibration & Rotation

Most 15-inch USB touch displays identify as standard HID multi-touch devices via `libinput`.

### Screen Rotation (Landscape vs. Portrait)
If your refrigerator mount requires portrait orientation (vertical 1080x1920):

1. **Rotate Video Output:**
   Edit `/boot/firmware/cmdline.txt` (or `/boot/cmdline.txt` on older releases), appending:
   ```text
   video=HDMI-A-1:1920x1080M@60,rotate=90
   ```
   Or in `/etc/X11/xorg.conf.d/10-monitor.conf`:
   ```xorg.conf
   Section "Monitor"
       Identifier "HDMI-1"
       Option "Rotate" "right"
   EndSection
   ```

2. **Rotate Touch Coordinate Matrix:**
   Verify your touch device name:
   ```bash
   xinput list
   ```
   Apply a 90-degree coordinate transformation matrix:
   ```bash
   # Portrait rotation (clockwise 90 deg)
   xinput set-prop "Touch Device Name" "Coordinate Transformation Matrix" 0 1 0 -1 0 1 0 0 1
   ```
   To make permanent, create `/etc/X11/xorg.conf.d/99-calibration.conf`:
   ```xorg.conf
   Section "InputClass"
       Identifier "calibration"
       MatchProduct "Touch Device Name"
       Option "TransformationMatrix" "0 1 0 -1 0 1 0 0 1"
   EndSection
   ```

---

## 4. Cursor Hiding

The `unclutter` utility runs in the background of the kiosk session:
```bash
unclutter -idle 0.5 -root &
```
The mouse pointer disappears after 0.5 seconds of inactivity and reappears only if a physical mouse is plugged in and moved.
