const draftForm = document.querySelector('#seller-draft-form');

if (draftForm && typeof XMLHttpRequest === 'function' && typeof createImageBitmap === 'function') {
  const get = (id) => draftForm.querySelector('#' + id);
  const input = draftForm.elements.image;
  const crop = draftForm.elements.crop;
  const editor = get('image-cropper');
  const canvas = get('crop-preview');
  const pixels = canvas.getContext('2d');
  const positionX = get('crop-x');
  const positionY = get('crop-y');
  const zoom = get('crop-zoom');
  const controls = [positionX, positionY, zoom];
  const confirm = get('crop-confirm');
  const cropStatus = get('crop-status');
  const feedback = get('upload-feedback');
  const progress = get('upload-progress');
  const message = get('upload-message');
  const submitButtons = draftForm.querySelectorAll('button[name="action"]');
  let frame, final, width, height, pending, drag;

  function setFeedback(text) {
    feedback.hidden = false;
    message.textContent = text;
  }

  function discard() {
    pending?.abort();
    pending = undefined;
    frame?.close();
    final?.close();
    frame = final = undefined;
    editor.hidden = true;
    crop.value = '';
    progress.hidden = true;
    feedback.hidden = true;
    confirm.disabled = true;
    controls.forEach((control) => { control.disabled = false; });
  }

  function selection() {
    const short = Math.min(width, height);
    const side = short - Math.round((short - 256) * Number(zoom.value) / 100);
    const x = Math.min(Number(positionX.value), width - side);
    const y = Math.min(Number(positionY.value), height - side);
    return [x, y, side];
  }

  function paint() {
    if (!frame || pending) return;
    crop.value = '';
    final?.close();
    final = undefined;
    const [x, y, side] = selection();
    positionX.max = width - side;
    positionY.max = height - side;
    positionX.value = x;
    positionY.value = y;
    pixels.drawImage(frame, x * frame.width / width, y * frame.height / height,
      side * frame.width / width, side * frame.height / height, 0, 0, canvas.width, canvas.height);
    cropStatus.textContent = 'Confirm the crop before saving the draft.';
  }

  async function requestPreview(selection) {
    const form = new FormData();
    form.append('csrf', draftForm.elements.csrf.value);
    if (selection) form.append('crop', selection);
    form.append('image', input.files[0]);
    const request = new AbortController();
    pending = request;
    confirm.disabled = true;
    controls.forEach((control) => { control.disabled = true; });
    const timeout = setTimeout(() => request.abort(), 35000);
    cropStatus.textContent = selection ? 'Preparing the final square…' : 'Loading the photo…';
    try {
      const result = await fetch(draftForm.getAttribute('action') + '/image-preview', {
        method: 'POST', body: form, signal: request.signal,
      });
      if (!result.ok) {
        const type = result.headers.get('Content-Type') || '';
        throw new Error(type.startsWith('text/plain') ? (await result.text()).trim() : 'The preview is unavailable. Check your session and choose the file again.');
      }
      const decoded = await createImageBitmap(await result.blob());
      if (pending !== request) {
        decoded.close();
        return;
      }
      pending = undefined;
      if (selection) {
        final = decoded;
        pixels.drawImage(final, 0, 0, canvas.width, canvas.height);
        crop.value = selection;
        cropStatus.textContent = 'Crop confirmed. Save the draft to keep it.';
      } else {
        frame = decoded;
        width = Number(result.headers.get('X-Image-Width'));
        height = Number(result.headers.get('X-Image-Height'));
        zoom.value = 0;
        const short = Math.min(width, height);
        positionX.max = width - short;
        positionY.max = height - short;
        positionX.value = Math.floor((width - short) / 2);
        positionY.value = Math.floor((height - short) / 2);
        paint();
      }
    } catch (error) {
      if (pending !== request) return;
      pending = undefined;
      cropStatus.textContent = error.name === 'AbortError' ? 'The preview timed out. Choose the file again.' : error.message;
    } finally {
      clearTimeout(timeout);
      if (!pending) {
        confirm.disabled = !frame;
        controls.forEach((control) => { control.disabled = !frame; });
      }
    }
  }

  input.addEventListener('change', () => {
    discard();
    if (!input.files.length) return;
    if (input.files[0].size > 10_000_000) {
      input.value = '';
      setFeedback('Choose a JPEG, PNG, or WebP of 10 MB or less.');
      return;
    }
    editor.hidden = false;
    requestPreview('');
  });

  controls.forEach((control) => control.addEventListener('input', paint));
  confirm.addEventListener('click', () => requestPreview(selection().join(',')));
  get('crop-cancel').addEventListener('click', () => {
    discard();
    input.value = '';
    input.focus();
  });

  canvas.addEventListener('pointerdown', (event) => {
    if (pending || !frame || !event.isPrimary || event.button !== 0) return;
    drag = {pointer: event.pointerId, x: event.clientX, y: event.clientY, crop: selection()};
    canvas.setPointerCapture(event.pointerId);
  });
  canvas.addEventListener('pointermove', (event) => {
    if (!drag || event.pointerId !== drag.pointer) return;
    const [x, y, side] = drag.crop;
    const scale = side / canvas.getBoundingClientRect().width;
    positionX.value = Math.round(x - (event.clientX - drag.x) * scale);
    positionY.value = Math.round(y - (event.clientY - drag.y) * scale);
    paint();
  });
  canvas.addEventListener('lostpointercapture', () => { drag = undefined; });

  draftForm.addEventListener('submit', (event) => {
    if (!input.files.length) return;
    event.preventDefault();
    if (!crop.value || pending) {
      setFeedback('Confirm the crop or cancel the image before saving.');
      return;
    }
    const data = new FormData(draftForm);
    if (event.submitter) data.set(event.submitter.name, event.submitter.value);
    const request = new XMLHttpRequest();
    request.open('POST', draftForm.getAttribute('action'));
    request.setRequestHeader('X-Draft-Upload', '1');
    request.timeout = 35000;
    progress.hidden = false;
    progress.removeAttribute('value');
    setFeedback('Uploading listing image…');
    submitButtons.forEach((button) => { button.disabled = true; });
    editor.inert = true;
    input.disabled = true;

    request.upload.addEventListener('progress', (upload) => {
      if (upload.lengthComputable) progress.value = 100 * upload.loaded / upload.total;
    });
    request.addEventListener('load', () => {
      const destination = request.getResponseHeader('X-Draft-Location');
      if (request.status === 204 && destination?.startsWith('/sell/drafts/')) {
        location.assign(destination);
        return;
      }
      const page = new DOMParser().parseFromString(request.responseText, 'text/html');
      message.textContent = page.querySelector('#seller-errors')?.textContent.trim()
        || (request.status === 503 ? 'Another image is being processed. Please try again.' : 'The draft could not be saved. Check your account session and try again.');
    });
    request.addEventListener('error', () => {
      message.textContent = 'The upload was interrupted. Try again.';
    });
    request.addEventListener('timeout', () => {
      message.textContent = 'The upload timed out. Check your draft before retrying.';
    });
    request.addEventListener('loadend', () => {
      progress.hidden = true;
      editor.inert = false;
      input.disabled = false;
      submitButtons.forEach((button) => { button.disabled = false; });
    });
    request.send(data);
  });
}
