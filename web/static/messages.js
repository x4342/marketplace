'use strict';

const conversationList = document.querySelector('.conversation-list');
if (conversationList) {
  try {
    if (sessionStorage.getItem('conversation-list') === 'closed') conversationList.open = false;
    conversationList.addEventListener('toggle', () => {
      sessionStorage.setItem('conversation-list', conversationList.open ? 'open' : 'closed');
    });
  } catch {
    // The native disclosure works in browsers that disable session storage.
  }
}

const composer = document.querySelector('#message-body');
if (composer) {
  let submitting = false;
  composer.form.addEventListener('submit', () => { submitting = true; });
  window.addEventListener('pageshow', () => { submitting = false; });
  window.addEventListener('beforeunload', (event) => {
    if (!submitting && composer.value.trim()) {
      event.preventDefault();
      event.returnValue = '';
    }
  });
}
