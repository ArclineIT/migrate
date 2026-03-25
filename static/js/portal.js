'use strict';

// Auto-refresh the dashboard every 60 seconds if on the dashboard page.
if (window.location.pathname === '/dashboard') {
  setTimeout(() => window.location.reload(), 60_000);
}

// Decode URL-encoded flash messages (redirect-after-post pattern puts them in query string).
const flashEl = document.querySelector('.flash');
if (flashEl) {
  flashEl.textContent = decodeURIComponent(flashEl.textContent.replace(/\+/g, ' '));
}

// Confirm-before-submit for any element with data-confirm attribute.
document.querySelectorAll('[data-confirm]').forEach(el => {
  el.addEventListener('click', e => {
    if (!confirm(el.dataset.confirm)) e.preventDefault();
  });
});
