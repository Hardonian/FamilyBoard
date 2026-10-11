(function() {
  'use strict';
  const form = document.getElementById('setup-form');
  if (!form) return;

  const password = document.getElementById('admin_password');
  const confirmPassword = document.getElementById('admin_password_confirm');
  const passwordFeedback = document.getElementById('password-feedback');
  const deviceNameInput = document.getElementById('device_name');
  const timezoneInput = document.getElementById('timezone');
  const tzFeedback = document.getElementById('timezone-feedback');
  const btnValidateTz = document.getElementById('btn-validate-tz');
  const diagModal = document.getElementById('setup-diagnostics-modal');
  const btnViewDiag = document.getElementById('btn-view-diagnostics');
  const btnCloseDiag = document.getElementById('btn-close-diag-modal');
  const btnDoneDiag = document.getElementById('btn-done-diag-modal');
  const btnCopyDiag = document.getElementById('btn-copy-diag-json');
  const diagContent = document.getElementById('setup-diag-content');

  let currentStep = 1;
  const totalSteps = 5;

  function updatePasswordValidation() {
    if (!password || !confirmPassword) return;
    const p1 = password.value;
    const p2 = confirmPassword.value;

    if (!p1 && !p2) {
      if (passwordFeedback) passwordFeedback.textContent = '';
      confirmPassword.setCustomValidity('');
      return;
    }

    if (p1.length > 0 && p1.length < 8) {
      password.setCustomValidity('Password must be at least 8 characters.');
      if (passwordFeedback) {
        passwordFeedback.className = 'form-help-inline danger';
        passwordFeedback.textContent = 'Password must be at least 8 characters.';
      }
      return;
    } else {
      password.setCustomValidity('');
    }

    if (p2.length > 0 && p1 !== p2) {
      confirmPassword.setCustomValidity('Passwords do not match.');
      if (passwordFeedback) {
        passwordFeedback.className = 'form-help-inline danger';
        passwordFeedback.textContent = 'Passwords do not match.';
      }
    } else if (p2.length > 0 && p1 === p2 && p1.length >= 8) {
      confirmPassword.setCustomValidity('');
      if (passwordFeedback) {
        passwordFeedback.className = 'form-help-inline success';
        passwordFeedback.textContent = '✓ Passwords match securely.';
      }
    } else {
      confirmPassword.setCustomValidity('');
      if (passwordFeedback) passwordFeedback.textContent = '';
    }
  }

  function validateStep(stepNum) {
    const card = document.querySelector(`[data-setup-step="${stepNum}"]`);
    if (!card) return true;

    if (stepNum === 2) {
      updatePasswordValidation();
    }

    const inputs = Array.from(card.querySelectorAll('input[required]'));
    for (const input of inputs) {
      if (!input.checkValidity()) {
        input.reportValidity();
        return false;
      }
    }

    if (stepNum === 2) {
      if (!password || password.value.length < 8) {
        if (password) password.reportValidity();
        return false;
      }
      if (password.value !== confirmPassword.value) {
        confirmPassword.reportValidity();
        return false;
      }
    }

    return true;
  }

  function updateReviewSummary() {
    const devName = (deviceNameInput && deviceNameInput.value.trim()) || 'FamilyBoard';
    const tz = (timezoneInput && timezoneInput.value.trim()) || 'UTC';
    const m1Name = document.getElementById('member1_name')?.value.trim() || 'Family Member 1';
    const m1Email = document.getElementById('member1_email')?.value.trim() || 'Not set';
    const m2Name = document.getElementById('member2_name')?.value.trim() || 'Family Member 2';
    const m2Email = document.getElementById('member2_email')?.value.trim() || 'Not set';
    const gClient = document.getElementById('google_client_id')?.value.trim();

    const revDev = document.getElementById('review-device-name');
    if (revDev) revDev.textContent = devName;

    const revTz = document.getElementById('review-timezone');
    if (revTz) revTz.textContent = tz;

    const revGoogle = document.getElementById('review-google-status');
    if (revGoogle) {
      if (gClient) {
        revGoogle.innerHTML = '<span class="status-badge status-badge-home">Configured (' + escapeHtml(gClient.substring(0, 16)) + '…)</span>';
      } else {
        revGoogle.textContent = 'Optional (Skipped, connect later in Admin)';
      }
    }

    const revMembers = document.getElementById('review-members');
    if (revMembers) {
      revMembers.textContent = `${m1Name} (${m1Email}), ${m2Name} (${m2Email})`;
    }
  }

  function showStep(stepNum) {
    currentStep = Math.max(1, Math.min(totalSteps, stepNum));

    document.querySelectorAll('[data-setup-step]').forEach(function(card) {
      const s = parseInt(card.dataset.setupStep, 10);
      if (s === currentStep) {
        card.style.display = 'block';
        card.removeAttribute('hidden');
      } else {
        card.style.display = 'none';
        card.setAttribute('hidden', '');
      }
    });

    document.querySelectorAll('[data-progress-step]').forEach(function(item) {
      const s = parseInt(item.dataset.progressStep, 10);
      item.classList.remove('active');
      if (s === currentStep) {
        item.classList.add('active');
      }
      if (s < currentStep) {
        item.classList.add('complete');
      } else {
        item.classList.remove('complete');
      }
    });

    if (currentStep === 5) {
      updateReviewSummary();
    }

    const currentCard = document.querySelector(`[data-setup-step="${currentStep}"]`);
    if (currentCard) {
      const firstInput = currentCard.querySelector('input:not([type="hidden"]), select, button');
      if (firstInput && document.activeElement !== firstInput) {
        firstInput.focus();
      }
    }
  }

  function verifyTimezone() {
    if (!timezoneInput) return;
    const val = timezoneInput.value.trim();
    if (!val) {
      if (tzFeedback) {
        tzFeedback.className = 'form-help-inline danger';
        tzFeedback.textContent = 'Please enter a timezone name.';
      }
      return;
    }

    try {
      const now = new Date();
      const formatter = new Intl.DateTimeFormat('en-US', {
        timeZone: val,
        dateStyle: 'full',
        timeStyle: 'short'
      });
      const formatted = formatter.format(now);
      if (tzFeedback) {
        tzFeedback.className = 'form-help-inline success';
        tzFeedback.textContent = `✓ Valid timezone: ${formatted}`;
      }
    } catch {
      if (tzFeedback) {
        tzFeedback.className = 'form-help-inline danger';
        tzFeedback.textContent = `⚠️ "${val}" is not a recognized IANA timezone. Try America/Toronto, America/New_York, or UTC.`;
      }
    }
  }

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text || '';
    return div.innerHTML;
  }

  // Next / Prev button event delegation
  form.addEventListener('click', function(e) {
    const nextBtn = e.target.closest('.js-next-step');
    if (nextBtn) {
      e.preventDefault();
      const targetStep = parseInt(nextBtn.dataset.next, 10);
      if (validateStep(currentStep)) {
        showStep(targetStep);
      }
      return;
    }

    const prevBtn = e.target.closest('.js-prev-step');
    if (prevBtn) {
      e.preventDefault();
      const targetStep = parseInt(prevBtn.dataset.prev, 10);
      showStep(targetStep);
      return;
    }
  });

  // Direct progress tab clicking
  document.querySelectorAll('[data-progress-step]').forEach(function(item) {
    item.style.cursor = 'pointer';
    item.addEventListener('click', function() {
      const target = parseInt(item.dataset.progressStep, 10);
      if (target <= currentStep || validateStep(currentStep)) {
        showStep(target);
      }
    });
  });

  // Timezone validation button
  btnValidateTz?.addEventListener('click', verifyTimezone);
  timezoneInput?.addEventListener('blur', function() {
    if (timezoneInput.value.trim()) verifyTimezone();
  });

  // Password live validation
  password?.addEventListener('input', updatePasswordValidation);
  confirmPassword?.addEventListener('input', updatePasswordValidation);

  // Diagnostics modal
  btnViewDiag?.addEventListener('click', async function() {
    if (!diagModal) return;
    if (diagContent) diagContent.textContent = 'Loading system readiness diagnostics…';
    if (typeof diagModal.showModal === 'function') {
      diagModal.showModal();
    } else {
      diagModal.setAttribute('open', '');
    }

    try {
      const resp = await fetch('/setup/diagnostics');
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data = await resp.json();
      if (diagContent) diagContent.textContent = JSON.stringify(data, null, 2);
    } catch (err) {
      if (diagContent) diagContent.textContent = 'Could not load diagnostics: ' + err.message;
    }
  });

  function closeDiagModal() {
    if (!diagModal) return;
    if (typeof diagModal.close === 'function') {
      diagModal.close();
    } else {
      diagModal.removeAttribute('open');
    }
  }

  btnCloseDiag?.addEventListener('click', closeDiagModal);
  btnDoneDiag?.addEventListener('click', closeDiagModal);
  diagModal?.addEventListener('click', function(e) {
    if (e.target === diagModal) closeDiagModal();
  });

  btnCopyDiag?.addEventListener('click', async function() {
    if (!diagContent) return;
    try {
      await navigator.clipboard.writeText(diagContent.textContent);
      const prev = btnCopyDiag.textContent;
      btnCopyDiag.textContent = '✓ Copied!';
      setTimeout(() => { btnCopyDiag.textContent = prev; }, 2000);
    } catch {
      btnCopyDiag.textContent = 'Error copying';
    }
  });

  // Check if server rendered an error on initial load
  const errorEl = document.getElementById('setup-error');
  if (errorEl && errorEl.textContent.trim()) {
    const msg = errorEl.textContent.toLowerCase();
    if (msg.includes('password')) {
      currentStep = 2;
    } else if (msg.includes('google')) {
      currentStep = 3;
    } else if (msg.includes('member') || msg.includes('email')) {
      currentStep = 4;
    } else {
      currentStep = 1;
    }
  }

  // Initialize wizard view
  showStep(currentStep);
})();
