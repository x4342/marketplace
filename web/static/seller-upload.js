const draftForm = document.querySelector('#seller-draft-form');

if (draftForm && typeof XMLHttpRequest === 'function') {
  const feedback = draftForm.querySelector('#upload-feedback');
  const progress = draftForm.querySelector('#upload-progress');
  const message = draftForm.querySelector('#upload-message');
  const submitButtons = draftForm.querySelectorAll('button[name="action"]');
  const input = draftForm.querySelector('input[name="image"]');

  draftForm.addEventListener('submit', (event) => {
    if (!input.files.length) return;
    event.preventDefault();
    feedback.hidden = false;

    if (input.files[0].size > 3 * 1024 * 1024) {
      progress.hidden = true;
      message.textContent = 'Upload a JPEG or PNG of 3 MB or less.';
      return;
    }

    const data = new FormData(draftForm);
    if (event.submitter) data.set(event.submitter.name, event.submitter.value);
    const request = new XMLHttpRequest();
    request.open('POST', draftForm.action);
    request.timeout = 20000;
    progress.hidden = false;
    progress.removeAttribute('value');
    message.textContent = 'Uploading listing image…';
    submitButtons.forEach((button) => { button.disabled = true; });

    request.upload.addEventListener('progress', (upload) => {
      if (upload.lengthComputable) progress.value = 100 * upload.loaded / upload.total;
    });
    request.addEventListener('load', () => {
      if (request.status === 200 && request.responseURL !== draftForm.action) {
        const next = new URL(request.responseURL);
        if (next.origin === location.origin) {
          location.assign(next);
          return;
        }
      }
      const page = new DOMParser().parseFromString(request.responseText, 'text/html');
      message.textContent = page.querySelector('#seller-errors')?.textContent.trim()
        || 'The draft could not be saved. Check your account session and try again.';
    });
    request.addEventListener('error', () => {
      message.textContent = 'The upload was interrupted. Try again.';
    });
    request.addEventListener('timeout', () => {
      message.textContent = 'The upload timed out. Check your draft before retrying.';
    });
    request.addEventListener('loadend', () => {
      progress.hidden = true;
      submitButtons.forEach((button) => { button.disabled = false; });
    });
    request.send(data);
  });
}
