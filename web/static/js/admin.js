// FamilyBoard administration UI
(function() {
  'use strict';

  const IDLE_TIMEOUT_MS = 15 * 60 * 1000;
  let idleTimer;

  function resetIdleTimer() {
    window.clearTimeout(idleTimer);
    idleTimer = window.setTimeout(function() {
      window.location.assign('/admin/lock');
    }, IDLE_TIMEOUT_MS);
  }

  function init() {
    ['pointerdown', 'keydown', 'touchstart'].forEach(function(eventName) {
      document.addEventListener(eventName, resetIdleTimer, { passive: true });
    });
    resetIdleTimer();

    document.querySelectorAll('.admin-color-input').forEach(function(input) {
      const code = input.parentElement && input.parentElement.querySelector('.admin-color-code');
      input.addEventListener('input', function() {
        if (code) code.textContent = input.value.toUpperCase();
      });
    });

    document.querySelectorAll('.admin-alert-success, .admin-alert-danger').forEach(function(alert) {
      alert.setAttribute('role', 'status');
      alert.setAttribute('aria-live', 'polite');
    });

    setupHardwareTab();
  }

  function setupHardwareTab() {
    // 1. Wi-Fi Rescan
    const btnRescanWifi = document.getElementById('btn-rescan-wifi');
    if (btnRescanWifi) {
      btnRescanWifi.addEventListener('click', async function() {
        btnRescanWifi.disabled = true;
        btnRescanWifi.textContent = 'Scanning…';
        try {
          const resp = await fetch('/api/system/wifi/scan');
          if (!resp.ok) throw new Error('Scan failed');
          const aps = await resp.json();
          renderWifiNetworks(aps);
        } catch (err) {
          alert('Wi-Fi scan failed: ' + err.message);
        } finally {
          btnRescanWifi.disabled = false;
          btnRescanWifi.textContent = 'Scan Networks';
        }
      });
    }

    // 2. Wi-Fi Connect Button handlers (delegate)
    document.addEventListener('click', function(e) {
      const btn = e.target.closest('.btn-connect-wifi');
      if (btn) {
        const ssid = btn.dataset.ssid;
        openWifiConnectBox(ssid);
      }
    });

    const btnCancelWifi = document.getElementById('btn-cancel-wifi-connect');
    if (btnCancelWifi) {
      btnCancelWifi.addEventListener('click', function() {
        const box = document.getElementById('wifi-connect-box');
        if (box) box.classList.add('hidden');
      });
    }

    const wifiForm = document.getElementById('wifi-connect-form');
    if (wifiForm) {
      wifiForm.addEventListener('submit', async function(e) {
        e.preventDefault();
        const ssid = document.getElementById('wifi-connect-ssid').value;
        const password = document.getElementById('wifi-connect-password').value;
        const btnSubmit = document.getElementById('btn-submit-wifi-connect');

        btnSubmit.disabled = true;
        btnSubmit.textContent = 'Connecting…';

        try {
          const resp = await fetch('/api/system/wifi/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ssid, password })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) {
            throw new Error(res.error || 'Connection failed');
          }
          alert('Connected to ' + ssid + ' successfully!');
          window.location.reload();
        } catch (err) {
          alert('Failed to connect: ' + err.message);
        } finally {
          btnSubmit.disabled = false;
          btnSubmit.textContent = 'Join Network';
        }
      });
    }

    // 3. Toggle Hotspot
    const btnToggleHotspot = document.getElementById('btn-toggle-hotspot');
    if (btnToggleHotspot) {
      btnToggleHotspot.addEventListener('click', async function() {
        const currentlyActive = btnToggleHotspot.dataset.active === 'true';
        btnToggleHotspot.disabled = true;
        try {
          const resp = await fetch('/api/system/wifi/hotspot', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enable: !currentlyActive })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Toggle failed');
          window.location.reload();
        } catch (err) {
          alert('Failed to toggle hotspot: ' + err.message);
          btnToggleHotspot.disabled = false;
        }
      });
    }

    // 4. Toggle Bluetooth Power
    const btnToggleBluetooth = document.getElementById('btn-toggle-bluetooth');
    if (btnToggleBluetooth) {
      btnToggleBluetooth.addEventListener('click', async function() {
        const currentlyPowered = btnToggleBluetooth.dataset.powered === 'true';
        btnToggleBluetooth.disabled = true;
        try {
          const resp = await fetch('/api/system/bluetooth/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ powered: !currentlyPowered })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Bluetooth toggle failed');
          window.location.reload();
        } catch (err) {
          alert('Failed to toggle Bluetooth: ' + err.message);
          btnToggleBluetooth.disabled = false;
        }
      });
    }

    // 5. Bluetooth Rescan
    const btnRescanBluetooth = document.getElementById('btn-rescan-bluetooth');
    if (btnRescanBluetooth) {
      btnRescanBluetooth.addEventListener('click', async function() {
        btnRescanBluetooth.disabled = true;
        btnRescanBluetooth.textContent = 'Scanning…';
        try {
          const resp = await fetch('/api/system/bluetooth/scan');
          if (!resp.ok) throw new Error('Bluetooth scan failed');
          const devices = await resp.json();
          renderBluetoothDevices(devices);
        } catch (err) {
          alert('Bluetooth scan failed: ' + err.message);
        } finally {
          btnRescanBluetooth.disabled = false;
          btnRescanBluetooth.textContent = 'Scan Devices';
        }
      });
    }

    // 6. Register Family Beacon Form
    const formRegisterBeacon = document.getElementById('form-register-beacon');
    if (formRegisterBeacon) {
      formRegisterBeacon.addEventListener('submit', async function(e) {
        e.preventDefault();
        const personName = document.getElementById('beacon-person-name').value.trim();
        const deviceName = document.getElementById('beacon-device-name').value.trim();
        const macAddress = document.getElementById('beacon-mac-address').value.trim();

        try {
          const resp = await fetch('/api/system/presence/beacon', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ person_name: personName, device_name: deviceName, mac_address: macAddress })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Failed to register beacon');
          window.location.reload();
        } catch (err) {
          alert('Error registering beacon: ' + err.message);
        }
      });
    }

    // 7. Delete Beacon (delegate)
    document.addEventListener('click', async function(e) {
      const btn = e.target.closest('.btn-delete-beacon');
      if (btn) {
        const id = btn.dataset.id;
        if (!confirm('Remove this presence beacon?')) return;
        try {
          const resp = await fetch('/api/system/presence/beacon/' + encodeURIComponent(id), {
            method: 'DELETE'
          });
          if (!resp.ok) throw new Error('Delete failed');
          window.location.reload();
        } catch (err) {
          alert('Failed to remove beacon: ' + err.message);
        }
      }
    });

    // 8. Quick Add from Bluetooth Scanned List
    document.addEventListener('click', function(e) {
      const btn = e.target.closest('.btn-quick-add-beacon');
      if (btn) {
        const mac = btn.dataset.mac;
        const name = btn.dataset.name;
        const macInput = document.getElementById('beacon-mac-address');
        const deviceInput = document.getElementById('beacon-device-name');
        if (macInput) macInput.value = mac;
        if (deviceInput) deviceInput.value = name || 'Bluetooth Device';
        document.getElementById('beacon-person-name')?.focus();
      }
    });

    // 9. Appliance Power: Reboot and Shutdown
    const btnReboot = document.getElementById('btn-reboot-appliance');
    if (btnReboot) {
      btnReboot.addEventListener('click', async function() {
        if (!confirm('Restart the FamilyBoard appliance now?')) return;
        try {
          await fetch('/api/system/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'reboot' })
          });
          alert('Appliance is restarting. This page will reload once ready.');
          setTimeout(() => window.location.reload(), 15000);
        } catch (err) {
          alert('Reboot request error: ' + err.message);
        }
      });
    }

    const btnShutdown = document.getElementById('btn-shutdown-appliance');
    if (btnShutdown) {
      btnShutdown.addEventListener('click', async function() {
        if (!confirm('Shut down the Radxa Dragon appliance now? You will need to unplug and plug it back in to turn it on.')) return;
        try {
          await fetch('/api/system/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'shutdown' })
          });
          alert('Appliance is shutting down safely.');
        } catch (err) {
          alert('Shutdown request error: ' + err.message);
        }
      });
    }

    // 10. NVMe SSD Migration
    const btnMigrateNVMe = document.getElementById('btn-migrate-nvme');
    if (btnMigrateNVMe) {
      btnMigrateNVMe.addEventListener('click', async function() {
        if (!confirm('Start cloning the entire operating system and FamilyBoard to the M.2 NVMe SSD? This will format the NVMe SSD and take 1-3 minutes.')) return;
        btnMigrateNVMe.disabled = true;
        btnMigrateNVMe.textContent = '⏳ Porting System to NVMe... (Please Wait)';
        try {
          const resp = await fetch('/api/system/storage/migrate-nvme', {
            method: 'POST'
          });
          const res = await resp.json();
          if (!resp.ok || !res.success) {
            throw new Error(res.error || 'NVMe migration failed');
          }
          alert('NVMe migration completed successfully! When ready, shut down the appliance, remove the MicroSD card, and power on to boot directly from NVMe.');
          window.location.reload();
        } catch (err) {
          alert('Migration error: ' + err.message);
          btnMigrateNVMe.disabled = false;
          btnMigrateNVMe.textContent = '🚀 Clone & Port System to NVMe SSD';
        }
      });
    }
  }

  function openWifiConnectBox(ssid) {
    const box = document.getElementById('wifi-connect-box');
    const title = document.getElementById('wifi-connect-title');
    const hiddenSsid = document.getElementById('wifi-connect-ssid');
    const passInput = document.getElementById('wifi-connect-password');

    if (!box || !title || !hiddenSsid) return;

    hiddenSsid.value = ssid;
    title.textContent = 'Connect to ' + ssid;
    if (passInput) passInput.value = '';
    box.classList.remove('hidden');
    box.scrollIntoView({ behavior: 'smooth', block: 'center' });
    if (passInput) passInput.focus();
  }

  function renderWifiNetworks(aps) {
    const tbody = document.getElementById('wifi-networks-tbody');
    if (!tbody) return;

    if (!aps || aps.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="admin-empty-cell">No wireless networks discovered.</td></tr>';
      return;
    }

    let html = '';
    aps.forEach(ap => {
      const activeBadge = ap.is_current ? '<span class="status-badge status-badge-home">Active</span>' : '<span class="admin-text-muted">Available</span>';
      const actionBtn = ap.is_current
        ? '<span class="admin-text-muted">Connected</span>'
        : `<button type="button" class="btn btn-primary btn-sm btn-connect-wifi" data-ssid="${escapeHtml(ap.ssid)}" data-security="${escapeHtml(ap.security)}" aria-label="Connect to ${escapeHtml(ap.ssid)}">Connect</button>`;

      html += `<tr>
        <td><strong>${escapeHtml(ap.ssid)}</strong></td>
        <td>${ap.signal_percent}%</td>
        <td><span class="badge-subtle">${escapeHtml(ap.security)}</span></td>
        <td>${activeBadge}</td>
        <td>${actionBtn}</td>
      </tr>`;
    });
    tbody.innerHTML = html;
  }

  function renderBluetoothDevices(devices) {
    const tbody = document.getElementById('bluetooth-devices-tbody');
    if (!tbody) return;

    if (!devices || devices.length === 0) {
      tbody.innerHTML = '<tr><td colspan="4" class="admin-empty-cell">No Bluetooth devices discovered.</td></tr>';
      return;
    }

    let html = '';
    devices.forEach(dev => {
      const name = dev.name || 'Unknown BLE Device';
      html += `<tr>
        <td><strong>${escapeHtml(name)}</strong></td>
        <td><code>${escapeHtml(dev.mac_address)}</code></td>
        <td>${dev.rssi} dBm</td>
        <td>
          <button type="button" class="btn btn-secondary btn-sm btn-quick-add-beacon" data-mac="${escapeHtml(dev.mac_address)}" data-name="${escapeHtml(name)}" aria-label="Use ${escapeHtml(dev.mac_address)} as beacon">
            Use for Presence
          </button>
        </td>
      </tr>`;
    });
    tbody.innerHTML = html;
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  document.addEventListener('DOMContentLoaded', init);
})();
