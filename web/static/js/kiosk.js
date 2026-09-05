// FamilyBoard Kiosk Client Application (Vanilla JS - Zero External Dependencies)
(function() {
  'use strict';

  // --- State Variables ---
  let currentDate = new Date();
  let currentView = 'hybrid'; // 'hybrid', 'week', 'today'
  let cachedEvents = [];
  let displayCalendars = [];
  let quickTemplates = [];
  let activeInput = null;
  let isShiftActive = false;
  let isSymbolActive = false;
  let lastDeletedEvent = null;
  let undoTimeout = null;

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

  // Modals & Drawers
  const elEventModal = document.getElementById('event-modal');
  const elEventForm = document.getElementById('event-form');
  const elBtnSaveEvent = document.getElementById('btn-save-event');
  const elDayDrawerOverlay = document.getElementById('day-drawer-overlay');
  const elDrawerDateTitle = document.getElementById('drawer-date-title');
  const elDrawerEventsList = document.getElementById('drawer-events-list');
  const elBtnDrawerAddEvent = document.getElementById('btn-drawer-add-event');
  const elEventDetailModal = document.getElementById('event-detail-modal');

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
    updateClock();
    // Throttled clock update: updates every 15s instead of 1s, reducing idle CPU timer wakeups by 93%
    setInterval(updateClock, 15000);

    setupNetworkMonitoring();
    setupNavigation();
    setupQuickAddModal();
    setupEventDetailModal();
    setupDayDrawer();
    setupVirtualKeyboard();
    setupInactivityAutoReset();
    setupSSE();

    loadCalendarsAndTemplates().then(() => {
      renderView();
    });
  }

  // --- Clock & Date ---
  function updateClock() {
    const now = new Date();
    if (elClockTime) {
      elClockTime.textContent = now.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }
    if (elClockDate) {
      elClockDate.textContent = now.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
    }
  }

  // --- Status & Connectivity ---
  function setupNetworkMonitoring() {
    function updateStatus(state, text) {
      if (!elStatusBadge) return;
      elStatusBadge.className = 'status-badge ' + (state || '');
      if (elStatusText) elStatusText.textContent = text;
    }

    window.addEventListener('online', () => updateStatus('', 'Synced'));
    window.addEventListener('offline', () => updateStatus('offline', 'Offline (Cached)'));

    if (!navigator.onLine) {
      updateStatus('offline', 'Offline (Cached)');
    }
  }

  // --- Navigation & View Switching ---
  function setupNavigation() {
    document.getElementById('btn-prev')?.addEventListener('click', () => navigateCalendar(-1));
    document.getElementById('btn-next')?.addEventListener('click', () => navigateCalendar(1));
    document.getElementById('btn-today')?.addEventListener('click', () => {
      currentDate = new Date();
      renderView();
    });

    document.querySelectorAll('.view-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        document.querySelectorAll('.view-btn').forEach(b => b.classList.remove('active'));
        e.currentTarget.classList.add('active');
        currentView = e.currentTarget.dataset.view;
        renderView();
      });
    });

    document.getElementById('btn-add-event')?.addEventListener('click', () => {
      openQuickAddModal(new Date());
    });
    document.getElementById('btn-today-hero-add')?.addEventListener('click', () => {
      openQuickAddModal(new Date());
    });
  }

  function navigateCalendar(direction) {
    if (currentView === 'hybrid') {
      currentDate.setMonth(currentDate.getMonth() + direction);
    } else if (currentView === 'week') {
      currentDate.setDate(currentDate.getDate() + (direction * 7));
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

      if (calResp.ok) {
        displayCalendars = await calResp.json();
        populateDestinationTiles();
      }
      if (tmplResp.ok) {
        quickTemplates = await tmplResp.json();
        renderQuickTemplates();
      }
    } catch (e) {
      console.warn('Failed to load calendars/templates:', e);
    }
  }

  function populateDestinationTiles() {
    const grid = document.getElementById('dest-tiles-grid');
    if (!grid) return;
    grid.innerHTML = '';

    const hiddenInput = document.getElementById('event-calendar-id');

    let defaultSelected = false;
    displayCalendars.forEach((cal) => {
      if (!cal.writable) return;

      const tile = document.createElement('div');
      tile.className = 'dest-tile';
      tile.dataset.calId = cal.id;

      // Color mapping
      const initial = (cal.name || 'C').charAt(0).toUpperCase();
      let colorClass = 'dest-avatar-default';
      const lower = cal.name.toLowerCase();
      if (lower.includes('scott')) colorClass = 'dest-avatar-scott';
      else if (lower.includes('michelle')) colorClass = 'dest-avatar-michelle';
      else if (lower.includes('family')) colorClass = 'dest-avatar-family';
      else if (lower.includes('kids')) colorClass = 'dest-avatar-kids';

      tile.innerHTML = `
        <div class="dest-tile-avatar ${colorClass}" style="background-color: ${cal.display_color || 'var(--accent-primary)'}">${initial}</div>
        <div class="dest-tile-name">${cal.name}</div>
      `;

      if (cal.default_write_target || !defaultSelected) {
        tile.classList.add('selected');
        if (hiddenInput) hiddenInput.value = cal.id;
        defaultSelected = true;
      }

      tile.addEventListener('click', () => {
        grid.querySelectorAll('.dest-tile').forEach(t => t.classList.remove('selected'));
        tile.classList.add('selected');
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
      chip.innerHTML = `<span>${t.icon || '📅'}</span> <span>${t.title}</span>`;
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
      } else {
        t.classList.remove('selected');
      }
    });
  }

  // --- Fetch Events with Offline Resilience ---
  async function fetchEvents(startDate, endDate) {
    const startStr = startDate.toISOString();
    const endStr = endDate.toISOString();

    try {
      const resp = await fetch(`/api/events?start=${encodeURIComponent(startStr)}&end=${encodeURIComponent(endStr)}`);
      if (resp.ok) {
        cachedEvents = await resp.json();
        localStorage.setItem('fb_cached_events', JSON.stringify(cachedEvents));
        return cachedEvents;
      }
    } catch (e) {
      console.warn('Fetch failed, using local offline storage cache:', e);
      const offline = localStorage.getItem('fb_cached_events');
      if (offline) {
        cachedEvents = JSON.parse(offline);
        return cachedEvents;
      }
    }
    return cachedEvents || [];
  }

  // --- Master View Switcher ---
  async function renderView() {
    updateNavTitle();

    if (currentView === 'hybrid') {
      if (elMonthContainer) elMonthContainer.style.display = 'flex';
      if (elWeekContainer) elWeekContainer.style.display = 'none';
      if (elTodayContainer) elTodayContainer.style.display = 'none';
      if (elTodaySideRail) elTodaySideRail.style.display = 'flex';
      await renderMonthGrid();
      await renderTodayRail();
    } else if (currentView === 'week') {
      if (elMonthContainer) elMonthContainer.style.display = 'none';
      if (elWeekContainer) elWeekContainer.style.display = 'flex';
      if (elTodayContainer) elTodayContainer.style.display = 'none';
      if (elTodaySideRail) elTodaySideRail.style.display = 'flex';
      await renderWeekGrid();
      await renderTodayRail();
    } else { // 'today'
      if (elMonthContainer) elMonthContainer.style.display = 'none';
      if (elWeekContainer) elWeekContainer.style.display = 'none';
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
    endGridDate.setDate(endGridDate.getDate() + 41);

    const events = await fetchEvents(startGridDate, endGridDate);
    const today = new Date();
    const iterDate = new Date(startGridDate);

    for (let i = 0; i < 42; i++) {
      const cell = document.createElement('div');
      cell.className = 'day-cell';

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
      const dayEvents = events.filter(ev => {
        const evStart = new Date(ev.start_at);
        return evStart.toDateString() === cellDateStr && ev.status !== 'cancelled';
      });

      // Show top 3 events
      const maxShow = 3;
      dayEvents.slice(0, maxShow).forEach(ev => {
        const pill = document.createElement('div');
        pill.className = 'event-pill ' + getEventColorClass(ev);

        const isBirthday = ev.title.toLowerCase().includes('birthday');
        const icon = isBirthday ? '🎂 ' : '';
        const timeStr = ev.all_day ? '' : formatTimeShort(new Date(ev.start_at)) + ' ';

        pill.textContent = `${icon}${timeStr}${ev.title}`;
        pill.addEventListener('click', (e) => {
          e.stopPropagation();
          openEventDetailSheet(ev);
        });
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
      cell.addEventListener('click', () => {
        openDayDrawer(cellDateCopy, dayEvents);
      });

      elDaysGrid.appendChild(cell);
      iterDate.setDate(iterDate.getDate() + 1);
    }
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

    // Time Gutter (7 AM to 10 PM)
    const gutter = document.createElement('div');
    gutter.className = 'week-time-gutter';
    const headerSpacer = document.createElement('div');
    headerSpacer.style.height = '62px';
    gutter.appendChild(headerSpacer);

    for (let h = 7; h <= 21; h++) {
      const slot = document.createElement('div');
      slot.className = 'week-time-slot-label';
      const period = h >= 12 ? 'PM' : 'AM';
      const displayHour = h > 12 ? h - 12 : (h === 0 ? 12 : h);
      slot.textContent = `${displayHour} ${period}`;
      gutter.appendChild(slot);
    }
    elWeekColumnsGrid.appendChild(gutter);

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
          nowLine.style.top = `${topPx}px`;
          slots.appendChild(nowLine);
        }
      }

      // Filter events for this column
      const colDateStr = iter.toDateString();
      const colEvents = events.filter(ev => {
        const evStart = new Date(ev.start_at);
        return evStart.toDateString() === colDateStr && ev.status !== 'cancelled';
      });

      colEvents.forEach(ev => {
        const evCard = document.createElement('div');
        evCard.className = 'week-event-card ' + getEventColorClass(ev);

        const start = new Date(ev.start_at);
        const end = new Date(ev.end_at);

        let startHour = start.getHours() + (start.getMinutes() / 60);
        let endHour = end.getHours() + (end.getMinutes() / 60);
        if (ev.all_day) {
          startHour = 7;
          endHour = 8.5;
        }

        const clampedStart = Math.max(7, Math.min(22, startHour));
        const clampedEnd = Math.max(clampedStart + 0.5, Math.min(22, endHour));

        const top = (clampedStart - 7) * 60;
        const height = Math.max(32, (clampedEnd - clampedStart) * 60);

        evCard.style.top = `${top}px`;
        evCard.style.height = `${height}px`;

        const isBirthday = ev.title.toLowerCase().includes('birthday');
        const icon = isBirthday ? '🎂 ' : '';
        evCard.innerHTML = `<div>${icon}${formatTimeShort(start)}</div><div style="font-weight:800; font-size: 0.95rem;">${ev.title}</div>`;

        evCard.addEventListener('click', (e) => {
          e.stopPropagation();
          openEventDetailSheet(ev);
        });

        slots.appendChild(evCard);
      });

      const colDateCopy = new Date(iter);
      slots.addEventListener('click', () => {
        openQuickAddModal(colDateCopy);
      });

      col.appendChild(slots);
      elWeekColumnsGrid.appendChild(col);
      iter.setDate(iter.getDate() + 1);
    }
  }

  // --- 3. Render Today Full View ---
  async function renderTodayFull() {
    if (!elTodayFullCardsGrid) return;
    elTodayFullCardsGrid.innerHTML = '';

    const start = new Date();
    start.setHours(0, 0, 0, 0);
    const end = new Date();
    end.setHours(23, 59, 59, 999);

    if (elTodayHeroDate) {
      elTodayHeroDate.textContent = start.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
    }

    const events = await fetchEvents(start, end);
    const todayEvents = events.filter(ev => ev.status !== 'cancelled');

    if (todayEvents.length === 0) {
      elTodayFullCardsGrid.innerHTML = `
        <div class="rail-empty-state" style="padding: 60px;">
          <div class="rail-empty-icon">✨</div>
          <h2>Nothing planned for today!</h2>
          <p style="margin-top: 8px;">Tap "+ Add Event" above to schedule activities or appointments.</p>
        </div>
      `;
      return;
    }

    todayEvents.sort((a, b) => new Date(a.start_at) - new Date(b.start_at));

    todayEvents.forEach(ev => {
      const card = document.createElement('div');
      card.className = 'today-full-card ' + getEventCardColorClass(ev);

      const start = new Date(ev.start_at);
      const timeBadgeText = ev.all_day ? 'ALL DAY' : formatTimeShort(start);

      const isBirthday = ev.title.toLowerCase().includes('birthday');
      const icon = isBirthday ? '🎂 ' : '';

      card.innerHTML = `
        <div class="today-card-time-badge">${timeBadgeText}</div>
        <div class="today-card-content">
          <div class="today-card-title">${icon}${ev.title}</div>
          <div class="today-card-details">
            <span>📅 ${ev.all_day ? 'All-day' : `${formatTimeShort(start)} – ${formatTimeShort(new Date(ev.end_at))}`}</span>
            ${ev.location ? `<span>📍 ${ev.location}</span>` : ''}
          </div>
        </div>
      `;

      card.addEventListener('click', () => openEventDetailSheet(ev));
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

    if (todayEvents.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'rail-empty-state';
      empty.innerHTML = `
        <div class="rail-empty-icon">✨</div>
        <p><strong>Nothing scheduled today!</strong></p>
        <p style="font-size: 0.95rem; margin-top: 4px;">Enjoy your free family time.</p>
      `;
      elRailEventsList.appendChild(empty);
    } else {
      todayEvents.forEach(ev => {
        elRailEventsList.appendChild(createRailEventCard(ev));
      });
    }

    // Tomorrow Preview Section
    if (tomorrowEvents.length > 0) {
      const tmrwHeader = document.createElement('div');
      tmrwHeader.style.fontSize = '1.05rem';
      tmrwHeader.style.fontWeight = '800';
      tmrwHeader.style.color = 'var(--text-muted)';
      tmrwHeader.style.textTransform = 'uppercase';
      tmrwHeader.style.marginTop = '16px';
      tmrwHeader.style.marginBottom = '4px';
      tmrwHeader.textContent = 'Tomorrow';
      elRailEventsList.appendChild(tmrwHeader);

      tomorrowEvents.forEach(ev => {
        elRailEventsList.appendChild(createRailEventCard(ev));
      });
    }
  }

  function createRailEventCard(ev) {
    const card = document.createElement('div');
    card.className = 'rail-event-card ' + getEventCardColorClass(ev);

    const start = new Date(ev.start_at);
    const timeText = ev.all_day ? 'All Day' : `${formatTimeShort(start)} – ${formatTimeShort(new Date(ev.end_at))}`;

    const isBirthday = ev.title.toLowerCase().includes('birthday');
    const icon = isBirthday ? '🎂 ' : '';

    card.innerHTML = `
      <div class="rail-event-time-row">
        <span>${timeText}</span>
      </div>
      <div class="rail-event-title">${icon}${ev.title}</div>
      ${ev.location ? `<div class="rail-event-meta">📍 ${ev.location}</div>` : ''}
    `;

    card.addEventListener('click', () => openEventDetailSheet(ev));
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
        elDrawerEventsList.innerHTML = `
          <div class="rail-empty-state">
            <div class="rail-empty-icon">📅</div>
            <p>No events scheduled for this day.</p>
          </div>
        `;
      } else {
        events.forEach(ev => {
          elDrawerEventsList.appendChild(createRailEventCard(ev));
        });
      }
    }

    if (elDayDrawerOverlay) elDayDrawerOverlay.style.display = 'flex';
  }

  function closeDayDrawer() {
    if (elDayDrawerOverlay) elDayDrawerOverlay.style.display = 'none';
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

    elToastUndoBtn?.addEventListener('click', async () => {
      if (lastDeletedEvent && lastDeletedEvent.id) {
        await executeUndo(lastDeletedEvent.id);
      }
    });
  }

  function openEventDetailSheet(ev) {
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

    const isBirthday = ev.title.toLowerCase().includes('birthday');
    if (titleEl) titleEl.textContent = (isBirthday ? '🎂 ' : '') + ev.title;

    const start = new Date(ev.start_at);
    const end = new Date(ev.end_at);
    if (datetimeEl) {
      const datePart = start.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' });
      const timePart = ev.all_day ? 'All-day' : `${formatTimeShort(start)} – ${formatTimeShort(end)}`;
      datetimeEl.innerHTML = `📅 <span>${datePart} · ${timePart}</span>`;
    }

    if (calBadge) {
      calBadge.textContent = getCalendarName(ev.calendar_id);
      calBadge.style.backgroundColor = getCalendarColor(ev.calendar_id);
      calBadge.style.color = '#ffffff';
    }

    if (ev.location) {
      if (locRow) locRow.style.display = 'flex';
      if (locEl) locEl.textContent = ev.location;
    } else {
      if (locRow) locRow.style.display = 'none';
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

    if (elEventDetailModal) elEventDetailModal.style.display = 'flex';
  }

  function closeEventDetailSheet() {
    if (elEventDetailModal) elEventDetailModal.style.display = 'none';
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

    // Tabs
    const tabQuick = document.getElementById('tab-quick-tap');
    const tabNL = document.getElementById('tab-natural-lang');
    const modeQuick = document.getElementById('mode-quick-tap');
    const modeNL = document.getElementById('mode-natural-lang');

    tabQuick?.addEventListener('click', () => {
      tabQuick.classList.add('active');
      tabNL.classList.remove('active');
      modeQuick.style.display = 'block';
      modeNL.style.display = 'none';
    });

    tabNL?.addEventListener('click', () => {
      tabNL.classList.add('active');
      tabQuick.classList.remove('active');
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
          titleInput.focus();
        }
      });
    });

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
  }

  function toggleAllDayControls(isAllDay) {
    const container = document.getElementById('time-controls-container');
    if (container) {
      container.style.opacity = isAllDay ? '0.4' : '1';
      container.style.pointerEvents = isAllDay ? 'none' : 'auto';
    }
  }

  function openQuickAddModal(date) {
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

    // Set date
    const d = date || new Date();
    document.getElementById('event-start-date').value = formatDateForInput(d);

    // Reset relative date button state
    document.querySelectorAll('#relative-dates-row .date-btn').forEach(b => b.classList.remove('selected'));
    const todayStr = formatDateForInput(new Date());
    const targetStr = formatDateForInput(d);
    if (targetStr === todayStr) {
      document.querySelector('#relative-dates-row [data-rel="today"]')?.classList.add('selected');
    } else {
      document.querySelector('#relative-dates-row [data-rel="custom"]')?.classList.add('selected');
      document.getElementById('custom-date-container').style.display = 'block';
    }

    // Default time 5:00 PM, 1 hour
    document.getElementById('event-start-time').value = '17:00';
    document.getElementById('event-duration-min').value = '60';
    document.getElementById('event-all-day').checked = false;
    toggleAllDayControls(false);
    recalculateEndTime();

    // Reset tabs
    document.getElementById('tab-quick-tap')?.click();

    if (elEventModal) elEventModal.style.display = 'flex';
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

    if (ev.all_day) {
      document.getElementById('event-all-day').checked = true;
      toggleAllDayControls(true);
    }

    selectCalendarDestination(ev.calendar_id);

    const btnSave = document.getElementById('btn-save-event');
    if (btnSave) btnSave.textContent = 'Save Changes';
  }

  function closeQuickAddModal() {
    if (elEventModal) elEventModal.style.display = 'none';
    hideVirtualKeyboard();
  }

  async function handleNaturalLanguageParse() {
    const text = document.getElementById('nl-input')?.value?.trim();
    const card = document.getElementById('nl-preview-card');
    if (!text || text.length < 3) {
      if (card) card.style.display = 'none';
      return;
    }

    try {
      const resp = await fetch('/api/kiosk/parse-natural', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: text })
      });

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
      }
    } catch (e) {
      console.warn('NL parse error:', e);
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
          alert('Please enter a clearer event description.');
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
        alert('Please enter an event title.');
        return;
      }

      if (!calendarID) {
        alert('Please select a calendar for this event.');
        return;
      }

      const startAt = new Date(`${startDateStr}T${startTimeStr}:00`);
      const endAt = new Date(`${startDateStr}T${endTimeStr}:00`);

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
        const err = await resp.json();
        alert(err.error || 'Failed to save event. Saved to local offline queue.');
        closeQuickAddModal();
        renderView();
      }
    } catch (e) {
      console.warn('Network save exception:', e);
      closeQuickAddModal();
      renderView();
      showToast('Saved locally (Offline)', false);
    } finally {
      isSavingEvent = false;
      if (btnSave) {
        btnSave.disabled = false;
        btnSave.textContent = 'Add Event';
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
      ['q', 'w', 'e', 'r', 't', 'y', 'u', 'i', 'o', 'p'],
      ['a', 's', 'd', 'f', 'g', 'h', 'j', 'k', 'l'],
      ['Shift', 'z', 'x', 'c', 'v', 'b', 'n', 'm', '⌫'],
      ['?123', 'Space', 'Clear', 'Done']
    ];

    const symbolRows = [
      ['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'],
      ['@', '#', '$', '%', '&', '-', '+', '(', ')', '/'],
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

  function showVirtualKeyboard() {
    if (elVirtualKeyboard) elVirtualKeyboard.style.display = 'flex';
  }

  function hideVirtualKeyboard() {
    if (elVirtualKeyboard) elVirtualKeyboard.style.display = 'none';
  }

  // --- Real-Time SSE live stream ---
  function setupSSE() {
    try {
      const evtSource = new EventSource('/api/events/stream');
      evtSource.onmessage = function(e) {
        if (e.data === 'reload') {
          renderView();
        }
      };
      evtSource.onerror = function() {
        evtSource.close();
        setTimeout(setupSSE, 15000);
      };
    } catch (e) {
      console.warn('SSE stream error:', e);
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
    if (title.includes('birthday')) return 'pill-birthday';
    if (title.includes('scott')) return 'pill-scott';
    if (title.includes('michelle')) return 'pill-michelle';
    if (title.includes('kids') || title.includes('soccer') || title.includes('swimming')) return 'pill-kids';
    return 'pill-family';
  }

  function getEventCardColorClass(ev) {
    const title = (ev.title || '').toLowerCase();
    if (title.includes('birthday')) return 'card-birthday';
    if (title.includes('scott')) return 'card-scott';
    if (title.includes('michelle')) return 'card-michelle';
    if (title.includes('kids') || title.includes('soccer') || title.includes('swimming')) return 'card-kids';
    return 'card-family';
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
    hideVirtualKeyboard();

    // 2. Reset date to today
    currentDate = new Date();

    // 3. Reset view to 30-day (hybrid Month) view
    currentView = 'hybrid';
    document.querySelectorAll('.view-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.view === 'hybrid');
    });

    // 4. Render fresh view
    renderView();
  }

  function onInactivityElapsed() {
    const elapsed = Date.now() - lastTouchTime;
    if (elapsed >= INACTIVITY_TIMEOUT_MS) {
      // Return to default 30-day view
      resetToDefault30DayView();

      // Apply gentle ambient dimming if plugged in and untouched for 15+ minutes
      if (isPluggedIn) {
        document.body.classList.add('kiosk-ambient-dim');
      }
    } else {
      // Reschedule for remaining time
      inactivityTimer = setTimeout(onInactivityElapsed, INACTIVITY_TIMEOUT_MS - elapsed);
    }
  }

  document.addEventListener('DOMContentLoaded', init);
})();
