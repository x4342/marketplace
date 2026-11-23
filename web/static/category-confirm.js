'use strict';

for (const form of document.querySelectorAll('[data-category-edit]')) {
  const prompt = form.querySelector('.platform-confirm');
  const checkbox = prompt.querySelector('input');
  prompt.hidden = true;
  checkbox.required = false;
  form.addEventListener('submit', (event) => {
    checkbox.checked = window.confirm('Save these category changes? Existing listing and order history will be kept.');
    if (!checkbox.checked) event.preventDefault();
  });
}
