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
  }

  document.addEventListener('DOMContentLoaded', init);
})();
