// FamilyBoard Kiosk Client Application (Vanilla JS - Zero External Dependencies)
(function() {
  'use strict';

  // --- State Variables ---
  let currentDate = new Date();
  let currentView = 'hybrid'; // 'hybrid', 'week', 'schedule', 'today'
  let cachedEvents = [];
  let displayCalendars = [];
  let quickTemplates = [];
  let activeInput = null;
  let isShiftActive = false;
  let isSymbolActive = false;
  let lastDeletedEvent = null;
  let undoTimeout = null;
  let renderSequence = 0;
  let naturalLanguageSequence = 0;
  let previouslyFocusedElement = null;
  let timeFormat = '12h';
  let birthdayHighlight = true;
  let eventSearchQuery = '';

  // --- DOM Elements ---
  const elClockTime = document.getElementById('clock-time');
  const elClockDate = document.getElementById('clock-date');
  const elNavTitle = document.getElementById('nav-title');
  const elDaysGrid = document.getElementById('days-grid');
  const elWeekColumnsGrid = document.getElementById('week-columns-grid');
  const elTodayFullCardsGrid = document.getElementById('today-full-cards-grid');
  const elTodayHeroDate = document.getElementById('today-hero-date');
  const elRailEventsList = document.getElementById('rail-events-list');
  const elTemplatesScrollRow = document.getElementById('templates-scroll-row');
  const elStatusBadge = document.getElementById('status-badge');
  const elStatusText = document.getElementById('status-text');

  // Containers
  const elMonthContainer = document.getElementById('month-view-container');
  const elWeekContainer = document.getElementById('week-view-container');
  const elTodayContainer = document.getElementById('today-view-container');
  const elTodaySideRail = document.getElementById('today-side-rail');
  const elScheduleContainer = document.getElementById('schedule-view-container');
  const elScheduleFeed = document.getElementById('schedule-timeline-feed');
  const elWeekAllDayRow = document.getElementById('week-allday-row');

  // Modals & Drawers
  const elEventModal = document.getElementById('event-modal');
  const elEventForm = document.getElementById('event-form');
  const elBtnSaveEvent = document.getElementById('btn-save-event');
  const elDayDrawerOverlay = document.getElementById('day-drawer-overlay');
  const elDrawerDateTitle = document.getElementById('drawer-date-title');
  const elDrawerEventsList = document.getElementById('drawer-events-list');
  const elBtnDrawerAddEvent = document.getElementById('btn-drawer-add-event');
  const elEventDetailModal = document.getElementById('event-detail-modal');
  const elWifiSetupModal = document.getElementById('modal-wifi-setup');
  const elMobilePairModal = document.getElementById('modal-mobile-pair');
  const elShortcutsHelpModal = document.getElementById('modal-shortcuts-help');

  // Virtual Keyboard
  const elVirtualKeyboard = document.getElementById('virtual-keyboard');
  const elKeyboardRows = document.getElementById('keyboard-rows');

  // Toast
  const elToastContainer = document.getElementById('toast-container');
  const elToastMessage = document.getElementById('toast-message');
  const elToastUndoBtn = document.getElementById('toast-undo-btn');

  let drawerSelectedDate = new Date();

  // --- Initialization ---
  function init() {
    applyKioskPreferences();
    updateClock();
    // Throttled clock update: updates every 15s instead of 1s, reducing idle CPU timer wakeups by 93%
    setInterval(updateClock, 15000);

    setupNetworkMonitoring();
    setupSyncStatus();
    setupHardwareTelemetry();
    setupNavigation();
    setupEventSearch();
    setupTouchSwipeNavigation();
    setupQuickAddModal();
    setupEventDetailModal();
    setupDayDrawer();
    setupShortcutsHelpModal();
    setupVirtualKeyboard();
    setupInactivityAutoReset();
    setupFotoCubePhotoFrame();
    setupMobilePairModal();
    setupMobilePwaBanner();
    setupMobileSyncTriggers();
    setupSSE();

    // A URL preference takes precedence over the appliance default.
    const urlParams = new URLSearchParams(window.location.search);
    const viewParam = normalizeView(urlParams.get('view'));
    if (viewParam) currentView = viewParam;
    syncViewButtons();

    loadCalendarsAndTemplates().then(() => {
      renderView();
    });

    document.addEventListener('keydown', handleGlobalKeydown);
  }

  function applyKioskPreferences() {
    const root = document.querySelector('.kiosk-main-layout');
    if (!root) return;
    const configuredView = normalizeView(root.dataset.defaultView);
    if (configuredView) currentView = configuredView;
    timeFormat = root.dataset.timeFormat === '24h' ? '24h' : '12h';
    birthdayHighlight = root.dataset.birthdayHighlight !== 'false';
    document.querySelectorAll('#time-chips-grid .time-chip').forEach(function(button) {
      button.textContent = formatClockValue(button.dataset.time);
    });
  }

  function normalizeView(value) {
    const normalized = value === 'month' ? 'hybrid' : value;
    return ['hybrid', 'week', 'schedule', 'today'].includes(normalized) ? normalized : '';
  }

  // --- Clock & Date ---
  function updateClock() {
    const now = new Date();
    if (elClockTime) {
      elClockTime.textContent = formatTimeShort(now);
    }
    if (elClockDate) {
      elClockDate.textContent = now.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
    }
    updateWeekNowIndicator();
  }

  // --- Status & Connectivity ---
  function updateStatus(state, text) {
    if (!elStatusBadge) return;
    elStatusBadge.className = 'status-badge ' + (state || '');
    if (elStatusText) elStatusText.textContent = text;
  }

  function setupNetworkMonitoring() {
    window.addEventListener('online', () => {
      updateStatus('syncing', 'Reconnecting…');
      renderView();
    });
    window.addEventListener('offline', () => updateStatus('offline', 'Offline · cached'));

    if (!navigator.onLine) {
      updateStatus('offline', 'Offline · cached');
    }
  }

  function setupEventSearch() {
    const input = document.getElementById('calendar-search');
    if (!input) return;
    let timer;
    input.addEventListener('input', function() {
      window.clearTimeout(timer);
      timer = window.setTimeout(function() {
        eventSearchQuery = input.value.trim().toLocaleLowerCase(document.documentElement.lang || undefined);
        renderView();
      }, 150);
    });
  }

  function filterVisibleEvents(events) {
    if (!eventSearchQuery) return events;
    return events.filter(function(event) {
      return [event.title, event.description, event.location, event.organizer]
        .some(function(value) { return String(value || '').toLocaleLowerCase(document.documentElement.lang || undefined).includes(eventSearchQuery); });
    });
  }

  function setupSyncStatus() {
    async function poll() {
      if (!navigator.onLine) return;
      try {
        const response = await fetch('/api/system/status', { cache: 'no-store' });
        if (!response.ok) throw new Error('status unavailable');
        const status = await response.json();
        const badgeState = status.state === 'healthy' ? '' : status.state;
        updateStatus(badgeState, status.message || 'Up to date');
        if (elStatusBadge) {
          const lastSync = status.oldest_last_sync ? new Date(status.oldest_last_sync).toLocaleString() : 'not synced yet';
          elStatusBadge.title = `${status.message}. Oldest calendar sync: ${lastSync}. Pending: ${status.pending_mutations}; failed: ${status.failed_mutations}.`;
        }
      } catch (error) {
        updateStatus('error', 'Status unavailable');
        if (elStatusBadge) elStatusBadge.title = 'Open Admin → System & Diagnostics for recovery steps.';
      }
    }
    poll();
    window.setInterval(poll, 30000);
  }

  function escapeKioskText(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  // --- Radxa Dragon Hardware & Presence Telemetry ---
  function setupHardwareTelemetry() {
    const elWifiBadge = document.getElementById('wifi-badge');
    const elWifiStatusText = document.getElementById('wifi-status-text');
    const elPresenceBadge = document.getElementById('presence-badge');
    const elPresenceContainer = document.getElementById('presence-members-container');

    async function pollHardware() {
      try {
        const wifiResp = await fetch('/api/system/wifi');
        if (wifiResp.ok) {
          const wifi = await wifiResp.json();
          if (elWifiBadge && elWifiStatusText) {
            if (wifi.hotspot_active) {
              elWifiBadge.classList.add('hotspot');
              elWifiBadge.title = 'Setup Hotspot Active: ' + (wifi.hotspot_ssid || 'FamilyBoard-Setup');
              elWifiStatusText.textContent = 'Setup AP';
            } else if (wifi.connected) {
              elWifiBadge.classList.remove('hotspot');
              elWifiBadge.title = `Wi-Fi: ${wifi.ssid} (${wifi.signal_percent}%, IP: ${wifi.ip_address || '--'})`;
              elWifiStatusText.textContent = wifi.ssid || 'Wi-Fi';
            } else {
              elWifiBadge.classList.remove('hotspot');
              elWifiBadge.title = 'Wi-Fi Disconnected';
              elWifiStatusText.textContent = 'Offline';
            }

            if (wifi.hotspot_active && !wifi.connected && !hasAutoPromptedSetup) {
              hasAutoPromptedSetup = true;
              setTimeout(() => openKioskWifiModal(), 2500);
            }
          }
        }
      } catch (_) {}

      try {
        const presResp = await fetch('/api/system/presence');
        if (presResp.ok) {
          const data = await presResp.json();
          const beacons = Array.isArray(data) ? data : [];
          if (elPresenceBadge && elPresenceContainer) {
            const presentMembers = beacons.filter(b => b.is_present);
            if (presentMembers.length > 0) {
              elPresenceBadge.classList.remove('hidden');
              elPresenceContainer.innerHTML = presentMembers.map(m =>
                `<span class="presence-chip" title="${escapeKioskText(m.person_name)} (${escapeKioskText(m.device_name)}) is home"><span class="presence-chip-dot"></span>${escapeKioskText(m.person_name)}</span>`
              ).join(' ');
            } else {
              elPresenceBadge.classList.add('hidden');
              elPresenceContainer.innerHTML = '';
            }
          }
        }
      } catch (_) {}
    }

    let hasAutoPromptedSetup = false;

    if (elWifiBadge) {
      elWifiBadge.addEventListener('click', function(e) {
        e.preventDefault();
        openKioskWifiModal();
      });
    }

    pollHardware();
    setInterval(pollHardware, 30000);
  }

  function openKioskWifiModal() {
    const modal = elWifiSetupModal;
    const btnClose = document.getElementById('btn-close-wifi-modal');
    const btnCloseFooter = document.getElementById('btn-close-wifi-modal-footer');
    const btnScan = document.getElementById('btn-kiosk-scan-wifi');
    const listContainer = document.getElementById('kiosk-wifi-list-container');
    const connectBox = document.getElementById('kiosk-wifi-connect-box');
    const selectedSsidLabel = document.getElementById('kiosk-selected-ssid-label');
    const passInput = document.getElementById('kiosk-wifi-password-input');
    const btnSubmitConnect = document.getElementById('btn-kiosk-submit-connect');
    const btnCancelConnect = document.getElementById('btn-kiosk-cancel-connect');
    const connectStatus = document.getElementById('kiosk-connect-status');
    const currentStatusText = document.getElementById('kiosk-current-wifi-status');
    const hotspotSsid = document.getElementById('kiosk-hotspot-ssid');

    if (!modal) return;

    previouslyFocusedElement = document.activeElement;
    modal.classList.remove('hidden');
    modal.style.display = 'flex';
    modal.setAttribute('aria-hidden', 'false');
    if (btnClose) btnClose.focus();

    if (btnClose) btnClose.onclick = closeKioskWifiModal;
    if (btnCloseFooter) btnCloseFooter.onclick = closeKioskWifiModal;
    modal.onclick = function(event) {
      if (event.target === modal) closeKioskWifiModal();
    };

    let selectedSSID = '';

    async function refreshStatusAndScan() {
      try {
        const statusResp = await fetch('/api/system/wifi');
        if (statusResp.ok) {
          const st = await statusResp.json();
          if (hotspotSsid && st.hotspot_ssid) hotspotSsid.textContent = st.hotspot_ssid;
          if (currentStatusText) {
            if (st.connected) {
              currentStatusText.textContent = `Connected to ${st.ssid || 'Wi-Fi'} (${st.signal_percent}%, IP: ${st.ip_address || '--'})`;
            } else if (st.hotspot_active) {
              currentStatusText.textContent = `Setup Hotspot Active: ${st.hotspot_ssid || 'FamilyBoard-Setup'} (192.168.4.1)`;
            } else {
              currentStatusText.textContent = 'Status: Disconnected';
            }
          }
        }
      } catch (_) {}

      if (listContainer) {
        listContainer.setAttribute('aria-busy', 'true');
        listContainer.innerHTML = '<div class="kiosk-wifi-empty">Scanning for wireless networks…</div>';
      }
      try {
        const scanResp = await fetch('/api/system/wifi/scan');
        if (!scanResp.ok) throw new Error('Scan failed');
        const aps = await scanResp.json();
        renderNetworks(aps);
      } catch (err) {
        if (listContainer) {
          listContainer.innerHTML = `<div class="kiosk-wifi-empty">Could not scan: ${escapeKioskText(err.message)}</div>`;
        }
      } finally {
        if (listContainer) listContainer.setAttribute('aria-busy', 'false');
      }
    }

    function renderNetworks(aps) {
      if (!listContainer) return;
      if (!aps || aps.length === 0) {
        listContainer.innerHTML = '<div class="kiosk-wifi-empty">No Wi-Fi networks found. Try clicking Scan again.</div>';
        return;
      }

      listContainer.replaceChildren();
      aps.forEach(ap => {
        const item = document.createElement('div');
        item.className = 'kiosk-wifi-item' + (ap.is_current ? ' active' : '');
        item.dataset.ssid = String(ap.ssid || '');

        const info = document.createElement('div');
        info.className = 'kiosk-wifi-item-info';
        const ssid = document.createElement('span');
        ssid.className = 'kiosk-wifi-item-ssid';
        ssid.textContent = ap.ssid || 'Hidden network';
        const meta = document.createElement('span');
        meta.className = 'kiosk-wifi-item-meta';
        const signal = Math.max(0, Math.min(100, Number(ap.signal_percent) || 0));
        meta.textContent = `${signal}% · ${ap.security || 'Open'}`;
        info.append(ssid, meta);

        const action = document.createElement('div');
        action.className = 'kiosk-wifi-item-action';
        if (ap.is_current) {
          const badge = document.createElement('span');
          badge.className = 'status-badge status-badge-home';
          badge.textContent = 'Connected';
          action.appendChild(badge);
        } else {
          const button = document.createElement('button');
          button.type = 'button';
          button.className = 'btn btn-primary btn-sm btn-select-wifi';
          button.dataset.ssid = String(ap.ssid || '');
          button.textContent = 'Join';
          action.appendChild(button);
        }
        item.append(info, action);
        listContainer.appendChild(item);
      });
    }

    if (listContainer) {
      listContainer.onclick = function(e) {
        const btn = e.target.closest('.btn-select-wifi') || e.target.closest('.kiosk-wifi-item');
        if (btn) {
          const ssid = btn.dataset.ssid;
          if (!ssid) return;
          selectedSSID = ssid;
          if (selectedSsidLabel) selectedSsidLabel.textContent = 'Connect to ' + ssid;
          if (connectBox) connectBox.classList.remove('hidden');
          if (passInput) {
            passInput.value = '';
            passInput.focus();
          }
          if (connectStatus) connectStatus.classList.add('hidden');
        }
      };
    }

    if (btnCancelConnect) {
      btnCancelConnect.onclick = function() {
        if (connectBox) connectBox.classList.add('hidden');
        selectedSSID = '';
      };
    }

    if (btnSubmitConnect) {
      btnSubmitConnect.onclick = async function() {
        if (!selectedSSID) return;
        const password = passInput ? passInput.value : '';
        btnSubmitConnect.disabled = true;
        btnSubmitConnect.textContent = 'Joining…';
        if (connectStatus) {
          connectStatus.classList.remove('hidden');
          connectStatus.className = 'kiosk-connect-status info';
          connectStatus.textContent = 'Connecting to ' + selectedSSID + '…';
        }

        try {
          const resp = await fetch('/api/system/wifi/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ssid: selectedSSID, password: password })
          });
          const res = await resp.json();
          if (!resp.ok || res.error) {
            throw new Error(res.error || 'Failed to connect');
          }
          if (connectStatus) {
            connectStatus.className = 'kiosk-connect-status success';
            connectStatus.textContent = 'Connected successfully to ' + selectedSSID + '!';
          }
          setTimeout(() => {
            if (connectBox) connectBox.classList.add('hidden');
            refreshStatusAndScan();
          }, 1500);
        } catch (err) {
          if (connectStatus) {
            connectStatus.className = 'kiosk-connect-status danger';
            connectStatus.textContent = 'Error: ' + err.message;
          }
        } finally {
          btnSubmitConnect.disabled = false;
          btnSubmitConnect.textContent = 'Connect';
        }
      };
    }

    if (btnScan) {
      btnScan.onclick = refreshStatusAndScan;
    }

    refreshStatusAndScan();
  }

  function closeKioskWifiModal() {
    if (!elWifiSetupModal) return;
    const wasOpen = elWifiSetupModal.getAttribute('aria-hidden') === 'false';
    elWifiSetupModal.classList.add('hidden');
    elWifiSetupModal.style.display = 'none';
    elWifiSetupModal.setAttribute('aria-hidden', 'true');
    document.getElementById('kiosk-wifi-connect-box')?.classList.add('hidden');
    const passwordInput = document.getElementById('kiosk-wifi-password-input');
    if (passwordInput) passwordInput.value = '';
    if (wasOpen) restorePreviousFocus();
  }

  // --- Navigation & View Switching ---
  function setupNavigation() {
    document.getElementById('btn-prev')?.addEventListener('click', () => navigateCalendar(-1));
    document.getElementById('btn-next')?.addEventListener('click', () => navigateCalendar(1));
    document.getElementById('btn-today')?.addEventListener('click', () => {
      triggerHaptic(10);
      currentDate = new Date();
      renderView();
    });

    document.querySelectorAll('.view-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        triggerHaptic(12);
        currentView = e.currentTarget.dataset.view;
        syncViewButtons();
        renderView();
      });
    });

    // Mobile Bottom Navigation Dock (iPhone & Poco F7)
    document.querySelectorAll('.mobile-nav-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        triggerHaptic(15);
        currentView = e.currentTarget.dataset.view;
        syncViewButtons();
        renderView();
      });
    });

    // Mobile 1-Thumb Floating Action Button
    document.getElementById('btn-mobile-fab-add')?.addEventListener('click', () => {
      triggerHaptic(18);
      openQuickAddModal(new Date());
    });

    document.getElementById('btn-add-event')?.addEventListener('click', () => {
      triggerHaptic(12);
      openQuickAddModal(new Date());
    });
    document.getElementById('btn-today-hero-add')?.addEventListener('click', () => {
      triggerHaptic(12);
      openQuickAddModal(new Date());
    });
    document.getElementById('btn-schedule-hero-add')?.addEventListener('click', () => {
      triggerHaptic(12);
      openQuickAddModal(new Date());
    });
  }

  function syncViewButtons() {
    document.querySelectorAll('.view-btn').forEach(btn => {
      const isActive = btn.dataset.view === currentView;
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });
    document.querySelectorAll('.mobile-nav-btn').forEach(btn => {
      const isActive = btn.dataset.view === currentView;
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });
  }

  function navigateCalendar(direction) {
    if (currentView === 'hybrid') {
      currentDate.setMonth(currentDate.getMonth() + direction);
    } else if (currentView === 'week') {
      currentDate.setDate(currentDate.getDate() + (direction * 7));
    } else if (currentView === 'schedule') {
      currentDate.setDate(currentDate.getDate() + (direction * 14));
    } else if (currentView === 'today') {
      currentDate.setDate(currentDate.getDate() + direction);
    }
    renderView();
  }

  // --- Load Metadata & Calendars ---
  async function loadCalendarsAndTemplates() {
    try {
      const [calResp, tmplResp] = await Promise.all([
        fetch('/api/calendars'),
        fetch('/api/kiosk/templates')
      ]);

      if (!calResp.ok || !tmplResp.ok) throw new Error('Metadata request failed');
      const calendars = await calResp.json();
      const templates = await tmplResp.json();
      displayCalendars = Array.isArray(calendars) ? calendars : [];
      quickTemplates = Array.isArray(templates) ? templates : [];
      populateDestinationTiles();
      renderQuickTemplates();
    } catch (e) {
      console.warn('Failed to load calendars/templates:', e);
      updateStatus(navigator.onLine ? 'error' : 'offline', navigator.onLine ? 'Needs attention' : 'Offline · cached');
    }
  }

  function populateDestinationTiles() {
    const grid = document.getElementById('dest-tiles-grid');
    if (!grid) return;
    grid.innerHTML = '';

    const hiddenInput = document.getElementById('event-calendar-id');

    // Find best default: calendar marked default_write_target, or named "home", or first writable
    const defaultCal = displayCalendars.find(c => c.writable && c.default_write_target) ||
                       displayCalendars.find(c => c.writable && (c.name || '').toLowerCase().includes('home')) ||
                       displayCalendars.find(c => c.writable);

    if (defaultCal && hiddenInput) {
      hiddenInput.value = defaultCal.id;
    }

    if (!defaultCal) {
      const empty = document.createElement('p');
      empty.className = 'destination-empty-state';
      empty.textContent = 'No writable calendar is connected yet. Add one in Admin settings.';
      grid.appendChild(empty);
      return;
    }

    displayCalendars.forEach((cal) => {
      if (!cal.writable) return;

      const tile = document.createElement('button');
      tile.type = 'button';
      tile.className = 'dest-tile';
      tile.dataset.calId = cal.id;

      // Color and icon mapping
      const lower = (cal.name || '').toLowerCase();
      const isHome = lower.includes('home');
      const initial = (cal.name || 'C').charAt(0).toUpperCase();
      let colorClass = 'dest-avatar-default';

      if (isHome) colorClass = 'dest-avatar-home';
      else if (lower.includes('member-1') || lower.includes('alex')) colorClass = 'dest-avatar-member-1';
      else if (lower.includes('member-2') || lower.includes('jordan')) colorClass = 'dest-avatar-member-2';
      else if (lower.includes('family')) colorClass = 'dest-avatar-family';
      else if (lower.includes('kids')) colorClass = 'dest-avatar-kids';

      const icon = isHome ? '🏠' : initial;
      const avatar = document.createElement('span');
      avatar.className = `dest-tile-avatar ${colorClass}`;
      avatar.style.backgroundColor = cal.display_color || 'var(--accent-primary)';
      avatar.textContent = icon;

      const name = document.createElement('span');
      name.className = 'dest-tile-name';
      name.textContent = cal.name || 'Calendar';
      if (cal.default_write_target) {
        const defaultBadge = document.createElement('span');
        defaultBadge.className = 'dest-default-mark';
        defaultBadge.textContent = 'Default';
        name.appendChild(defaultBadge);
      }

      tile.append(avatar, name);

      if (defaultCal && cal.id === defaultCal.id) {
        tile.classList.add('selected');
        tile.setAttribute('aria-pressed', 'true');
      } else {
        tile.setAttribute('aria-pressed', 'false');
      }

      tile.addEventListener('click', () => {
        grid.querySelectorAll('.dest-tile').forEach(t => {
          t.classList.remove('selected');
          t.setAttribute('aria-pressed', 'false');
        });
        tile.classList.add('selected');
        tile.setAttribute('aria-pressed', 'true');
        if (hiddenInput) hiddenInput.value = cal.id;
      });

      grid.appendChild(tile);
    });
  }

  function renderQuickTemplates() {
    if (!elTemplatesScrollRow) return;
    elTemplatesScrollRow.innerHTML = '';

    quickTemplates.forEach(t => {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'template-chip';
      const icon = document.createElement('span');
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = t.icon || '📅';
      const label = document.createElement('span');
      label.textContent = t.title || 'New event';
      chip.append(icon, label);
      chip.addEventListener('click', () => {
        openQuickAddWithTemplate(t);
      });
      elTemplatesScrollRow.appendChild(chip);
    });
  }

  function openQuickAddWithTemplate(tmpl) {
    openQuickAddModal(new Date());

    const titleInput = document.getElementById('event-title');
    if (titleInput) titleInput.value = tmpl.title;

    if (tmpl.location) {
      const locInput = document.getElementById('event-location');
      if (locInput) locInput.value = tmpl.location;
    }

    if (tmpl.default_duration_minutes) {
      setDuration(tmpl.default_duration_minutes);
    }

    if (tmpl.all_day) {
      const allDayCb = document.getElementById('event-all-day');
      if (allDayCb) {
        allDayCb.checked = true;
        toggleAllDayControls(true);
      }
    }

    if (tmpl.calendar_id) {
      selectCalendarDestination(tmpl.calendar_id);
    }
  }

  function selectCalendarDestination(calID) {
    const hiddenInput = document.getElementById('event-calendar-id');
    if (hiddenInput) hiddenInput.value = calID;

    const tiles = document.querySelectorAll('#dest-tiles-grid .dest-tile');
    tiles.forEach(t => {
      if (t.dataset.calId === calID) {
        t.classList.add('selected');
        t.setAttribute('aria-pressed', 'true');
      } else {
        t.classList.remove('selected');
        t.setAttribute('aria-pressed', 'false');
      }
    });
  }

  function safeStorageGet(key) {
    try {
      return localStorage.getItem(key);
    } catch (_) {
      return null;
    }
  }

  function safeStorageSet(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch (_) {}
  }

  // --- Fetch Events with Offline Resilience ---
  async function fetchEvents(startDate, endDate) {
    const startStr = startDate.toISOString();
    const endStr = endDate.toISOString();

    try {
      const resp = await fetch(`/api/events?start=${encodeURIComponent(startStr)}&end=${encodeURIComponent(endStr)}`);
      if (resp.ok) {
        const events = await resp.json();
        if (!Array.isArray(events)) throw new Error('Invalid event response');
        cachedEvents = events;
        safeStorageSet('fb_cached_events', JSON.stringify(cachedEvents));
        updateStatus('', 'Up to date');
        return filterVisibleEvents(cachedEvents);
      }
      throw new Error(`Event request failed (${resp.status})`);
    } catch (e) {
      console.warn('Fetch failed, using local offline storage cache:', e);
      const offline = safeStorageGet('fb_cached_events');
      if (offline) {
        try {
          const parsed = JSON.parse(offline);
          cachedEvents = Array.isArray(parsed) ? parsed : [];
        } catch (_) {
          cachedEvents = [];
        }
        updateStatus('offline', 'Showing cached events');
        return filterVisibleEvents(cachedEvents);
      }
      updateStatus('error', 'Could not load events');
    }
    return filterVisibleEvents(cachedEvents || []);
  }

  // --- Master View Switcher ---
  async function renderView() {
    const sequence = ++renderSequence;
    updateNavTitle();
    syncViewButtons();

    if (currentView === 'hybrid') {
      if (elMonthContainer) elMonthContainer.style.display = 'flex';
      if (elWeekContainer) elWeekContainer.style.display = 'none';
      if (elScheduleContainer) elScheduleContainer.style.display = 'none';
      if (elTodayContainer) elTodayContainer.style.display = 'none';
      if (elTodaySideRail) elTodaySideRail.style.display = 'flex';
      await renderMonthGrid();
      if (sequence !== renderSequence) return;
      await renderTodayRail();
    } else if (currentView === 'week') {
      if (elMonthContainer) elMonthContainer.style.display = 'none';
      if (elWeekContainer) elWeekContainer.style.display = 'flex';
      if (elScheduleContainer) elScheduleContainer.style.display = 'none';
      if (elTodayContainer) elTodayContainer.style.display = 'none';
      if (elTodaySideRail) elTodaySideRail.style.display = 'flex';
      await renderWeekGrid();
      if (sequence !== renderSequence) return;
      await renderTodayRail();
    } else if (currentView === 'schedule') {
      if (elMonthContainer) elMonthContainer.style.display = 'none';
      if (elWeekContainer) elWeekContainer.style.display = 'none';
      if (elScheduleContainer) elScheduleContainer.style.display = 'flex';
      if (elTodayContainer) elTodayContainer.style.display = 'none';
      if (elTodaySideRail) elTodaySideRail.style.display = 'none';
      await renderScheduleView();
    } else { // 'today'
      if (elMonthContainer) elMonthContainer.style.display = 'none';
      if (elWeekContainer) elWeekContainer.style.display = 'none';
      if (elScheduleContainer) elScheduleContainer.style.display = 'none';
      if (elTodayContainer) elTodayContainer.style.display = 'flex';
      if (elTodaySideRail) elTodaySideRail.style.display = 'none';
      await renderTodayFull();
    }
  }

  function updateNavTitle() {
    if (!elNavTitle) return;
    if (currentView === 'hybrid') {
      elNavTitle.textContent = currentDate.toLocaleDateString([], { month: 'long', year: 'numeric' });
    } else if (currentView === 'week') {
      const start = new Date(currentDate);
      start.setDate(start.getDate() - start.getDay());
      const end = new Date(start);
      end.setDate(end.getDate() + 6);
      elNavTitle.textContent = `${start.toLocaleDateString([], { month: 'short', day: 'numeric' })} – ${end.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })}`;
    } else if (currentView === 'schedule') {
      const end = new Date(currentDate);
      end.setDate(end.getDate() + 13);
      elNavTitle.textContent = `${currentDate.toLocaleDateString([], { month: 'short', day: 'numeric' })} – ${end.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })}`;
    } else {
      elNavTitle.textContent = currentDate.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
    }
  }

  // --- 1. Render Month Grid ---
  async function renderMonthGrid() {
    if (!elDaysGrid) return;
    elDaysGrid.innerHTML = '';

    const year = currentDate.getFullYear();
    const month = currentDate.getMonth();

    const firstDay = new Date(year, month, 1);
    const startGridDate = new Date(firstDay);
    startGridDate.setDate(startGridDate.getDate() - startGridDate.getDay());

    const endGridDate = new Date(startGridDate);
    endGridDate.setDate(endGridDate.getDate() + 42);

    const events = await fetchEvents(startGridDate, endGridDate);
    const today = new Date();
    const iterDate = new Date(startGridDate);

    // Pre-index non-cancelled events by date string for O(1) cell lookup
    const eventsByDate = new Map();
    events.forEach(ev => {
      if (ev.status === 'cancelled') return;
      const key = new Date(ev.start_at).toDateString();
      let list = eventsByDate.get(key);
      if (!list) {
        list = [];
        eventsByDate.set(key, list);
      }
      list.push(ev);
    });

    const fragment = document.createDocumentFragment();

    for (let i = 0; i < 42; i++) {
      const cell = document.createElement('div');
      cell.className = 'day-cell';
      cell.setAttribute('role', 'button');
      cell.tabIndex = 0;
      cell.setAttribute('aria-label', iterDate.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' }));

      const isOtherMonth = iterDate.getMonth() !== month;
      if (isOtherMonth) cell.classList.add('other-month');

      const isToday = iterDate.toDateString() === today.toDateString();
      if (isToday) cell.classList.add('today');

      // Day Number Row
      const numRow = document.createElement('div');
      numRow.className = 'day-number-row';

      const num = document.createElement('div');
      num.className = 'day-number';
      num.textContent = iterDate.getDate();
      numRow.appendChild(num);

      if (isToday) {
        const todayPill = document.createElement('span');
        todayPill.className = 'today-label-badge';
        todayPill.textContent = 'TODAY';
        numRow.appendChild(todayPill);
      }

      cell.appendChild(numRow);

      // Event Pills Container
      const pillsContainer = document.createElement('div');
      pillsContainer.className = 'event-pills-container';

      const cellDateStr = iterDate.toDateString();
      const dayEvents = eventsByDate.get(cellDateStr) || [];

      // Show top 3 events
      const maxShow = 3;
      dayEvents.slice(0, maxShow).forEach(ev => {
        const pill = document.createElement('div');
        pill.className = 'event-pill ' + getEventColorClass(ev);
        makeInteractive(pill, () => openEventDetailSheet(ev), `${ev.title}, ${ev.all_day ? 'all day' : formatTimeShort(new Date(ev.start_at))}`);

        const isBirthday = isBirthdayEvent(ev);
        const icon = isBirthday ? '🎂 ' : '';
        const timeStr = ev.all_day ? '' : formatTimeShort(new Date(ev.start_at)) + ' ';

        pill.textContent = `${icon}${timeStr}${ev.title}`;
        pill.addEventListener('click', (e) => e.stopPropagation());
        pillsContainer.appendChild(pill);
      });

      // Overflow chip
      if (dayEvents.length > maxShow) {
        const morePill = document.createElement('div');
        morePill.className = 'pill-more';
        morePill.textContent = `+${dayEvents.length - maxShow} more`;
        pillsContainer.appendChild(morePill);
      }

      cell.appendChild(pillsContainer);

      const cellDateCopy = new Date(iterDate);
      const onDayClick = () => {
        if (dayEvents.length === 0) {
          openQuickAddModal(cellDateCopy);
        } else {
          openDayDrawer(cellDateCopy, dayEvents);
        }
      };
      cell.addEventListener('click', onDayClick);
      cell.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onDayClick();
        }
      });

      fragment.appendChild(cell);
      iterDate.setDate(iterDate.getDate() + 1);
    }

    elDaysGrid.appendChild(fragment);
  }

  // --- 2. Render Week Grid ---
  async function renderWeekGrid() {
    if (!elWeekColumnsGrid) return;
    elWeekColumnsGrid.innerHTML = '';

    const startOfWeek = new Date(currentDate);
    startOfWeek.setDate(startOfWeek.getDate() - startOfWeek.getDay());
    startOfWeek.setHours(0, 0, 0, 0);

    const endOfWeek = new Date(startOfWeek);
    endOfWeek.setDate(endOfWeek.getDate() + 7);

    const events = await fetchEvents(startOfWeek, endOfWeek);
    renderWeekAllDayRow(startOfWeek, events);

    // Pre-index non-all-day events for O(1) column lookup
    const timedEventsByDate = new Map();
    events.forEach(ev => {
      if (ev.status === 'cancelled' || ev.all_day) return;
      const key = new Date(ev.start_at).toDateString();
      let list = timedEventsByDate.get(key);
      if (!list) {
        list = [];
        timedEventsByDate.set(key, list);
      }
      list.push(ev);
    });

    const fragment = document.createDocumentFragment();

    // Time Gutter (7 AM to 10 PM)
    const gutter = document.createElement('div');
    gutter.className = 'week-time-gutter';
    const headerSpacer = document.createElement('div');
    headerSpacer.style.height = '62px';
    gutter.appendChild(headerSpacer);

    for (let h = 7; h <= 21; h++) {
      const slot = document.createElement('div');
      slot.className = 'week-time-slot-label';
      slot.textContent = formatClockValue(`${String(h).padStart(2, '0')}:00`);
      gutter.appendChild(slot);
    }
    fragment.appendChild(gutter);

    // 7 Day Columns
    const today = new Date();
    const iter = new Date(startOfWeek);

    for (let d = 0; d < 7; d++) {
      const col = document.createElement('div');
      col.className = 'week-day-col';

      const isToday = iter.toDateString() === today.toDateString();

      // Header
      const header = document.createElement('div');
      header.className = 'week-day-col-header' + (isToday ? ' today' : '');
      header.innerHTML = `
        <div class="week-day-name">${iter.toLocaleDateString([], { weekday: 'short' })}</div>
        <div class="week-day-number">${iter.getDate()}</div>
      `;
      col.appendChild(header);

      // Slots area
      const slots = document.createElement('div');
      slots.className = 'week-day-slots';

      // Red Now Line if Today
      if (isToday) {
        const now = new Date();
        const currentHour = now.getHours() + (now.getMinutes() / 60);
        if (currentHour >= 7 && currentHour <= 22) {
          const topPx = (currentHour - 7) * 60;
          const nowLine = document.createElement('div');
          nowLine.className = 'now-indicator-line';
          nowLine.id = 'week-now-indicator-line';
          nowLine.style.top = `${topPx}px`;
          slots.appendChild(nowLine);
        }
      }

      // Filter events for this column via O(1) Map
      const colDateStr = iter.toDateString();
      const colEvents = timedEventsByDate.get(colDateStr) || [];

      colEvents.forEach(ev => {
        const evCard = document.createElement('div');
        evCard.className = 'week-event-card ' + getEventColorClass(ev);
        makeInteractive(evCard, () => openEventDetailSheet(ev), ev.title);

        const start = new Date(ev.start_at);
        const end = new Date(ev.end_at);

        let startHour = start.getHours() + (start.getMinutes() / 60);
        let endHour = end.getHours() + (end.getMinutes() / 60);

        const clampedStart = Math.max(7, Math.min(22, startHour));
        const clampedEnd = Math.max(clampedStart + 0.5, Math.min(22, endHour));

        const top = (clampedStart - 7) * 60;
        const height = Math.max(32, (clampedEnd - clampedStart) * 60);

        evCard.style.top = `${top}px`;
        evCard.style.height = `${height}px`;

        const isBirthday = isBirthdayEvent(ev);
        const icon = isBirthday ? '🎂 ' : '';
        const time = document.createElement('div');
        time.textContent = `${icon}${formatTimeShort(start)}`;
        const title = document.createElement('div');
        title.className = 'week-event-title';
        title.textContent = ev.title || 'Untitled event';
        evCard.append(time, title);
        evCard.addEventListener('click', (e) => e.stopPropagation());

        slots.appendChild(evCard);
      });

      const colDateCopy = new Date(iter);
      slots.addEventListener('click', (e) => {
        const rect = slots.getBoundingClientRect();
        const clickY = e.clientY - rect.top;
        const clickedHour = Math.min(21, Math.max(7, Math.floor(clickY / 60) + 7));
        const clickedTimeStr = `${String(clickedHour).padStart(2, '0')}:00`;
        openQuickAddModal(colDateCopy, clickedTimeStr);
      });

      col.appendChild(slots);
      fragment.appendChild(col);
      iter.setDate(iter.getDate() + 1);
    }

    elWeekColumnsGrid.appendChild(fragment);
  }

  function renderWeekAllDayRow(startOfWeek, events) {
    if (!elWeekAllDayRow) return;
    elWeekAllDayRow.innerHTML = '';
    const label = document.createElement('div');
    label.className = 'week-allday-gutter-label';
    label.textContent = 'All day';
    elWeekAllDayRow.appendChild(label);

    const allDayEventsByDate = new Map();
    events.forEach(event => {
      if (!event.all_day || event.status === 'cancelled') return;
      const key = new Date(event.start_at).toDateString();
      let list = allDayEventsByDate.get(key);
      if (!list) {
        list = [];
        allDayEventsByDate.set(key, list);
      }
      list.push(event);
    });

    const rowFrag = document.createDocumentFragment();
    for (let dayIndex = 0; dayIndex < 7; dayIndex++) {
      const day = new Date(startOfWeek);
      day.setDate(day.getDate() + dayIndex);
      const cell = document.createElement('div');
      cell.className = 'week-allday-cell';
      const dayEvents = allDayEventsByDate.get(day.toDateString()) || [];
      dayEvents.forEach(event => {
        const chip = document.createElement('div');
        chip.className = `week-allday-chip ${getEventColorClass(event)}`;
        chip.textContent = event.title || 'Untitled event';
        makeInteractive(chip, () => openEventDetailSheet(event), event.title);
        cell.appendChild(chip);
      });
      rowFrag.appendChild(cell);
    }
    elWeekAllDayRow.appendChild(rowFrag);
  }

  async function renderScheduleView() {
    if (!elScheduleFeed) return;
    elScheduleFeed.innerHTML = '';
    const start = new Date(currentDate);
    start.setHours(0, 0, 0, 0);
    const end = new Date(start);
    end.setDate(end.getDate() + 14);
    const events = (await fetchEvents(start, end))
      .filter(event => event.status !== 'cancelled')
      .sort((a, b) => new Date(a.start_at) - new Date(b.start_at));

    if (!events.length) {
      elScheduleFeed.appendChild(createEmptyState('🗓️', 'No plans in the next two weeks', 'Add an event to get the family agenda started.'));
      return;
    }

    const grouped = new Map();
    events.forEach(event => {
      const key = new Date(event.start_at).toDateString();
      if (!grouped.has(key)) grouped.set(key, []);
      grouped.get(key).push(event);
    });

    grouped.forEach((dayEvents, key) => {
      const date = new Date(dayEvents[0].start_at);
      const group = document.createElement('section');
      group.className = 'schedule-day-group';
      const header = document.createElement('div');
      header.className = 'schedule-day-header';
      const titleWrap = document.createElement('div');
      titleWrap.className = 'schedule-day-title-wrap';
      const title = document.createElement('h3');
      title.className = 'schedule-day-title';
      title.textContent = date.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
      titleWrap.appendChild(title);
      if (key === new Date().toDateString()) {
        const badge = document.createElement('span');
        badge.className = 'schedule-badge-today';
        badge.textContent = 'Today';
        titleWrap.appendChild(badge);
      }
      const add = document.createElement('button');
      add.type = 'button';
      add.className = 'schedule-btn-add-day';
      add.textContent = '+ Add';
      add.addEventListener('click', () => openQuickAddModal(date));
      header.append(titleWrap, add);

      const stack = document.createElement('div');
      stack.className = 'schedule-events-stack';
      dayEvents.forEach(event => {
        const card = document.createElement('div');
        card.className = `schedule-event-card ${getEventCardColorClass(event)}`;
        makeInteractive(card, () => openEventDetailSheet(event), event.title);
        const time = document.createElement('div');
        time.className = 'schedule-event-time';
        time.textContent = event.all_day ? 'All day' : formatTimeShort(new Date(event.start_at));
        const body = document.createElement('div');
        body.className = 'schedule-event-body';
        const eventTitle = document.createElement('strong');
        eventTitle.textContent = event.title || 'Untitled event';
        body.appendChild(eventTitle);
        if (event.location) {
          const meta = document.createElement('span');
          meta.className = 'schedule-event-meta';
          meta.textContent = `📍 ${event.location}`;
          body.appendChild(meta);
        }
        card.append(time, body);
        stack.appendChild(card);
      });
      group.append(header, stack);
      elScheduleFeed.appendChild(group);
    });
  }

  // --- 3. Render Today Full View ---
  async function renderTodayFull() {
    if (!elTodayFullCardsGrid) return;
    elTodayFullCardsGrid.innerHTML = '';

    const start = new Date(currentDate);
    start.setHours(0, 0, 0, 0);
    const end = new Date(start);
    end.setDate(end.getDate() + 1);

    if (elTodayHeroDate) {
      elTodayHeroDate.textContent = start.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
    }

    const events = await fetchEvents(start, end);
    const todayEvents = events.filter(ev => ev.status !== 'cancelled');

    if (todayEvents.length === 0) {
      elTodayFullCardsGrid.appendChild(createEmptyState('✨', 'Nothing planned for today', 'Add an event whenever the family makes plans.'));
      return;
    }

    todayEvents.sort((a, b) => new Date(a.start_at) - new Date(b.start_at));

    todayEvents.forEach(ev => {
      const card = document.createElement('div');
      card.className = 'today-full-card ' + getEventCardColorClass(ev);
      makeInteractive(card, () => openEventDetailSheet(ev), ev.title);

      const start = new Date(ev.start_at);
      const timeBadgeText = ev.all_day ? 'ALL DAY' : formatTimeShort(start);

      const isBirthday = (ev.title || '').toLowerCase().includes('birthday');
      const icon = isBirthday ? '🎂 ' : '';
      const timeBadge = document.createElement('div');
      timeBadge.className = 'today-card-time-badge';
      timeBadge.textContent = timeBadgeText;
      const content = document.createElement('div');
      content.className = 'today-card-content';
      const title = document.createElement('div');
      title.className = 'today-card-title';
      title.textContent = `${icon}${ev.title || 'Untitled event'}`;
      const details = document.createElement('div');
      details.className = 'today-card-details';
      const timeDetail = document.createElement('span');
      timeDetail.textContent = `📅 ${ev.all_day ? 'All-day' : `${formatTimeShort(start)} – ${formatTimeShort(new Date(ev.end_at))}`}`;
      details.appendChild(timeDetail);
      if (ev.location) {
        const location = document.createElement('span');
        location.textContent = `📍 ${ev.location}`;
        details.appendChild(location);
      }
      content.append(title, details);
      card.append(timeBadge, content);
      elTodayFullCardsGrid.appendChild(card);
    });
  }

  // --- 4. Render Today Side Rail ---
  async function renderTodayRail() {
    if (!elRailEventsList) return;
    elRailEventsList.innerHTML = '';

    const start = new Date();
    start.setHours(0, 0, 0, 0);
    const end = new Date();
    end.setDate(end.getDate() + 1);
    end.setHours(23, 59, 59, 999);

    const events = await fetchEvents(start, end);
    const todayDateStr = new Date().toDateString();
    const tomorrowDateStr = new Date(Date.now() + 86400000).toDateString();

    const todayEvents = events.filter(ev => new Date(ev.start_at).toDateString() === todayDateStr && ev.status !== 'cancelled');
    const tomorrowEvents = events.filter(ev => new Date(ev.start_at).toDateString() === tomorrowDateStr && ev.status !== 'cancelled');

    todayEvents.sort((a, b) => new Date(a.start_at) - new Date(b.start_at));
    tomorrowEvents.sort((a, b) => new Date(a.start_at) - new Date(b.start_at));

    const fragment = document.createDocumentFragment();

    if (todayEvents.length === 0) {
      fragment.appendChild(createEmptyState('✨', 'Nothing scheduled today', 'Enjoy the breathing room.'));
    } else {
      todayEvents.forEach(ev => {
        fragment.appendChild(createRailEventCard(ev));
      });
    }

    // Tomorrow Preview Section
    if (tomorrowEvents.length > 0) {
      const tmrwHeader = document.createElement('div');
      tmrwHeader.className = 'rail-section-label';
      tmrwHeader.textContent = 'Tomorrow';
      fragment.appendChild(tmrwHeader);

      tomorrowEvents.forEach(ev => {
        fragment.appendChild(createRailEventCard(ev));
      });
    }

    elRailEventsList.appendChild(fragment);
  }

  function createRailEventCard(ev) {
    const card = document.createElement('div');
    card.className = 'rail-event-card ' + getEventCardColorClass(ev);
    makeInteractive(card, () => openEventDetailSheet(ev), ev.title);

    const start = new Date(ev.start_at);
    const timeText = ev.all_day ? 'All Day' : `${formatTimeShort(start)} – ${formatTimeShort(new Date(ev.end_at))}`;

    const isBirthday = (ev.title || '').toLowerCase().includes('birthday');
    const icon = isBirthday ? '🎂 ' : '';
    const timeRow = document.createElement('div');
    timeRow.className = 'rail-event-time-row';
    timeRow.textContent = timeText;
    const title = document.createElement('div');
    title.className = 'rail-event-title';
    title.textContent = `${icon}${ev.title || 'Untitled event'}`;
    card.append(timeRow, title);
    if (ev.location) {
      const location = document.createElement('div');
      location.className = 'rail-event-meta';
      location.textContent = `📍 ${ev.location}`;
      card.appendChild(location);
    }
    return card;
  }

  // --- Day Drawer ---
  function setupDayDrawer() {
    document.getElementById('btn-close-drawer')?.addEventListener('click', closeDayDrawer);
    elDayDrawerOverlay?.addEventListener('click', (e) => {
      if (e.target === elDayDrawerOverlay) closeDayDrawer();
    });

    elBtnDrawerAddEvent?.addEventListener('click', () => {
      closeDayDrawer();
      openQuickAddModal(drawerSelectedDate);
    });
  }

  function openDayDrawer(date, events) {
    drawerSelectedDate = date;
    if (elDrawerDateTitle) {
      elDrawerDateTitle.textContent = date.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
    }

    if (elDrawerEventsList) {
      elDrawerEventsList.innerHTML = '';
      if (!events || events.length === 0) {
        elDrawerEventsList.appendChild(createEmptyState('📅', 'No events scheduled', 'This day is wide open.'));
      } else {
        events.forEach(ev => {
          elDrawerEventsList.appendChild(createRailEventCard(ev));
        });
      }
    }

    if (elDayDrawerOverlay) {
      previouslyFocusedElement = document.activeElement;
      elDayDrawerOverlay.style.display = 'flex';
      elDayDrawerOverlay.setAttribute('aria-hidden', 'false');
      document.getElementById('btn-close-drawer')?.focus();
    }
  }

  function closeDayDrawer() {
    if (elDayDrawerOverlay) {
      elDayDrawerOverlay.style.display = 'none';
      elDayDrawerOverlay.setAttribute('aria-hidden', 'true');
      restorePreviousFocus();
    }
  }

  // --- Event Detail Sheet & Undo Flow ---
  function setupEventDetailModal() {
    document.getElementById('btn-close-detail')?.addEventListener('click', closeEventDetailSheet);
    document.getElementById('btn-detail-ok')?.addEventListener('click', closeEventDetailSheet);

    const btnDelete = document.getElementById('btn-detail-delete');
    const deleteBox = document.getElementById('detail-delete-confirm-box');
    const normalActions = document.getElementById('detail-normal-actions');

    btnDelete?.addEventListener('click', () => {
      if (deleteBox) deleteBox.style.display = 'block';
    });

    document.getElementById('btn-cancel-delete')?.addEventListener('click', () => {
      if (deleteBox) deleteBox.style.display = 'none';
    });

    document.getElementById('btn-confirm-delete')?.addEventListener('click', async () => {
      const eventID = btnDelete.dataset.eventId;
      if (eventID) {
        await executeDelete(eventID);
      }
    });

    document.getElementById('btn-detail-edit')?.addEventListener('click', () => {
      const ev = btnDelete.eventObject;
      closeEventDetailSheet();
      if (ev) openEditEventModal(ev);
    });

    document.getElementById('btn-detail-quick-edit')?.addEventListener('click', () => {
      const ev = btnDelete.eventObject;
      closeEventDetailSheet();
      if (ev) openEditEventModal(ev);
    });

    elToastUndoBtn?.addEventListener('click', async () => {
      if (lastDeletedEvent && lastDeletedEvent.id) {
        await executeUndo(lastDeletedEvent.id);
      }
    });

    elEventDetailModal?.addEventListener('click', (event) => {
      if (event.target === elEventDetailModal) closeEventDetailSheet();
    });
  }

  function openEventDetailSheet(ev) {
    const accentStrip = document.getElementById('detail-accent-strip');
    if (accentStrip) {
      accentStrip.style.backgroundColor = getCalendarColor(ev.calendar_id);
    }

    const titleEl = document.getElementById('detail-title');
    const calBadge = document.getElementById('detail-cal-badge');
    const datetimeEl = document.getElementById('detail-datetime');
    const locRow = document.getElementById('detail-location-row');
    const locEl = document.getElementById('detail-location');
    const descRow = document.getElementById('detail-description-row');
    const descEl = document.getElementById('detail-description');
    const btnDelete = document.getElementById('btn-detail-delete');
    const deleteBox = document.getElementById('detail-delete-confirm-box');

    if (deleteBox) deleteBox.style.display = 'none';

    const isBirthday = (ev.title || '').toLowerCase().includes('birthday');
    if (titleEl) titleEl.textContent = (isBirthday ? '🎂 ' : '') + ev.title;

    const start = new Date(ev.start_at);
    const end = new Date(ev.end_at);
    if (datetimeEl) {
      const datePart = start.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' });
      const timePart = ev.all_day ? 'All-day' : `${formatTimeShort(start)} – ${formatTimeShort(end)}`;
      datetimeEl.innerHTML = `📅 <span>${datePart} · ${timePart}</span>`;
    }

    if (calBadge) {
      const calName = getCalendarName(ev.calendar_id);
      const calObj = displayCalendars.find(c => c.id === ev.calendar_id);
      const prov = (ev.provider || (calObj ? calObj.provider : '')).toLowerCase();
      let provLabel = '';
      if (prov === 'apple') provLabel = ' · Apple iCloud';
      else if (prov === 'microsoft') provLabel = ' · Exchange/365';
      else if (prov === 'google') provLabel = ' · Google Workspace';
      calBadge.textContent = calName + provLabel;
      calBadge.style.backgroundColor = getCalendarColor(ev.calendar_id);
      calBadge.style.color = '#ffffff';
    }

    if (ev.location) {
      if (locRow) locRow.style.display = 'flex';
      if (locEl) locEl.textContent = ev.location;
    } else {
      if (locRow) locRow.style.display = 'none';
    }

    // Meeting link detection (Google Meet, Microsoft Teams, Apple FaceTime, Zoom)
    const meetingRow = document.getElementById('detail-meeting-row');
    const meetingBtn = document.getElementById('detail-meeting-btn');
    const meetingIcon = document.getElementById('detail-meeting-icon');
    const meetingLabel = document.getElementById('detail-meeting-label');
    if (meetingRow && meetingBtn) {
      const combinedText = ((ev.description || '') + ' ' + (ev.location || '')).trim();
      let meetUrl = null;
      let meetType = null;

      const meetGoogleMatch = combinedText.match(/https:\/\/meet\.google\.com\/[a-z0-9-]+/i);
      const meetTeamsMatch = combinedText.match(/https:\/\/(teams\.microsoft\.com|teams\.live\.com)\/[^\s<>"']+/i);
      const meetFaceTimeMatch = combinedText.match(/https:\/\/facetime\.apple\.com\/[^\s<>"']+/i);
      const meetZoomMatch = combinedText.match(/https:\/\/[a-z0-9-]+\.zoom\.us\/[^\s<>"']+/i);

      if (meetGoogleMatch) {
        meetUrl = meetGoogleMatch[0];
        meetType = 'google';
      } else if (meetTeamsMatch) {
        meetUrl = meetTeamsMatch[0];
        meetType = 'teams';
      } else if (meetFaceTimeMatch) {
        meetUrl = meetFaceTimeMatch[0];
        meetType = 'apple';
      } else if (meetZoomMatch) {
        meetUrl = meetZoomMatch[0];
        meetType = 'zoom';
      }

      if (meetUrl) {
        meetingRow.style.display = 'flex';
        meetingBtn.href = meetUrl;
        meetingBtn.className = 'btn-meeting-link btn-meet-' + meetType;
        if (meetType === 'google') {
          if (meetingIcon) meetingIcon.textContent = '📹';
          if (meetingLabel) meetingLabel.textContent = 'Join Google Meet';
        } else if (meetType === 'teams') {
          if (meetingIcon) meetingIcon.textContent = '👥';
          if (meetingLabel) meetingLabel.textContent = 'Join Microsoft Teams';
        } else if (meetType === 'apple') {
          if (meetingIcon) meetingIcon.textContent = '📞';
          if (meetingLabel) meetingLabel.textContent = 'Join FaceTime Call';
        } else {
          if (meetingIcon) meetingIcon.textContent = '🎥';
          if (meetingLabel) meetingLabel.textContent = 'Join Zoom Meeting';
        }
      } else {
        meetingRow.style.display = 'none';
      }
    }

    if (ev.description) {
      if (descRow) descRow.style.display = 'block';
      if (descEl) descEl.textContent = ev.description;
    } else {
      if (descRow) descRow.style.display = 'none';
    }

    if (btnDelete) {
      btnDelete.dataset.eventId = ev.id;
      btnDelete.eventObject = ev;
    }

    if (elEventDetailModal) {
      previouslyFocusedElement = document.activeElement;
      elEventDetailModal.style.display = 'flex';
      elEventDetailModal.setAttribute('aria-hidden', 'false');
      document.getElementById('btn-detail-ok')?.focus();
    }
  }

  function closeEventDetailSheet() {
    if (elEventDetailModal) {
      elEventDetailModal.style.display = 'none';
      elEventDetailModal.setAttribute('aria-hidden', 'true');
      restorePreviousFocus();
    }
  }

  async function executeDelete(eventID) {
    try {
      const resp = await fetch(`/api/events/${encodeURIComponent(eventID)}`, { method: 'DELETE' });
      if (resp.ok) {
        const res = await resp.json();
        lastDeletedEvent = res.event || { id: eventID };
        closeEventDetailSheet();
        renderView();
        showToast('Event deleted', true);
      }
    } catch (e) {
      console.warn('Delete failed:', e);
      closeEventDetailSheet();
    }
  }

  async function executeUndo(eventID) {
    try {
      const resp = await fetch(`/api/events/${encodeURIComponent(eventID)}/restore`, { method: 'POST' });
      if (resp.ok) {
        lastDeletedEvent = null;
        hideToast();
        renderView();
        showToast('Event restored ✓', false);
      }
    } catch (e) {
      console.warn('Restore failed:', e);
    }
  }

  function showToast(msg, showUndo) {
    if (!elToastContainer || !elToastMessage) return;
    elToastMessage.textContent = msg;
    if (elToastUndoBtn) elToastUndoBtn.style.display = showUndo ? 'inline-block' : 'none';
    elToastContainer.style.display = 'block';

    if (undoTimeout) clearTimeout(undoTimeout);
    undoTimeout = setTimeout(hideToast, showUndo ? 5000 : 1800);
  }

  function hideToast() {
    if (elToastContainer) elToastContainer.style.display = 'none';
  }

  // --- Quick Add Modal Flow ---
  function setupQuickAddModal() {
    document.getElementById('btn-cancel-modal')?.addEventListener('click', closeQuickAddModal);
    elEventModal?.addEventListener('click', (event) => {
      if (event.target === elEventModal) closeQuickAddModal();
    });

    // Tabs
    const tabQuick = document.getElementById('tab-quick-tap');
    const tabNL = document.getElementById('tab-natural-lang');
    const modeQuick = document.getElementById('mode-quick-tap');
    const modeNL = document.getElementById('mode-natural-lang');

    tabQuick?.addEventListener('click', () => {
      tabQuick.classList.add('active');
      tabNL.classList.remove('active');
      tabQuick.setAttribute('aria-selected', 'true');
      tabNL.setAttribute('aria-selected', 'false');
      modeQuick.style.display = 'block';
      modeNL.style.display = 'none';
    });

    tabNL?.addEventListener('click', () => {
      tabNL.classList.add('active');
      tabQuick.classList.remove('active');
      tabNL.setAttribute('aria-selected', 'true');
      tabQuick.setAttribute('aria-selected', 'false');
      modeNL.style.display = 'flex';
      modeQuick.style.display = 'none';
      document.getElementById('nl-input')?.focus();
    });

    // Suggestion chips
    document.querySelectorAll('.suggestion-chip').forEach(chip => {
      chip.addEventListener('click', () => {
        const titleInput = document.getElementById('event-title');
        if (titleInput) {
          titleInput.value = chip.dataset.title;
          if (chip.dataset.dur) setDuration(Number(chip.dataset.dur));
          if (chip.dataset.allday === 'true') {
            const allDay = document.getElementById('event-all-day');
            allDay.checked = true;
            toggleAllDayControls(true);
          }
        }
      });
    });

    document.getElementById('btn-clear-title')?.addEventListener('click', () => {
      const title = document.getElementById('event-title');
      title.value = '';
      title.focus();
    });

    document.getElementById('btn-time-minus-15')?.addEventListener('click', () => adjustStartTime(-15));
    document.getElementById('btn-time-plus-15')?.addEventListener('click', () => adjustStartTime(15));

    // Relative dates
    setupRelativeDates();

    // Time chips & Durations
    setupTimeAndDurations();

    // All-day toggle
    const allDayCb = document.getElementById('event-all-day');
    allDayCb?.addEventListener('change', (e) => {
      toggleAllDayControls(e.target.checked);
    });

    // More Options toggle
    const btnMore = document.getElementById('btn-toggle-more');
    const moreContainer = document.getElementById('more-options-container');
    btnMore?.addEventListener('click', () => {
      if (moreContainer) {
        const isHidden = !moreContainer.style.display || moreContainer.style.display === 'none';
        moreContainer.style.display = isHidden ? 'flex' : 'none';
        btnMore.innerHTML = isHidden ? '<span>− Less Options</span>' : '<span>+ More Options (Location, Notes, Repeat)</span>';
        btnMore.setAttribute('aria-expanded', String(isHidden));
      }
    });

    // Recurrence buttons
    document.querySelectorAll('#recurrence-row .dur-chip').forEach(btn => {
      btn.addEventListener('click', (e) => {
        document.querySelectorAll('#recurrence-row .dur-chip').forEach(b => b.classList.remove('selected'));
        e.currentTarget.classList.add('selected');
        document.getElementById('event-rrule').value = e.currentTarget.dataset.recur;
      });
    });

    // Natural Language input debouncing
    const nlInput = document.getElementById('nl-input');
    let nlTimer = null;
    nlInput?.addEventListener('input', () => {
      clearTimeout(nlTimer);
      nlTimer = setTimeout(handleNaturalLanguageParse, 300);
    });

    // Form submission
    elEventForm?.addEventListener('submit', async (e) => {
      e.preventDefault();
      await handleEventSave();
    });
  }

  function setupRelativeDates() {
    const row = document.getElementById('relative-dates-row');
    const customContainer = document.getElementById('custom-date-container');
    const startDateInput = document.getElementById('event-start-date');

    // Calculate upcoming Sat / Sun
    const now = new Date();
    const daysToSat = (6 - now.getDay() + 7) % 7 || 7;
    const daysToSun = (7 - now.getDay() + 7) % 7 || 7;

    const satDate = new Date(now);
    satDate.setDate(now.getDate() + daysToSat);
    const sunDate = new Date(now);
    sunDate.setDate(now.getDate() + daysToSun);

    const btnSat = document.getElementById('btn-rel-sat');
    if (btnSat) btnSat.textContent = `Sat (${satDate.toLocaleDateString([], { month: 'numeric', day: 'numeric' })})`;
    const btnSun = document.getElementById('btn-rel-sun');
    if (btnSun) btnSun.textContent = `Sun (${sunDate.toLocaleDateString([], { month: 'numeric', day: 'numeric' })})`;

    row?.querySelectorAll('.date-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        row.querySelectorAll('.date-btn').forEach(b => b.classList.remove('selected'));
        e.currentTarget.classList.add('selected');

        const rel = e.currentTarget.dataset.rel;
        let selectedDate = new Date();

        if (rel === 'today') {
          selectedDate = new Date();
          if (customContainer) customContainer.style.display = 'none';
        } else if (rel === 'tomorrow') {
          selectedDate.setDate(selectedDate.getDate() + 1);
          if (customContainer) customContainer.style.display = 'none';
        } else if (rel === 'sat') {
          selectedDate = satDate;
          if (customContainer) customContainer.style.display = 'none';
        } else if (rel === 'sun') {
          selectedDate = sunDate;
          if (customContainer) customContainer.style.display = 'none';
        } else if (rel === 'next-week') {
          selectedDate.setDate(selectedDate.getDate() + 7);
          if (customContainer) customContainer.style.display = 'none';
        } else if (rel === 'custom') {
          if (customContainer) customContainer.style.display = 'block';
          startDateInput?.focus();
          return;
        }

        if (startDateInput) {
          startDateInput.value = formatDateForInput(selectedDate);
        }
      });
    });
  }

  function setupTimeAndDurations() {
    document.querySelectorAll('#time-chips-grid .time-chip').forEach(btn => {
      btn.addEventListener('click', (e) => {
        document.querySelectorAll('#time-chips-grid .time-chip').forEach(b => b.classList.remove('selected'));
        e.currentTarget.classList.add('selected');
        document.getElementById('event-start-time').value = e.currentTarget.dataset.time;
        recalculateEndTime();
      });
    });

    document.querySelectorAll('#durations-row .dur-chip').forEach(btn => {
      btn.addEventListener('click', (e) => {
        document.querySelectorAll('#durations-row .dur-chip').forEach(b => b.classList.remove('selected'));
        e.currentTarget.classList.add('selected');
        document.getElementById('event-duration-min').value = e.currentTarget.dataset.dur;
        recalculateEndTime();
      });
    });
  }

  function setDuration(durMin) {
    document.getElementById('event-duration-min').value = durMin;
    document.querySelectorAll('#durations-row .dur-chip').forEach(b => {
      if (b.dataset.dur == durMin) b.classList.add('selected');
      else b.classList.remove('selected');
    });
    recalculateEndTime();
  }

  function recalculateEndTime() {
    const timeStr = document.getElementById('event-start-time').value || '17:00';
    const durMin = parseInt(document.getElementById('event-duration-min').value || '60', 10);

    const parts = timeStr.split(':');
    const h = parseInt(parts[0], 10);
    const m = parseInt(parts[1], 10);

    const totalMin = (h * 60) + m + durMin;
    const endH = Math.floor(totalMin / 60) % 24;
    const endM = totalMin % 60;

    const endTimeStr = `${String(endH).padStart(2, '0')}:${String(endM).padStart(2, '0')}`;
    document.getElementById('event-end-time').value = endTimeStr;
    updateTimeSummary();
  }

  function adjustStartTime(minutes) {
    const input = document.getElementById('event-start-time');
    const [hours, mins] = (input.value || '17:00').split(':').map(Number);
    const total = ((hours * 60) + mins + minutes + (24 * 60)) % (24 * 60);
    input.value = `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`;
    document.querySelectorAll('#time-chips-grid .time-chip').forEach(button => button.classList.toggle('selected', button.dataset.time === input.value));
    recalculateEndTime();
  }

  function updateTimeSummary() {
    const startValue = document.getElementById('event-start-time')?.value || '17:00';
    const endValue = document.getElementById('event-end-time')?.value || '18:00';
    const duration = Number(document.getElementById('event-duration-min')?.value || 60);
    const summary = document.getElementById('time-summary-banner');
    const readout = document.getElementById('stepper-time-readout');
    const startLabel = formatClockValue(startValue);
    const endLabel = formatClockValue(endValue);
    if (readout) readout.textContent = startLabel;
    if (summary) summary.textContent = `🕒 ${startLabel} – ${endLabel} (${formatDuration(duration)})`;
  }

  function formatClockValue(value) {
    const [hours, minutes] = value.split(':').map(Number);
    return formatTimeShort(new Date(2000, 0, 1, hours, minutes));
  }

  function formatDuration(minutes) {
    if (minutes < 60) return `${minutes} min`;
    const hours = minutes / 60;
    return Number.isInteger(hours) ? `${hours} hr${hours === 1 ? '' : 's'}` : `${hours.toFixed(1)} hrs`;
  }

  function toggleAllDayControls(isAllDay) {
    const container = document.getElementById('time-controls-container');
    if (container) {
      container.style.opacity = isAllDay ? '0.4' : '1';
      container.style.pointerEvents = isAllDay ? 'none' : 'auto';
      container.setAttribute('aria-disabled', String(isAllDay));
    }
  }

  function openQuickAddModal(date, initialTimeStr) {
    previouslyFocusedElement = document.activeElement;
    document.getElementById('event-id').value = '';
    document.getElementById('event-title').value = '';
    document.getElementById('event-location').value = '';
    document.getElementById('event-description').value = '';
    document.getElementById('nl-input').value = '';

    const btnSave = document.getElementById('btn-save-event');
    if (btnSave) {
      btnSave.disabled = false;
      btnSave.textContent = 'Add Event';
    }

    const previewCard = document.getElementById('nl-preview-card');
    if (previewCard) previewCard.style.display = 'none';

    const moreContainer = document.getElementById('more-options-container');
    const moreButton = document.getElementById('btn-toggle-more');
    if (moreContainer) moreContainer.style.display = 'none';
    if (moreButton) {
      moreButton.innerHTML = '<span>+ More Options (Location, Notes, Repeat)</span>';
      moreButton.setAttribute('aria-expanded', 'false');
    }
    document.getElementById('event-rrule').value = '';
    document.querySelectorAll('#recurrence-row .dur-chip').forEach(button => {
      button.classList.toggle('selected', button.dataset.recur === '');
    });

    // Set date
    const d = date || new Date();
    document.getElementById('event-start-date').value = formatDateForInput(d);

    // Reset relative date button state
    document.querySelectorAll('#relative-dates-row .date-btn').forEach(b => b.classList.remove('selected'));
    const todayStr = formatDateForInput(new Date());
    const targetStr = formatDateForInput(d);
    document.getElementById('custom-date-container').style.display = 'none';
    if (targetStr === todayStr) {
      document.querySelector('#relative-dates-row [data-rel="today"]')?.classList.add('selected');
    } else {
      document.querySelector('#relative-dates-row [data-rel="custom"]')?.classList.add('selected');
      document.getElementById('custom-date-container').style.display = 'block';
    }

    // Default time (or initialTimeStr passed from clicked hour slot)
    const timeToUse = initialTimeStr || '17:00';
    document.getElementById('event-start-time').value = timeToUse;
    document.getElementById('event-duration-min').value = '60';
    document.querySelectorAll('#time-chips-grid .time-chip').forEach(button => button.classList.toggle('selected', button.dataset.time === timeToUse));
    document.querySelectorAll('#durations-row .dur-chip').forEach(button => button.classList.toggle('selected', button.dataset.dur === '60'));
    document.getElementById('event-all-day').checked = false;
    toggleAllDayControls(false);
    recalculateEndTime();

    // Reset tabs
    document.getElementById('tab-quick-tap')?.click();

    if (elEventModal) {
      elEventModal.style.display = 'flex';
      elEventModal.setAttribute('aria-hidden', 'false');
      window.setTimeout(() => document.getElementById('event-title')?.focus(), 0);
    }
  }

  function openEditEventModal(ev) {
    openQuickAddModal(new Date(ev.start_at));

    document.getElementById('event-id').value = ev.id;
    document.getElementById('event-title').value = ev.title;
    document.getElementById('event-location').value = ev.location || '';
    document.getElementById('event-description').value = ev.description || '';

    const start = new Date(ev.start_at);
    const end = new Date(ev.end_at);

    document.getElementById('event-start-date').value = formatDateForInput(start);
    document.getElementById('event-start-time').value = start.toTimeString().substring(0, 5);
    document.getElementById('event-end-time').value = end.toTimeString().substring(0, 5);
    const durationMinutes = Math.max(30, Math.round((end - start) / 60000));
    document.getElementById('event-duration-min').value = durationMinutes;
    document.querySelectorAll('#time-chips-grid .time-chip').forEach(button => button.classList.toggle('selected', button.dataset.time === start.toTimeString().substring(0, 5)));
    document.querySelectorAll('#durations-row .dur-chip').forEach(button => button.classList.toggle('selected', Number(button.dataset.dur) === durationMinutes));

    const recurrenceRule = ev.recurrence_rule || '';
    document.getElementById('event-rrule').value = recurrenceRule;
    document.querySelectorAll('#recurrence-row .dur-chip').forEach(button => button.classList.toggle('selected', button.dataset.recur === recurrenceRule));
    if (ev.location || ev.description || recurrenceRule) {
      document.getElementById('more-options-container').style.display = 'flex';
      document.getElementById('btn-toggle-more').innerHTML = '<span>− Less Options</span>';
      document.getElementById('btn-toggle-more').setAttribute('aria-expanded', 'true');
    }

    if (ev.all_day) {
      document.getElementById('event-all-day').checked = true;
      toggleAllDayControls(true);
    }

    selectCalendarDestination(ev.calendar_id);

    const btnSave = document.getElementById('btn-save-event');
    if (btnSave) btnSave.textContent = 'Save Changes';
  }

  function closeQuickAddModal() {
    if (elEventModal) {
      elEventModal.style.display = 'none';
      elEventModal.setAttribute('aria-hidden', 'true');
    }
    hideVirtualKeyboard();
    restorePreviousFocus();
  }

  async function handleNaturalLanguageParse() {
    const text = document.getElementById('nl-input')?.value?.trim();
    const card = document.getElementById('nl-preview-card');
    if (!text || text.length < 3) {
      if (card) card.style.display = 'none';
      return;
    }

    const sequence = ++naturalLanguageSequence;
    if (card) {
      card.style.display = 'flex';
      document.getElementById('nl-parsed-title').textContent = 'Understanding your event…';
      document.getElementById('nl-parsed-datetime').textContent = '';
      document.getElementById('nl-parsed-location').style.display = 'none';
    }

    try {
      const resp = await fetch('/api/kiosk/parse-natural', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: text })
      });

      if (sequence !== naturalLanguageSequence) return;
      if (resp.ok) {
        const data = await resp.json();
        if (data.title && card) {
          document.getElementById('nl-parsed-title').textContent = data.title;
          const start = new Date(data.start_at);
          const end = new Date(data.end_at);
          const timeStr = data.all_day ? 'All-day' : `${formatTimeShort(start)} – ${formatTimeShort(end)}`;
          document.getElementById('nl-parsed-datetime').textContent = `📅 ${start.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })} · ${timeStr}`;

          const locEl = document.getElementById('nl-parsed-location');
          if (data.location) {
            locEl.style.display = 'flex';
            locEl.textContent = `📍 ${data.location}`;
          } else {
            locEl.style.display = 'none';
          }

          card.style.display = 'flex';
        }
      } else {
        throw new Error(`Parse request failed (${resp.status})`);
      }
    } catch (e) {
      console.warn('NL parse error:', e);
      if (sequence === naturalLanguageSequence && card) {
        document.getElementById('nl-parsed-title').textContent = 'Could not understand that yet';
        document.getElementById('nl-parsed-datetime').textContent = 'Try including a day and time.';
      }
    }
  }

  let isSavingEvent = false;

  async function handleEventSave() {
    if (isSavingEvent) return;
    isSavingEvent = true;

    const btnSave = document.getElementById('btn-save-event');
    if (btnSave) {
      btnSave.disabled = true;
      btnSave.textContent = 'Saving...';
    }

    const isNLTab = document.getElementById('tab-natural-lang')?.classList.contains('active');
    let title, startDateStr, startTimeStr, endTimeStr, allDay, location, description, calendarID, rrule;

    try {
      if (isNLTab) {
        const parsedTitle = document.getElementById('nl-parsed-title')?.textContent;
        if (!parsedTitle || parsedTitle === '--') {
          showToast('Add a clearer day and time', false);
          return;
        }
        title = parsedTitle;
        const text = document.getElementById('nl-input').value;

        // Quick parse call to ensure latest values
        const parseResp = await fetch('/api/kiosk/parse-natural', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text: text })
        });
        if (!parseResp.ok) throw new Error('Could not parse event description');
        const parsed = await parseResp.json();

        const start = new Date(parsed.start_at);
        const end = new Date(parsed.end_at);
        startDateStr = formatDateForInput(start);
        startTimeStr = start.toTimeString().substring(0, 5);
        endTimeStr = end.toTimeString().substring(0, 5);
        allDay = parsed.all_day;
        location = parsed.location || '';
        description = '';
        calendarID = document.getElementById('event-calendar-id').value;
        rrule = '';
      } else {
        title = document.getElementById('event-title').value.trim();
        startDateStr = document.getElementById('event-start-date').value;
        startTimeStr = document.getElementById('event-start-time').value || '17:00';
        endTimeStr = document.getElementById('event-end-time').value || '18:00';
        allDay = document.getElementById('event-all-day').checked;
        location = document.getElementById('event-location').value.trim();
        description = document.getElementById('event-description').value.trim();
        calendarID = document.getElementById('event-calendar-id').value;
        rrule = document.getElementById('event-rrule').value;
      }

      if (!title) {
        showToast('Add a title for the event', false);
        return;
      }

      if (!calendarID) {
        showToast('Choose a calendar first', false);
        return;
      }

      let startAt;
      let endAt;
      if (allDay) {
        startAt = new Date(`${startDateStr}T00:00:00`);
        endAt = new Date(startAt);
        endAt.setDate(endAt.getDate() + 1);
      } else {
        startAt = new Date(`${startDateStr}T${startTimeStr}:00`);
        endAt = new Date(`${startDateStr}T${endTimeStr}:00`);
        if (endAt <= startAt) endAt.setDate(endAt.getDate() + 1);
      }

      if (Number.isNaN(startAt.getTime()) || Number.isNaN(endAt.getTime())) {
        showToast('Choose a valid date and time', false);
        return;
      }

      const id = document.getElementById('event-id').value;
      const payload = {
        id: id || undefined,
        calendar_id: calendarID,
        title: title,
        location: location,
        description: description,
        start_at: startAt.toISOString(),
        end_at: endAt.toISOString(),
        all_day: allDay,
        recurrence_rule: rrule
      };

      const method = id ? 'PUT' : 'POST';
      const url = id ? `/api/events/${encodeURIComponent(id)}` : '/api/events';

      const resp = await fetch(url, {
        method: method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (resp.ok) {
        closeQuickAddModal();
        renderView();
        showToast(id ? 'Saved ✓' : 'Added ✓', false);
      } else {
        const err = await resp.json().catch(() => ({}));
        showToast(err.error || 'Could not save the event', false);
      }
    } catch (e) {
      console.warn('Network save exception:', e);
      showToast('Could not reach FamilyBoard', false);
    } finally {
      isSavingEvent = false;
      if (btnSave) {
        btnSave.disabled = false;
        btnSave.textContent = document.getElementById('event-id').value ? 'Save Changes' : 'Add Event';
      }
    }
  }

  // --- On-Screen Virtual Touch Keyboard ---
  function setupVirtualKeyboard() {
    document.getElementById('btn-hide-keyboard')?.addEventListener('click', hideVirtualKeyboard);

    const inputs = ['event-title', 'event-location', 'event-description', 'nl-input'];
    inputs.forEach(id => {
      const el = document.getElementById(id);
      el?.addEventListener('focus', () => {
        activeInput = el;
        showVirtualKeyboard();
      });
    });

    renderKeyboardKeys();
  }

  function renderKeyboardKeys() {
    if (!elKeyboardRows) return;
    elKeyboardRows.innerHTML = '';

    const normalRows = [
      ['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'],
      ['q', 'w', 'e', 'r', 't', 'y', 'u', 'i', 'o', 'p'],
      ['a', 's', 'd', 'f', 'g', 'h', 'j', 'k', 'l'],
      ['Shift', 'z', 'x', 'c', 'v', 'b', 'n', 'm', '⌫'],
      ['?123', '.', '-', 'Space', 'Clear', 'Done']
    ];

    const symbolRows = [
      ['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'],
      ['@', '#', '$', '%', '&', '*', '+', '(', ')', '/'],
      ['ABC', ':', ';', '!', '?', '\'', '"', '⌫'],
      ['Space', 'Clear', 'Done']
    ];

    const currentLayout = isSymbolActive ? symbolRows : normalRows;

    currentLayout.forEach(rowKeys => {
      const rowDiv = document.createElement('div');
      rowDiv.className = 'keyboard-row';

      rowKeys.forEach(k => {
        const keyBtn = document.createElement('button');
        keyBtn.type = 'button';
        keyBtn.className = 'key-btn';

        let displayKey = k;
        if (!isSymbolActive && isShiftActive && k.length === 1) {
          displayKey = k.toUpperCase();
        }

        keyBtn.textContent = displayKey;

        if (/^[0-9]$/.test(k)) keyBtn.classList.add('number-key');
        if (k === 'Space') keyBtn.classList.add('space-key');
        else if (k === 'Done') keyBtn.classList.add('done-key');
        else if (['Shift', '⌫', '?123', 'ABC', 'Clear'].includes(k)) keyBtn.classList.add('special-key');

        keyBtn.addEventListener('click', (e) => {
          e.preventDefault();
          handleKeyPress(k);
        });

        rowDiv.appendChild(keyBtn);
      });

      elKeyboardRows.appendChild(rowDiv);
    });
  }

  function handleKeyPress(k) {
    if (!activeInput) return;

    if (k === 'Shift') {
      isShiftActive = !isShiftActive;
      renderKeyboardKeys();
      return;
    }
    if (k === '?123') {
      isSymbolActive = true;
      renderKeyboardKeys();
      return;
    }
    if (k === 'ABC') {
      isSymbolActive = false;
      renderKeyboardKeys();
      return;
    }
    if (k === 'Done') {
      hideVirtualKeyboard();
      return;
    }
    if (k === 'Clear') {
      activeInput.value = '';
      activeInput.dispatchEvent(new Event('input'));
      return;
    }
    if (k === '⌫') {
      activeInput.value = activeInput.value.slice(0, -1);
      activeInput.dispatchEvent(new Event('input'));
      return;
    }

    let charToAdd = k;
    if (k === 'Space') charToAdd = ' ';
    else if (!isSymbolActive && isShiftActive) charToAdd = k.toUpperCase();

    activeInput.value += charToAdd;
    activeInput.dispatchEvent(new Event('input'));
  }

  function isMobileDevice() {
    return window.innerWidth <= 768 || /Mobi|Android|iPhone|iPad|iPod/i.test(navigator.userAgent);
  }

  function triggerHaptic(duration) {
    if ('vibrate' in navigator) {
      try { navigator.vibrate(duration || 15); } catch (_) {}
    }
  }

  let usingPhysicalKeyboard = false;

  function showVirtualKeyboard() {
    // Suppress physical touchscreen keyboard on mobile smartphones (iPhone & Android)
    if (isMobileDevice()) return;
    // Suppress touchscreen keyboard if user is typing on a physical wireless or USB keyboard
    if (usingPhysicalKeyboard) return;
    if (elVirtualKeyboard) elVirtualKeyboard.style.display = 'flex';
  }

  function hideVirtualKeyboard() {
    if (elVirtualKeyboard) elVirtualKeyboard.style.display = 'none';
  }

  // --- Real-Time Flawless Synchronization Engine ---
  let sseEventSource = null;
  let sseReconnectTimer = null;
  let sseRetryDelay = 1000;
  const MAX_RETRY_DELAY = 10000;

  function setupSSE() {
    if (sseReconnectTimer) {
      clearTimeout(sseReconnectTimer);
      sseReconnectTimer = null;
    }
    if (sseEventSource) {
      try { sseEventSource.close(); } catch (_) {}
      sseEventSource = null;
    }

    try {
      sseEventSource = new EventSource('/api/events/stream');

      sseEventSource.onopen = function() {
        sseRetryDelay = 1000; // Reset backoff upon successful connection
        updateStatus('', 'Synced');
      };

      sseEventSource.onmessage = function(e) {
        if (e.data === 'reload') {
          triggerHaptic(10);
          updateStatus('syncing', 'Updating…');
          renderView().finally(() => {
            updateStatus('', 'Synced');
          });
        }
      };

      sseEventSource.onerror = function() {
        if (sseEventSource) {
          try { sseEventSource.close(); } catch (_) {}
          sseEventSource = null;
        }

        if (navigator.onLine) {
          updateStatus('syncing', 'Syncing…');
        } else {
          updateStatus('offline', 'Offline · cached');
        }

        // Exponential backoff with random jitter to prevent reconnect stampedes
        const jitter = Math.floor(Math.random() * 500);
        const delay = Math.min(sseRetryDelay + jitter, MAX_RETRY_DELAY);
        sseRetryDelay = Math.min(sseRetryDelay * 2, MAX_RETRY_DELAY);

        sseReconnectTimer = setTimeout(setupSSE, delay);
      };
    } catch (e) {
      console.warn('SSE connection initialization error:', e);
      sseReconnectTimer = setTimeout(setupSSE, 5000);
    }
  }

  // Mobile Lifecycle & Reconnection Triggers
  function setupMobileSyncTriggers() {
    // When phone screen unlocks or user switches back to FamilyBoard tab
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        if (!sseEventSource || sseEventSource.readyState === EventSource.CLOSED) {
          setupSSE();
        }
        renderView();
      }
    });

    window.addEventListener('pageshow', () => {
      if (!sseEventSource || sseEventSource.readyState === EventSource.CLOSED) {
        setupSSE();
      }
      renderView();
    });

    window.addEventListener('focus', () => {
      if (!sseEventSource || sseEventSource.readyState === EventSource.CLOSED) {
        setupSSE();
      }
    });

    // Register Service Worker for mobile PWA offline cache
    if ('serviceWorker' in navigator) {
      window.addEventListener('load', () => {
        navigator.serviceWorker.register('/sw.js', { scope: '/' })
          .then(() => {
            console.log('[PWA] Service Worker registered for offline resilience');
          })
          .catch(err => {
            console.log('[PWA] Service Worker registration omitted:', err);
          });
      });
    }
  }

  // Mobile Phone Pairing Modal (iPhone & Poco F7)
  function setupMobilePairModal() {
    const btnPair = document.getElementById('btn-pair-phone');
    const modal = document.getElementById('modal-mobile-pair');
    const btnClose = document.getElementById('btn-close-pair-modal');
    const btnCloseFooter = document.getElementById('btn-close-pair-modal-footer');
    const textUrl = document.getElementById('text-pair-url');
    const btnCopy = document.getElementById('btn-copy-pair-url');
    const imgQr = document.getElementById('img-pair-qrcode');

    if (!btnPair || !modal) return;

    btnPair.addEventListener('click', async () => {
      triggerHaptic(15);
      modal.classList.remove('hidden');
      modal.setAttribute('aria-hidden', 'false');

      try {
        const resp = await fetch('/api/system/mobile-pair');
        if (resp.ok) {
          const data = await resp.json();
          if (textUrl && data.pair_url) {
            textUrl.textContent = data.pair_url;
            textUrl.dataset.url = data.pair_url;
          }
          if (imgQr) {
            imgQr.src = (data.qr_url || '/api/system/mobile-pair/qrcode') + '?t=' + Date.now();
          }
        }
      } catch (_) {
        if (textUrl) textUrl.textContent = window.location.origin + '/';
      }
    });

    function closePairModal() {
      closeMobilePairModal();
    }

    if (btnClose) btnClose.onclick = closeMobilePairModal;
    if (btnCloseFooter) btnCloseFooter.onclick = closeMobilePairModal;
    modal.onclick = (e) => {
      if (e.target === modal) closeMobilePairModal();
    };

    if (btnCopy) {
      btnCopy.addEventListener('click', async () => {
        triggerHaptic(10);
        const urlToCopy = textUrl?.dataset?.url || textUrl?.textContent || window.location.href;
        try {
          await navigator.clipboard.writeText(urlToCopy);
          btnCopy.textContent = 'Copied ✓';
          setTimeout(() => { btnCopy.textContent = 'Copy'; }, 2000);
        } catch (_) {
          btnCopy.textContent = 'Copied ✓';
        }
      });
    }
  }

  function closeMobilePairModal() {
    if (!elMobilePairModal) return;
    elMobilePairModal.classList.add('hidden');
    elMobilePairModal.style.display = 'none';
    elMobilePairModal.setAttribute('aria-hidden', 'true');
    restorePreviousFocus();
  }

  // --- Keyboard Shortcuts Guide Modal ---
  function setupShortcutsHelpModal() {
    const btnShortcuts = document.getElementById('btn-shortcuts-help');
    const btnClose = document.getElementById('btn-close-shortcuts-modal');
    const btnCloseFooter = document.getElementById('btn-close-shortcuts-footer');

    if (btnShortcuts) {
      btnShortcuts.addEventListener('click', () => {
        triggerHaptic(12);
        openShortcutsHelpModal();
      });
    }

    if (btnClose) btnClose.onclick = closeShortcutsHelpModal;
    if (btnCloseFooter) btnCloseFooter.onclick = closeShortcutsHelpModal;
    if (elShortcutsHelpModal) {
      elShortcutsHelpModal.addEventListener('click', (e) => {
        if (e.target === elShortcutsHelpModal) closeShortcutsHelpModal();
      });
    }
  }

  function openShortcutsHelpModal() {
    if (!elShortcutsHelpModal) return;
    previouslyFocusedElement = document.activeElement;
    elShortcutsHelpModal.classList.remove('hidden');
    elShortcutsHelpModal.style.display = 'flex';
    elShortcutsHelpModal.setAttribute('aria-hidden', 'false');
    const closeBtn = document.getElementById('btn-close-shortcuts-modal');
    if (closeBtn) closeBtn.focus();
  }

  function closeShortcutsHelpModal() {
    if (!elShortcutsHelpModal) return;
    elShortcutsHelpModal.classList.add('hidden');
    elShortcutsHelpModal.style.display = 'none';
    elShortcutsHelpModal.setAttribute('aria-hidden', 'true');
    restorePreviousFocus();
  }

  // --- Real-Time Week View "Now" Indicator Line ---
  function updateWeekNowIndicator() {
    if (currentView !== 'week') return;
    const indicator = document.getElementById('week-now-indicator-line');
    if (!indicator) return;
    const now = new Date();
    const currentHour = now.getHours() + (now.getMinutes() / 60);
    if (currentHour >= 7 && currentHour <= 22) {
      const topPx = (currentHour - 7) * 60;
      indicator.style.top = `${topPx}px`;
      indicator.style.display = 'block';
    } else {
      indicator.style.display = 'none';
    }
  }

  // --- Touch Gesture Navigation (Left/Right Swipe) ---
  function setupTouchSwipeNavigation() {
    let touchStartX = 0;
    let touchStartY = 0;
    let touchStartTime = 0;

    window.addEventListener('touchstart', (e) => {
      if (e.touches.length !== 1) return;
      const touch = e.touches[0];
      touchStartX = touch.clientX;
      touchStartY = touch.clientY;
      touchStartTime = Date.now();
    }, { passive: true });

    window.addEventListener('touchend', (e) => {
      // Don't intercept swipe if any overlay modal or drawer is open
      if (getVisibleOverlay()) return;

      // Don't swipe if touching inside horizontal scroll containers or form controls
      const target = e.target;
      if (target && target.closest && target.closest(
        '#virtual-keyboard, .suggestions-cloud, .relative-dates-row, .time-chips-grid, .templates-scroll-row, .dest-tiles-grid, input, textarea, select'
      )) {
        return;
      }

      if (e.changedTouches.length !== 1) return;
      const touch = e.changedTouches[0];
      const deltaX = touch.clientX - touchStartX;
      const deltaY = touch.clientY - touchStartY;
      const elapsed = Date.now() - touchStartTime;

      // Swipe requirements: duration < 600ms, horizontal distance >= 60px, dominant horizontal axis
      if (elapsed < 600 && Math.abs(deltaX) >= 60 && Math.abs(deltaX) > 1.6 * Math.abs(deltaY)) {
        triggerHaptic(15);
        if (deltaX < 0) {
          // Swipe left -> advance to next period
          navigateCalendar(1);
        } else {
          // Swipe right -> go back to previous period
          navigateCalendar(-1);
        }
      }
    }, { passive: true });
  }

  // Mobile PWA Home Screen Banner
  function setupMobilePwaBanner() {
    const banner = document.getElementById('mobile-pwa-banner');
    const btnHow = document.getElementById('btn-pwa-install-how');
    const btnDismiss = document.getElementById('btn-pwa-dismiss');

    if (!banner) return;

    const isStandalone = window.matchMedia('(display-mode: standalone)').matches || ('standalone' in navigator && navigator.standalone);
    const isDismissed = localStorage.getItem('fb_pwa_dismissed') === 'true';

    // Show banner only on mobile devices if not already running full-screen PWA and not dismissed
    if (isMobileDevice() && !isStandalone && !isDismissed) {
      setTimeout(() => {
        banner.classList.remove('hidden');
      }, 2500);
    }

    if (btnHow) {
      btnHow.addEventListener('click', () => {
        triggerHaptic(12);
        banner.classList.add('hidden');
        const btnPair = document.getElementById('btn-pair-phone');
        if (btnPair) btnPair.click();
      });
    }

    if (btnDismiss) {
      btnDismiss.addEventListener('click', () => {
        banner.classList.add('hidden');
        localStorage.setItem('fb_pwa_dismissed', 'true');
      });
    }
  }

  // --- Utility Helpers ---
  function formatDateForInput(d) {
    const yyyy = d.getFullYear();
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const dd = String(d.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  }

  function formatTimeShort(d) {
    return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  }

  function formatClockValue(timeStr) {
    if (!timeStr) return '';
    const parts = timeStr.split(':');
    let h = parseInt(parts[0], 10);
    const m = parts[1] || '00';
    if (isNaN(h)) return timeStr;
    if (timeFormat === '24h') {
      return `${String(h).padStart(2, '0')}:${m}`;
    }
    const ampm = h >= 12 ? 'PM' : 'AM';
    h = h % 12;
    if (h === 0) h = 12;
    return m === '00' ? `${h} ${ampm}` : `${h}:${m} ${ampm}`;
  }

  function getCalendarName(calID) {
    const cal = displayCalendars.find(c => c.id === calID);
    return cal ? cal.name : 'Family';
  }

  function getCalendarColor(calID) {
    const cal = displayCalendars.find(c => c.id === calID);
    return cal ? (cal.display_color || 'var(--accent-primary)') : 'var(--accent-primary)';
  }

  function getEventColorClass(ev) {
    const title = (ev.title || '').toLowerCase();
    const cal = displayCalendars.find(c => c.id === ev.calendar_id);
    const calName = (cal ? cal.name : '').toLowerCase();
    const prov = (ev.provider || (cal ? cal.provider : '')).toLowerCase();
    if (calName.includes('home')) return 'pill-home';
    if (title.includes('birthday')) return 'pill-birthday';
    if (prov === 'apple') return 'pill-apple';
    if (prov === 'microsoft') return 'pill-microsoft';
    if (ev.calendar_id === 'cal-member-1' || title.includes('member-1') || title.includes('alex')) return 'pill-member-1';
    if (ev.calendar_id === 'cal-member-2' || title.includes('member-2') || title.includes('jordan')) return 'pill-member-2';
    if (title.includes('kids') || title.includes('soccer') || title.includes('swimming')) return 'pill-kids';
    return 'pill-family';
  }

  function getEventCardColorClass(ev) {
    const title = (ev.title || '').toLowerCase();
    const cal = displayCalendars.find(c => c.id === ev.calendar_id);
    const calName = (cal ? cal.name : '').toLowerCase();
    const prov = (ev.provider || (cal ? cal.provider : '')).toLowerCase();
    if (calName.includes('home')) return 'card-home';
    if (title.includes('birthday')) return 'card-birthday';
    if (prov === 'apple') return 'card-apple';
    if (prov === 'microsoft') return 'card-microsoft';
    if (ev.calendar_id === 'cal-member-1' || title.includes('member-1') || title.includes('alex')) return 'card-member-1';
    if (ev.calendar_id === 'cal-member-2' || title.includes('member-2') || title.includes('jordan')) return 'card-member-2';
    if (title.includes('kids') || title.includes('soccer') || title.includes('swimming')) return 'card-kids';
    return 'card-family';
  }

  function makeInteractive(element, action, label) {
    element.setAttribute('role', 'button');
    element.tabIndex = 0;
    if (label) element.setAttribute('aria-label', label);
    element.addEventListener('click', action);
    element.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        event.stopPropagation();
        action();
      }
    });
  }

  function createEmptyState(iconText, titleText, detailText) {
    const empty = document.createElement('div');
    empty.className = 'rail-empty-state';
    const icon = document.createElement('div');
    icon.className = 'rail-empty-icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.textContent = iconText;
    const title = document.createElement('strong');
    title.textContent = titleText;
    const detail = document.createElement('p');
    detail.className = 'empty-state-detail';
    detail.textContent = detailText;
    empty.append(icon, title, detail);
    return empty;
  }

  function restorePreviousFocus() {
    if (previouslyFocusedElement && document.contains(previouslyFocusedElement)) {
      previouslyFocusedElement.focus();
    }
    previouslyFocusedElement = null;
  }

  function getVisibleOverlay() {
    return [elShortcutsHelpModal, elEventDetailModal, elEventModal, elDayDrawerOverlay, elMobilePairModal, elWifiSetupModal].find(overlay =>
      overlay && overlay.getAttribute('aria-hidden') === 'false'
    );
  }

  function handleGlobalKeydown(event) {
    // When typing on a physical keyboard, auto-dismiss touchscreen keyboard
    if (event.isTrusted) {
      usingPhysicalKeyboard = true;
      hideVirtualKeyboard();
    }

    const overlay = getVisibleOverlay();
    if (!overlay) {
      // Don't intercept shortcuts if user is currently focused on an input element
      const activeEl = document.activeElement;
      const tag = (activeEl?.tagName || '').toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select') {
        if (event.key === 'Escape') {
          activeEl.blur();
        }
        return;
      }

      const key = event.key;
      // Kiosk Keyboard Shortcuts
      if (key === 'c' || key === 'C' || key === 'n' || key === 'N' || key === '+') {
        event.preventDefault();
        triggerHaptic(12);
        openQuickAddModal(new Date());
        return;
      }
      if (key === 't' || key === 'T') {
        event.preventDefault();
        triggerHaptic(10);
        currentDate = new Date();
        renderView();
        showToast('Jumped to Today 📅', false);
        return;
      }
      if (key === 'm' || key === 'M') {
        event.preventDefault();
        triggerHaptic(10);
        currentView = 'hybrid';
        syncViewButtons();
        renderView();
        showToast('Month (30-day) View', false);
        return;
      }
      if (key === 'w' || key === 'W') {
        event.preventDefault();
        triggerHaptic(10);
        currentView = 'week';
        syncViewButtons();
        renderView();
        showToast('Week View', false);
        return;
      }
      if (key === 's' || key === 'S') {
        event.preventDefault();
        triggerHaptic(10);
        currentView = 'schedule';
        syncViewButtons();
        renderView();
        showToast('Schedule View', false);
        return;
      }
      if (key === 'd' || key === 'D') {
        event.preventDefault();
        triggerHaptic(10);
        currentView = 'today';
        syncViewButtons();
        renderView();
        showToast('Day View', false);
        return;
      }
      if (key === 'ArrowLeft') {
        event.preventDefault();
        navigateCalendar(-1);
        return;
      }
      if (key === 'ArrowRight') {
        event.preventDefault();
        navigateCalendar(1);
        return;
      }
      if (key === '?' || (key === '/' && !event.shiftKey)) {
        event.preventDefault();
        triggerHaptic(12);
        openShortcutsHelpModal();
        return;
      }
      if (key === 'p' || key === 'P') {
        event.preventDefault();
        openFotoCubePhotoFrame();
        return;
      }
      return;
    }

    // Overlay IS open:
    if (event.key === 'Escape') {
      event.preventDefault();
      if (overlay === elShortcutsHelpModal) closeShortcutsHelpModal();
      else if (overlay === elEventDetailModal) closeEventDetailSheet();
      else if (overlay === elEventModal) closeQuickAddModal();
      else if (overlay === elDayDrawerOverlay) closeDayDrawer();
      else if (overlay === elMobilePairModal) closeMobilePairModal();
      else closeKioskWifiModal();
      return;
    }

    // Inside Quick Add Modal: Enter to submit
    if (overlay === elEventModal && event.key === 'Enter') {
      const activeEl = document.activeElement;
      // Allow Shift+Enter in description for newlines
      if (activeEl && activeEl.id === 'event-description' && event.shiftKey) {
        return;
      }
      event.preventDefault();
      handleEventSave();
      return;
    }

    // Inside Event Detail Modal: E to edit
    if (overlay === elEventDetailModal && (event.key === 'e' || event.key === 'E')) {
      const btnDelete = document.getElementById('btn-detail-delete');
      const ev = btnDelete?.eventObject;
      closeEventDetailSheet();
      if (ev) openEditEventModal(ev);
      return;
    }

    // Inside Day Drawer: C or N to add event for drawer's date
    if (overlay === elDayDrawerOverlay && (event.key === 'c' || event.key === 'C' || event.key === 'n' || event.key === 'N' || event.key === '+')) {
      event.preventDefault();
      closeDayDrawer();
      openQuickAddModal(drawerSelectedDate);
      return;
    }

    if (event.key !== 'Tab') return;
    const focusable = Array.from(overlay.querySelectorAll(
      'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'
    )).filter(element => element.offsetParent !== null);
    if (!focusable.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  // --- 15-Minute Inactivity Auto-Reset & 30-Day Default Mode ---
  // The kiosk defaults to the 30-day Month view whenever plugged in and touched within the last 15 minutes.
  // If left unattended for > 15 minutes, it automatically closes drawers/modals, resets to Today's 30-day view,
  // and applies a gentle ambient display dimming state until touched again.
  const INACTIVITY_TIMEOUT_MS = 15 * 60 * 1000; // 15 minutes
  let lastTouchTime = Date.now();
  let inactivityTimer = null;
  let isPluggedIn = true; // Default AC appliance

  function setupInactivityAutoReset() {
    // Check battery charging status if available
    if (navigator.getBattery) {
      navigator.getBattery().then(battery => {
        isPluggedIn = battery.charging;
        battery.addEventListener('chargingchange', () => {
          isPluggedIn = battery.charging;
          handleUserActivity();
        });
      }).catch(() => {
        isPluggedIn = true;
      });
    }

    // Register user touch / interaction events
    const activityEvents = ['touchstart', 'pointerdown', 'mousedown', 'keydown', 'click'];
    activityEvents.forEach(evt => {
      window.addEventListener(evt, handleUserActivity, { passive: true });
    });

    // Start inactivity watchdog
    resetInactivityTimer();
  }

  function handleUserActivity() {
    lastTouchTime = Date.now();

    // If currently in FotoCube photo frame screensaver, wake up immediately into calendar
    if (isPhotoFrameOpen) {
      closeFotoCubePhotoFrame();
      return;
    }

    // If currently dimmed/ambient, wake up instantly into 30-day view
    if (document.body.classList.contains('kiosk-ambient-dim')) {
      document.body.classList.remove('kiosk-ambient-dim');
      if (currentView !== 'hybrid') {
        resetToDefault30DayView();
      }
    }

    resetInactivityTimer();
  }

  function resetInactivityTimer() {
    if (inactivityTimer) {
      clearTimeout(inactivityTimer);
    }
    inactivityTimer = setTimeout(onInactivityElapsed, INACTIVITY_TIMEOUT_MS);
  }

  function resetToDefault30DayView() {
    // 1. Close any open overlays, modals, and sheets
    closeQuickAddModal();
    closeEventDetailSheet();
    closeDayDrawer();
    closeKioskWifiModal();
    hideVirtualKeyboard();

    // 2. Reset date to today
    currentDate = new Date();

    // 3. Reset view to 30-day (hybrid Month) view
    currentView = 'hybrid';
    document.querySelectorAll('.view-btn').forEach(btn => {
      const isActive = btn.dataset.view === 'hybrid';
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });

    // 4. Render fresh view
    renderView();
  }

  function onInactivityElapsed() {
    if (isMobileDevice()) return; // Don't hijack smartphone browser with kitchen screensaver
    const elapsed = Date.now() - lastTouchTime;
    if (elapsed >= INACTIVITY_TIMEOUT_MS) {
      // Return to default 30-day view
      resetToDefault30DayView();

      // Launch FotoCube ambient digital photo frame screensaver
      openFotoCubePhotoFrame();
    } else {
      // Reschedule for remaining time
      inactivityTimer = setTimeout(onInactivityElapsed, INACTIVITY_TIMEOUT_MS - elapsed);
    }
  }

  // --- FotoCube Ambient Photo Frame Mode ---
  let photoUrls = [];
  let currentPhotoIndex = 0;
  let photoCycleTimer = null;
  let isPhotoFrameOpen = false;

  const DEFAULT_AMBIENT_BACKGROUNDS = [
    'linear-gradient(135deg, #0f172a 0%, #1e1b4b 50%, #312e81 100%)',
    'linear-gradient(135deg, #064e3b 0%, #065f46 50%, #047857 100%)',
    'linear-gradient(135deg, #1e293b 0%, #334155 50%, #1e3a8a 100%)',
    'linear-gradient(135deg, #451a03 0%, #78350f 50%, #b45309 100%)'
  ];

  function setupFotoCubePhotoFrame() {
    const btnToggle = document.getElementById('btn-toggle-photo-frame');
    const elScreensaver = document.getElementById('photo-frame-screensaver');

    if (btnToggle) {
      btnToggle.addEventListener('click', (e) => {
        e.stopPropagation();
        openFotoCubePhotoFrame();
      });
    }

    if (elScreensaver) {
      elScreensaver.addEventListener('click', () => {
        closeFotoCubePhotoFrame();
      });
    }

    loadPhotosList();
  }

  async function loadPhotosList() {
    try {
      const resp = await fetch('/api/photos');
      if (resp.ok) {
        const data = await resp.json();
        if (data.photos && data.photos.length > 0) {
          photoUrls = data.photos;
        }
      }
    } catch (_) {}
  }

  function openFotoCubePhotoFrame() {
    const elScreensaver = document.getElementById('photo-frame-screensaver');
    if (!elScreensaver) return;

    isPhotoFrameOpen = true;
    elScreensaver.classList.remove('hidden');
    elScreensaver.setAttribute('aria-hidden', 'false');

    updatePhotoFrameHUD();
    renderNextPhoto();

    if (photoCycleTimer) clearInterval(photoCycleTimer);
    photoCycleTimer = setInterval(renderNextPhoto, 15000);
  }

  function closeFotoCubePhotoFrame() {
    const elScreensaver = document.getElementById('photo-frame-screensaver');
    if (!elScreensaver) return;

    isPhotoFrameOpen = false;
    elScreensaver.classList.add('hidden');
    elScreensaver.setAttribute('aria-hidden', 'true');

    if (photoCycleTimer) {
      clearInterval(photoCycleTimer);
      photoCycleTimer = null;
    }

    resetInactivityTimer();
  }

  function updatePhotoFrameHUD() {
    const now = new Date();
    const elTime = document.getElementById('photo-frame-time');
    const elDate = document.getElementById('photo-frame-date');

    if (elTime) {
      elTime.textContent = now.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }
    if (elDate) {
      elDate.textContent = now.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
    }

    // Next upcoming event card
    const elEventCard = document.getElementById('photo-frame-event-card');
    const elEventTitle = document.getElementById('photo-frame-event-title');
    const elEventTime = document.getElementById('photo-frame-event-time');

    if (elEventCard && elEventTitle && elEventTime) {
      const upcoming = (cachedEvents || [])
        .filter(ev => ev.status !== 'cancelled' && new Date(ev.end_at) >= now)
        .sort((a, b) => new Date(a.start_at) - new Date(b.start_at))[0];

      if (upcoming) {
        elEventCard.classList.remove('hidden');
        elEventTitle.textContent = upcoming.title;
        const evStart = new Date(upcoming.start_at);
        const timeLabel = upcoming.all_day ? 'All day' : evStart.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
        elEventTime.textContent = timeLabel + ' · ' + evStart.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });
      } else {
        elEventCard.classList.add('hidden');
      }
    }
  }

  function renderNextPhoto() {
    const elBg = document.getElementById('photo-frame-bg');
    if (!elBg) return;

    updatePhotoFrameHUD();

    if (photoUrls.length > 0) {
      const url = photoUrls[currentPhotoIndex % photoUrls.length];
      elBg.style.backgroundImage = `url("${url}")`;
      currentPhotoIndex++;
    } else {
      const grad = DEFAULT_AMBIENT_BACKGROUNDS[currentPhotoIndex % DEFAULT_AMBIENT_BACKGROUNDS.length];
      elBg.style.backgroundImage = grad;
      currentPhotoIndex++;
    }

    elBg.classList.toggle('zoom');
  }

  document.addEventListener('DOMContentLoaded', init);
})();
