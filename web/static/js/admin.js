// FamilyBoard administration UI
(function() {
  'use strict';

  const IDLE_TIMEOUT_MS = 15 * 60 * 1000;
  const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content || '';
  let idleTimer;

  function apiFetch(url, options) {
    const requestOptions = { ...(options || {}) };
    const headers = new Headers(requestOptions.headers || {});
    if (csrfToken && requestOptions.method && requestOptions.method.toUpperCase() !== 'GET') {
      headers.set('X-CSRF-Token', csrfToken);
    }
    requestOptions.headers = headers;
    return window.fetch(url, requestOptions);
  }

  function showToast(message, kind) {
    const region = document.getElementById('app-toast-region');
    if (!region) return;

    const toast = document.createElement('div');
    toast.className = 'app-toast app-toast-' + (kind || 'info');
    toast.setAttribute('role', kind === 'error' ? 'alert' : 'status');
    const text = document.createElement('span');
    text.textContent = message;
    const dismiss = document.createElement('button');
    dismiss.type = 'button';
    dismiss.className = 'app-toast-dismiss';
    dismiss.setAttribute('aria-label', 'Dismiss message');
    dismiss.textContent = '×';
    dismiss.addEventListener('click', function() { toast.remove(); });
    toast.append(text, dismiss);
    region.appendChild(toast);
    window.setTimeout(function() { toast.remove(); }, kind === 'error' ? 8000 : 4500);
  }

  function refreshAfterFeedback(message, kind, delay) {
    showToast(message, kind || 'success');
    window.setTimeout(function() { window.location.reload(); }, delay || 650);
  }

  function resetIdleTimer() {
    window.clearTimeout(idleTimer);
    idleTimer = window.setTimeout(function() {
      apiFetch('/admin/lock', { method: 'POST' })
        .finally(function() { window.location.assign('/'); });
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

    setupAppleCalendarModal();
    setupHardwareTab();
  }

  function setupAppleCalendarModal() {
    const modal = document.getElementById('modal-apple-connect');
    const authType = document.getElementById('apple_auth_type');
    if (!modal || !authType) return;

    const personSelect = document.getElementById('apple_person_id');
    const webcalGroup = document.getElementById('apple-webcal-fields');
    const caldavGroup = document.getElementById('apple-caldav-fields');
    const webcalInput = document.getElementById('apple_webcal_url');
    const caldavInputs = [
      document.getElementById('apple_id') || document.getElementById('apple_email'),
      document.getElementById('apple_app_password')
    ].filter(Boolean);

    function toggleFields() {
      const useCalDAV = authType.value === 'caldav';
      if (webcalGroup) webcalGroup.classList.toggle('hidden', useCalDAV);
      if (caldavGroup) caldavGroup.classList.toggle('hidden', !useCalDAV);
      if (webcalInput) {
        webcalInput.disabled = useCalDAV;
        webcalInput.required = !useCalDAV;
      }
      caldavInputs.forEach(function(input) {
        input.disabled = !useCalDAV;
        input.required = useCalDAV;
      });
    }

    function closeModal() {
      if (typeof modal.close === 'function') modal.close();
      else modal.removeAttribute('open');
    }

    document.querySelectorAll('.js-open-apple-modal').forEach(function(button) {
      button.addEventListener('click', function() {
        if (personSelect) personSelect.value = button.dataset.personId || '';
        toggleFields();
        if (typeof modal.showModal === 'function') modal.showModal();
        else modal.setAttribute('open', '');
        window.setTimeout(function() { authType.focus(); }, 0);
      });
    });
    document.querySelectorAll('.js-close-apple-modal').forEach(function(button) {
      button.addEventListener('click', closeModal);
    });
    modal.addEventListener('click', function(event) {
      const rect = modal.getBoundingClientRect();
      const outside = event.clientX < rect.left || event.clientX > rect.right ||
        event.clientY < rect.top || event.clientY > rect.bottom;
      if (outside) closeModal();
    });
    authType.addEventListener('change', toggleFields);
    toggleFields();
  }

  function setupHardwareTab() {
    // 1. Wi-Fi Rescan
    const btnRescanWifi = document.getElementById('btn-rescan-wifi');
    if (btnRescanWifi) {
      btnRescanWifi.addEventListener('click', async function() {
        btnRescanWifi.disabled = true;
        btnRescanWifi.textContent = 'Scanning…';
        try {
          const resp = await apiFetch('/api/system/wifi/scan');
          if (!resp.ok) throw new Error('Scan failed');
          const aps = await resp.json();
          renderWifiNetworks(aps);
        } catch (err) {
          showToast('Wi-Fi scan failed: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/wifi/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ssid, password })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) {
            throw new Error(res.error || 'Connection failed');
          }
          refreshAfterFeedback('Connected to ' + ssid + ' successfully.');
        } catch (err) {
          showToast('Failed to connect: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/wifi/hotspot', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enable: !currentlyActive })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Toggle failed');
          refreshAfterFeedback(currentlyActive ? 'Setup hotspot disabled.' : 'Setup hotspot enabled.');
        } catch (err) {
          showToast('Failed to toggle hotspot: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/bluetooth/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ powered: !currentlyPowered })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Bluetooth toggle failed');
          refreshAfterFeedback(currentlyPowered ? 'Bluetooth turned off.' : 'Bluetooth turned on.');
        } catch (err) {
          showToast('Failed to toggle Bluetooth: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/bluetooth/scan');
          if (!resp.ok) throw new Error('Bluetooth scan failed');
          const devices = await resp.json();
          renderBluetoothDevices(devices);
        } catch (err) {
          showToast('Bluetooth scan failed: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/presence/beacon', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ person_name: personName, device_name: deviceName, mac_address: macAddress })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Failed to register beacon');
          refreshAfterFeedback('Presence beacon registered.');
        } catch (err) {
          showToast('Could not register beacon: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/presence/beacon/' + encodeURIComponent(id), {
            method: 'DELETE'
          });
          if (!resp.ok) throw new Error('Delete failed');
          refreshAfterFeedback('Presence beacon removed.');
        } catch (err) {
          showToast('Failed to remove beacon: ' + err.message, 'error');
        }
      }
    });

    // 8. Quick Add from Bluetooth Scanned List & Keyboard Actions
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

    // 8b. Bluetooth Wireless Keyboard Connect / Disconnect / Forget
    document.addEventListener('click', async function(e) {
      const btnConnect = e.target.closest('.btn-bt-connect');
      if (btnConnect) {
        const mac = btnConnect.dataset.mac;
        if (!mac) return;
        btnConnect.disabled = true;
        btnConnect.textContent = 'Connecting…';
        try {
          const resp = await apiFetch('/api/system/bluetooth/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mac_address: mac })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Failed to connect');
          refreshAfterFeedback('Device paired and connected. It is ready for the kiosk.');
        } catch (err) {
          showToast('Bluetooth connect error: ' + err.message, 'error');
          btnConnect.disabled = false;
          btnConnect.textContent = 'Pair & Connect';
        }
        return;
      }

      const btnDisconnect = e.target.closest('.btn-bt-disconnect');
      if (btnDisconnect) {
        const mac = btnDisconnect.dataset.mac;
        if (!mac) return;
        btnDisconnect.disabled = true;
        btnDisconnect.textContent = 'Disconnecting…';
        try {
          const resp = await apiFetch('/api/system/bluetooth/disconnect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mac_address: mac })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Failed to disconnect');
          refreshAfterFeedback('Bluetooth device disconnected.');
        } catch (err) {
          showToast('Bluetooth disconnect error: ' + err.message, 'error');
          btnDisconnect.disabled = false;
          btnDisconnect.textContent = 'Disconnect';
        }
        return;
      }

      const btnRemove = e.target.closest('.btn-bt-remove');
      if (btnRemove) {
        const mac = btnRemove.dataset.mac;
        if (!mac) return;
        if (!confirm('Forget / unpair Bluetooth device ' + mac + '?')) return;
        btnRemove.disabled = true;
        try {
          const resp = await apiFetch('/api/system/bluetooth/remove', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mac_address: mac })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) throw new Error(res.error || 'Failed to remove device');
          refreshAfterFeedback('Bluetooth device forgotten.');
        } catch (err) {
          showToast('Bluetooth remove error: ' + err.message, 'error');
          btnRemove.disabled = false;
        }
        return;
      }
    });

    // 9. Appliance Power: Reboot and Shutdown
    const btnReboot = document.getElementById('btn-reboot-appliance');
    if (btnReboot) {
      btnReboot.addEventListener('click', async function() {
        if (!confirm('Restart the FamilyBoard appliance now?')) return;
        try {
          const resp = await apiFetch('/api/system/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'reboot' })
          });
          if (!resp.ok) throw new Error('Restart request was rejected');
          showToast('Appliance is restarting. This page will reload once ready.', 'warning');
          setTimeout(() => window.location.reload(), 15000);
        } catch (err) {
          showToast('Reboot request error: ' + err.message, 'error');
        }
      });
    }

    const btnShutdown = document.getElementById('btn-shutdown-appliance');
    if (btnShutdown) {
      btnShutdown.addEventListener('click', async function() {
        if (!confirm('Shut down the Radxa Dragon appliance now? You will need to unplug and plug it back in to turn it on.')) return;
        try {
          const resp = await apiFetch('/api/system/power', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'shutdown' })
          });
          if (!resp.ok) throw new Error('Shutdown request was rejected');
          showToast('Appliance is shutting down safely.', 'warning');
        } catch (err) {
          showToast('Shutdown request error: ' + err.message, 'error');
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
          const resp = await apiFetch('/api/system/storage/migrate-nvme', {
            method: 'POST'
          });
          const res = await resp.json();
          if (!resp.ok || !res.success) {
            throw new Error(res.error || 'NVMe migration failed');
          }
          refreshAfterFeedback('NVMe migration complete. Shut down, remove the MicroSD card, then boot from NVMe.', 'success', 1400);
        } catch (err) {
          showToast('Migration error: ' + err.message, 'error');
          btnMigrateNVMe.disabled = false;
          btnMigrateNVMe.textContent = '🚀 Clone & Port System to NVMe SSD';
        }
      });
    }

    setupFotoCubePhotoManager();
  }

  function setupFotoCubePhotoManager() {
    const dropzone = document.getElementById('photo-dropzone');
    const fileInput = document.getElementById('photo-file-input');
    const galleryGrid = document.getElementById('photo-gallery-grid');
    const countBadge = document.getElementById('photo-count-badge');
    const spinner = document.getElementById('photo-upload-spinner');

    if (!dropzone || !galleryGrid) return;

    loadPhotos();

    // Click to upload
    dropzone.addEventListener('click', function() {
      if (fileInput) fileInput.click();
    });

    dropzone.addEventListener('keydown', function(e) {
      if ((e.key === 'Enter' || e.key === ' ') && fileInput) {
        e.preventDefault();
        fileInput.click();
      }
    });

    // File input selection
    if (fileInput) {
      fileInput.addEventListener('change', function() {
        if (fileInput.files && fileInput.files.length > 0) {
          uploadFiles(Array.from(fileInput.files));
          fileInput.value = '';
        }
      });
    }

    // Drag and drop events
    ['dragenter', 'dragover'].forEach(function(eventName) {
      dropzone.addEventListener(eventName, function(e) {
        e.preventDefault();
        e.stopPropagation();
        dropzone.classList.add('drag-over');
      });
    });

    ['dragleave', 'drop'].forEach(function(eventName) {
      dropzone.addEventListener(eventName, function(e) {
        e.preventDefault();
        e.stopPropagation();
        dropzone.classList.remove('drag-over');
      });
    });

    dropzone.addEventListener('drop', function(e) {
      if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
        uploadFiles(Array.from(e.dataTransfer.files));
      }
    });

    // Delete photo click handler (delegated)
    galleryGrid.addEventListener('click', async function(e) {
      const deleteBtn = e.target.closest('.btn-delete-photo');
      if (!deleteBtn) return;

      const filename = deleteBtn.dataset.filename;
      if (!filename) return;

      if (!confirm('Remove "' + filename + '" from the FotoCube slideshow?')) {
        return;
      }

      deleteBtn.disabled = true;
      deleteBtn.textContent = '…';

      try {
        const resp = await apiFetch('/api/photos/' + encodeURIComponent(filename), {
          method: 'DELETE'
        });
        const res = await resp.json();
        if (!resp.ok || res.error) {
          throw new Error(res.error || 'Failed to delete photo');
        }
        await loadPhotos();
      } catch (err) {
        showToast('Could not remove photo: ' + err.message, 'error');
        deleteBtn.disabled = false;
        deleteBtn.textContent = '🗑️';
      }
    });

    async function loadPhotos() {
      try {
        const resp = await apiFetch('/api/photos');
        if (!resp.ok) throw new Error('Failed to fetch photos');
        const data = await resp.json();
        const photos = Array.isArray(data.photos)
          ? data.photos.map(normalizePhotoURL).filter(Boolean)
          : [];

        if (countBadge) {
          countBadge.textContent = photos.length + (photos.length === 1 ? ' Photo' : ' Photos');
        }

        if (photos.length === 0) {
          galleryGrid.innerHTML = `
            <div class="photo-empty-state">
              <p>No custom photos uploaded yet.</p>
              <small class="admin-text-muted">FotoCube is currently displaying default atmospheric backgrounds. Drop your family photos above to personalize your board!</small>
            </div>
          `;
          return;
        }

        let html = '';
        photos.forEach(function(photoUrl) {
          const filename = photoUrl.split('/').pop();
          const displayName = filename.replace(/^\\d+_/, '');
          html += `
            <div class="photo-manager-card">
              <div class="photo-manager-thumb-box">
                <img src="${escapeHtml(photoUrl)}" alt="${escapeHtml(displayName)}" class="photo-manager-thumb" loading="lazy">
              </div>
              <div class="photo-manager-meta">
                <span class="photo-manager-name" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span>
                <button type="button" class="btn btn-danger btn-sm btn-delete-photo" data-filename="${escapeHtml(filename)}" aria-label="Delete ${escapeHtml(displayName)}">
                  🗑️
                </button>
              </div>
            </div>
          `;
        });
        galleryGrid.innerHTML = html;
      } catch (err) {
        galleryGrid.innerHTML = `<div class="admin-empty-cell">Could not load photos: ${escapeHtml(err.message)}</div>`;
      }
    }

    async function uploadFiles(files) {
      if (spinner) spinner.classList.remove('hidden');
      let uploadedCount = 0;
      let errorMessages = [];

      for (const file of files) {
        const validTypes = ['image/jpeg', 'image/png', 'image/webp'];
        if (!validTypes.includes(file.type)) {
          errorMessages.push(file.name + ': Unsupported image format (use JPG, PNG, or WebP)');
          continue;
        }

        const formData = new FormData();
        formData.append('photo', file);

        try {
          const resp = await apiFetch('/api/photos/upload', {
            method: 'POST',
            body: formData
          });
          const res = await resp.json();
          if (!resp.ok || res.error) {
            throw new Error(res.error || 'Upload failed');
          }
          uploadedCount++;
        } catch (err) {
          errorMessages.push(file.name + ': ' + err.message);
        }
      }

      if (spinner) spinner.classList.add('hidden');

      if (uploadedCount > 0) {
        showToast(uploadedCount + (uploadedCount === 1 ? ' photo uploaded.' : ' photos uploaded.'), 'success');
      }
      if (errorMessages.length > 0) {
        showToast('Upload notices: ' + errorMessages.join(' · '), 'warning');
      }

      await loadPhotos();
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
      const signalPercent = Math.max(0, Math.min(100, Number(ap.signal_percent) || 0));
      const activeBadge = ap.is_current ? '<span class="status-badge status-badge-home">Active</span>' : '<span class="admin-text-muted">Available</span>';
      const actionBtn = ap.is_current
        ? '<span class="admin-text-muted">Connected</span>'
        : `<button type="button" class="btn btn-primary btn-sm btn-connect-wifi" data-ssid="${escapeHtml(ap.ssid)}" data-security="${escapeHtml(ap.security)}" aria-label="Connect to ${escapeHtml(ap.ssid)}">Connect</button>`;

      html += `<tr>
        <td><strong>${escapeHtml(ap.ssid)}</strong></td>
        <td>${signalPercent}%</td>
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
      tbody.innerHTML = '<tr><td colspan="5" class="admin-empty-cell">No Bluetooth devices discovered.</td></tr>';
      return;
    }

    let html = '';
    devices.forEach(dev => {
      const name = dev.name || 'Unknown BLE Device';
      const rssi = Number.isFinite(Number(dev.rssi)) ? Math.round(Number(dev.rssi)) : '—';
      const isKb = dev.is_keyboard || dev.device_type === 'keyboard';
      const typeBadge = isKb
        ? '<span class="badge-keyboard">⌨️ Wireless Keyboard</span>'
        : `<span class="badge-subtle">📡 ${escapeHtml(dev.device_type || 'BLE')}</span>`;
      const connBadge = dev.connected
        ? '<span class="status-badge status-badge-home status-badge-compact">Connected</span>'
        : (dev.paired ? '<span class="status-badge status-badge-compact">Paired</span>' : '');

      let actionBtns = '';
      if (dev.connected) {
        actionBtns += `<button type="button" class="btn btn-secondary btn-sm btn-bt-disconnect" data-mac="${escapeHtml(dev.mac_address)}" aria-label="Disconnect ${escapeHtml(name)}">Disconnect</button>`;
      } else {
        const btnClass = isKb ? 'btn-primary' : 'btn-secondary';
        actionBtns += `<button type="button" class="btn ${btnClass} btn-sm btn-bt-connect" data-mac="${escapeHtml(dev.mac_address)}" aria-label="Pair and connect ${escapeHtml(name)}">Pair &amp; Connect</button>`;
      }
      actionBtns += ` <button type="button" class="btn btn-secondary btn-sm btn-quick-add-beacon" data-mac="${escapeHtml(dev.mac_address)}" data-name="${escapeHtml(name)}" aria-label="Use ${escapeHtml(dev.mac_address)} as beacon">Use for Presence</button>`;
      if (dev.paired) {
        actionBtns += ` <button type="button" class="btn btn-danger btn-sm btn-bt-remove" data-mac="${escapeHtml(dev.mac_address)}" aria-label="Forget ${escapeHtml(name)}">Forget</button>`;
      }

      html += `<tr>
        <td><strong>${escapeHtml(name)}</strong></td>
        <td>
          <div class="admin-inline-actions">
            ${typeBadge}
            ${connBadge}
          </div>
        </td>
        <td><code>${escapeHtml(dev.mac_address)}</code></td>
        <td>${rssi}${rssi === '—' ? '' : ' dBm'}</td>
        <td>
          <div class="admin-inline-actions">
            ${actionBtns}
          </div>
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

  function normalizePhotoURL(value) {
    try {
      const url = new URL(String(value), window.location.origin);
      if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/photos/')) return '';
      return url.pathname + url.search;
    } catch (_) {
      return '';
    }
  }

  document.addEventListener('DOMContentLoaded', init);
})();
