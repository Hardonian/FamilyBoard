// FamilyBoard Kiosk Client Application (Vanilla JS - Zero External Dependencies)
(function() {
  'use strict';

  let currentDate = new Date();
  let currentView = 'month';
  let cachedEvents = [];
  let displayCalendars = [];

  // DOM Elements
  const elClockTime = document.getElementById('clock-time');
  const elClockDate = document.getElementById('clock-date');
  const elNavTitle = document.getElementById('nav-title');
  const elDaysGrid = document.getElementById('days-grid');
  const elAgendaView = document.getElementById('agenda-view');
  const elStatusBadge = document.getElementById('status-badge');
  const elStatusText = document.getElementById('status-text');
  const elEventModal = document.getElementById('event-modal');
  const elEventForm = document.getElementById('event-form');
  const elBtnDelete = document.getElementById('btn-delete-event');

  function init() {
    updateClock();
    setInterval(updateClock, 1000);

    setupNetworkMonitoring();
    setupNavigation();
    setupModal();
    setupSSE();

    loadCalendars();
    renderView();
  }

  function updateClock() {
    const now = new Date();
    if (elClockTime) {
      elClockTime.textContent = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }
    if (elClockDate) {
      elClockDate.textContent = now.toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });
    }
  }

  function setupNetworkMonitoring() {
    function updateOnlineStatus() {
      if (navigator.onLine) {
        if (elStatusBadge) {
          elStatusBadge.className = 'status-badge';
          elStatusText.textContent = 'Appliance Online';
        }
      } else {
        if (elStatusBadge) {
          elStatusBadge.className = 'status-badge offline';
          elStatusText.textContent = 'Offline (Cached)';
        }
      }
    }
    window.addEventListener('online', updateOnlineStatus);
    window.addEventListener('offline', updateOnlineStatus);
    updateOnlineStatus();
  }

  function setupNavigation() {
    document.getElementById('btn-prev')?.addEventListener('click', () => {
      navigateCalendar(-1);
    });
    document.getElementById('btn-next')?.addEventListener('click', () => {
      navigateCalendar(1);
    });
    document.getElementById('btn-today')?.addEventListener('click', () => {
      currentDate = new Date();
      renderView();
    });

    document.querySelectorAll('.view-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        document.querySelectorAll('.view-btn').forEach(b => b.classList.remove('active'));
        e.target.classList.add('active');
        currentView = e.target.dataset.view;
        renderView();
      });
    });

    document.getElementById('btn-add-event')?.addEventListener('click', () => {
      openAddEventModal(new Date());
    });
  }

  function navigateCalendar(direction) {
    if (currentView === 'month') {
      currentDate.setMonth(currentDate.getMonth() + direction);
    } else if (currentView === 'week') {
      currentDate.setDate(currentDate.getDate() + (direction * 7));
    } else {
      currentDate.setDate(currentDate.getDate() + direction);
    }
    renderView();
  }

  async function loadCalendars() {
    try {
      const resp = await fetch('/api/calendars');
      if (resp.ok) {
        displayCalendars = await resp.json();
        populateCalendarSelect();
      }
    } catch (e) {
      console.warn('Could not load calendars:', e);
    }
  }

  function populateCalendarSelect() {
    const sel = document.getElementById('event-calendar-id');
    if (!sel) return;
    sel.innerHTML = '';
    displayCalendars.forEach(cal => {
      if (cal.writable) {
        const opt = document.createElement('option');
        opt.value = cal.id;
        opt.textContent = cal.name;
        if (cal.default_write_target) opt.selected = true;
        sel.appendChild(opt);
      }
    });
  }

  async function renderView() {
    updateNavTitle();

    if (currentView === 'month') {
      if (elDaysGrid) elDaysGrid.style.display = 'grid';
      if (elAgendaView) elAgendaView.style.display = 'none';
      await renderMonthGrid();
    } else {
      if (elDaysGrid) elDaysGrid.style.display = 'none';
      if (elAgendaView) elAgendaView.style.display = 'flex';
      await renderAgendaView();
    }
  }

  function updateNavTitle() {
    if (!elNavTitle) return;
    if (currentView === 'month') {
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
      console.warn('Network fetch failed, loading offline local storage cache:', e);
      const offline = localStorage.getItem('fb_cached_events');
      if (offline) {
        cachedEvents = JSON.parse(offline);
        return cachedEvents;
      }
    }
    return cachedEvents || [];
  }

  async function renderMonthGrid() {
    if (!elDaysGrid) return;
    elDaysGrid.innerHTML = '';

    const year = currentDate.getFullYear();
    const month = currentDate.getMonth();

    const firstDay = new Date(year, month, 1);
    const lastDay = new Date(year, month + 1, 0);

    const startGridDate = new Date(firstDay);
    startGridDate.setDate(startGridDate.getDate() - startGridDate.getDay());

    const endGridDate = new Date(startGridDate);
    endGridDate.setDate(endGridDate.getDate() + 41); // 6 rows of 7 days

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

      const numRow = document.createElement('div');
      numRow.className = 'day-number-row';

      const num = document.createElement('div');
      num.className = 'day-number';
      num.textContent = iterDate.getDate();
      numRow.appendChild(num);
      cell.appendChild(numRow);

      // Event pills container
      const pillsContainer = document.createElement('div');
      pillsContainer.className = 'event-pills-container';

      const cellDateStr = iterDate.toDateString();
      const dayEvents = events.filter(ev => {
        const evStart = new Date(ev.start_at);
        return evStart.toDateString() === cellDateStr;
      });

      dayEvents.forEach(ev => {
        const pill = document.createElement('div');
        pill.className = 'event-pill';

        // Color coding
        if (ev.title.toLowerCase().includes('scott')) {
          pill.classList.add('pill-scott');
        } else if (ev.title.toLowerCase().includes('michelle')) {
          pill.classList.add('pill-michelle');
        } else {
          pill.classList.add('pill-family');
        }

        const timeStr = ev.all_day ? '' : new Date(ev.start_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ' ';
        pill.textContent = timeStr + ev.title;
        pill.addEventListener('click', (e) => {
          e.stopPropagation();
          openEditEventModal(ev);
        });
        pillsContainer.appendChild(pill);
      });

      cell.appendChild(pillsContainer);

      const cellDateCopy = new Date(iterDate);
      cell.addEventListener('click', () => {
        openAddEventModal(cellDateCopy);
      });

      elDaysGrid.appendChild(cell);
      iterDate.setDate(iterDate.getDate() + 1);
    }
  }

  async function renderAgendaView() {
    if (!elAgendaView) return;
    elAgendaView.innerHTML = '';

    const start = new Date(currentDate);
    const end = new Date(currentDate);
    end.setDate(end.getDate() + 14); // 2 weeks upcoming

    const events = await fetchEvents(start, end);

    if (events.length === 0) {
      elAgendaView.innerHTML = '<div class="agenda-day-card"><p style="text-align:center; padding: 24px; color: var(--text-muted); font-size: 1.2rem;">No events scheduled for this period.</p></div>';
      return;
    }

    // Group by date
    const grouped = {};
    events.forEach(ev => {
      const d = new Date(ev.start_at).toLocaleDateString([], { weekday: 'long', month: 'long', day: 'numeric' });
      if (!grouped[d]) grouped[d] = [];
      grouped[d].push(ev);
    });

    Object.keys(grouped).forEach(dateStr => {
      const card = document.createElement('div');
      card.className = 'agenda-day-card';

      const title = document.createElement('div');
      title.className = 'agenda-day-title';
      title.textContent = dateStr;
      card.appendChild(title);

      grouped[dateStr].forEach(ev => {
        const item = document.createElement('div');
        item.className = 'agenda-event-item';

        const timeDiv = document.createElement('div');
        timeDiv.className = 'agenda-event-time';
        timeDiv.textContent = ev.all_day ? 'All Day' : new Date(ev.start_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

        const detDiv = document.createElement('div');
        detDiv.className = 'agenda-event-details';

        const titleDiv = document.createElement('div');
        titleDiv.className = 'agenda-event-title';
        titleDiv.textContent = ev.title;

        detDiv.appendChild(titleDiv);
        if (ev.location) {
          const locDiv = document.createElement('div');
          locDiv.className = 'agenda-event-location';
          locDiv.textContent = '📍 ' + ev.location;
          detDiv.appendChild(locDiv);
        }

        item.appendChild(timeDiv);
        item.appendChild(detDiv);
        item.addEventListener('click', () => openEditEventModal(ev));

        card.appendChild(item);
      });

      elAgendaView.appendChild(card);
    });
  }

  function setupModal() {
    document.getElementById('btn-close-modal')?.addEventListener('click', closeModal);
    document.getElementById('btn-cancel-modal')?.addEventListener('click', closeModal);

    elEventForm?.addEventListener('submit', async (e) => {
      e.preventDefault();
      await saveEvent();
    });

    elBtnDelete?.addEventListener('click', async () => {
      const eventID = document.getElementById('event-id').value;
      if (eventID && confirm('Are you sure you want to delete this event?')) {
        await deleteEvent(eventID);
      }
    });
  }

  function openAddEventModal(date) {
    document.getElementById('modal-title').textContent = 'New Family Event';
    document.getElementById('event-id').value = '';
    document.getElementById('event-title').value = '';
    document.getElementById('event-location').value = '';
    document.getElementById('event-description').value = '';

    const yyyy = date.getFullYear();
    const mm = String(date.getMonth() + 1).padStart(2, '0');
    const dd = String(date.getDate()).padStart(2, '0');
    const dateVal = `${yyyy}-${mm}-${dd}`;

    document.getElementById('event-start-date').value = dateVal;
    document.getElementById('event-end-date').value = dateVal;
    document.getElementById('event-start-time').value = '10:00';
    document.getElementById('event-end-time').value = '11:00';
    document.getElementById('event-all-day').checked = false;

    if (elBtnDelete) elBtnDelete.style.display = 'none';
    if (elEventModal) elEventModal.style.display = 'flex';
  }

  function openEditEventModal(ev) {
    document.getElementById('modal-title').textContent = 'Edit Event';
    document.getElementById('event-id').value = ev.id;
    document.getElementById('event-title').value = ev.title;
    document.getElementById('event-location').value = ev.location || '';
    document.getElementById('event-description').value = ev.description || '';

    const start = new Date(ev.start_at);
    const end = new Date(ev.end_at);

    document.getElementById('event-start-date').value = start.toISOString().split('T')[0];
    document.getElementById('event-end-date').value = end.toISOString().split('T')[0];
    document.getElementById('event-start-time').value = start.toTimeString().substring(0, 5);
    document.getElementById('event-end-time').value = end.toTimeString().substring(0, 5);
    document.getElementById('event-all-day').checked = ev.all_day;

    if (elBtnDelete) elBtnDelete.style.display = 'inline-flex';
    if (elEventModal) elEventModal.style.display = 'flex';
  }

  function closeModal() {
    if (elEventModal) elEventModal.style.display = 'none';
  }

  async function saveEvent() {
    const id = document.getElementById('event-id').value;
    const title = document.getElementById('event-title').value;
    const location = document.getElementById('event-location').value;
    const description = document.getElementById('event-description').value;
    const calendarID = document.getElementById('event-calendar-id').value;
    const allDay = document.getElementById('event-all-day').checked;

    const startDateStr = document.getElementById('event-start-date').value;
    const startTimeStr = document.getElementById('event-start-time').value || '00:00';
    const endDateStr = document.getElementById('event-end-date').value;
    const endTimeStr = document.getElementById('event-end-time').value || '00:00';

    const startAt = new Date(`${startDateStr}T${startTimeStr}:00`);
    const endAt = new Date(`${endDateStr}T${endTimeStr}:00`);

    const payload = {
      id: id || undefined,
      calendar_id: calendarID,
      title: title,
      location: location,
      description: description,
      start_at: startAt.toISOString(),
      end_at: endAt.toISOString(),
      all_day: allDay,
    };

    const method = id ? 'PUT' : 'POST';
    const url = id ? `/api/events/${encodeURIComponent(id)}` : '/api/events';

    try {
      const resp = await fetch(url, {
        method: method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (resp.ok) {
        closeModal();
        renderView();
      } else {
        alert('Failed to save event. Saved to local queue.');
      }
    } catch (e) {
      console.warn('Network failure during save:', e);
      closeModal();
      renderView();
    }
  }

  async function deleteEvent(id) {
    try {
      const resp = await fetch(`/api/events/${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (resp.ok) {
        closeModal();
        renderView();
      }
    } catch (e) {
      console.warn('Delete failed:', e);
    }
  }

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
        setTimeout(setupSSE, 30000); // Reconnect in 30s
      };
    } catch (e) {
      console.warn('SSE not supported or failed:', e);
    }
  }

  document.addEventListener('DOMContentLoaded', init);
})();
