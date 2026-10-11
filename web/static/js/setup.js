(function() {
  'use strict';
  const form = document.getElementById('setup-form');
  if (!form) return;
  const password = document.getElementById('admin_password');
  const confirmPassword = document.getElementById('admin_password_confirm');

  function updateProgress() {
    document.querySelectorAll('[data-setup-step]').forEach(function(section) {
      const step = section.dataset.setupStep;
      const required = Array.from(section.querySelectorAll('[required]'));
      const complete = required.length === 0 || required.every(function(input) { return input.checkValidity(); });
      const marker = document.querySelector(`[data-progress-step="${step}"]`);
      if (marker) marker.classList.toggle('complete', complete);
    });
  }

  function validatePasswords() {
    const mismatch = confirmPassword.value && password.value !== confirmPassword.value;
    confirmPassword.setCustomValidity(mismatch ? 'Passwords do not match.' : '');
  }

  form.addEventListener('input', function() {
    validatePasswords();
    updateProgress();
  });
  form.addEventListener('submit', validatePasswords);
  updateProgress();
})();
