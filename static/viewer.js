const viewerState = {
  storageKey: 'peek-a-go:last-trace',
  steps: [],
  index: 0,
  activeLineHandle: null,
};

const viewerElements = {
  editorInput: document.getElementById('viewer-editor'),
  prevButton: document.getElementById('prev-button'),
  nextButton: document.getElementById('next-button'),
  stepRange: document.getElementById('step-range'),
  stepLabel: document.getElementById('step-label'),
  currentFile: document.getElementById('current-file'),
  currentLine: document.getElementById('current-line'),
  currentFunction: document.getElementById('current-function'),
  framesView: document.getElementById('frames-view'),
  objectsView: document.getElementById('objects-view'),
  status: document.getElementById('viewer-status'),
};

const viewerEditor = CodeMirror.fromTextArea(viewerElements.editorInput, {
  mode: 'text/x-go',
  lineNumbers: true,
  readOnly: true,
  lineWrapping: false,
  viewportMargin: Infinity,
});

viewerEditor.setSize('100%', 720);

viewerElements.prevButton.addEventListener('click', () => setStep(viewerState.index - 1));
viewerElements.nextButton.addEventListener('click', () => setStep(viewerState.index + 1));
viewerElements.stepRange.addEventListener('input', (event) => {
  setStep(Number(event.target.value));
});

loadTrace();

function loadTrace() {
  const raw = sessionStorage.getItem(viewerState.storageKey);
  if (!raw) {
    setViewerStatus('No trace found. Go back and run the tracer first.', true);
    viewerEditor.setValue('// No trace loaded.');
    syncControls();
    return;
  }

  try {
    const payload = JSON.parse(raw);
    viewerState.steps = payload.steps || [];
    viewerState.index = 0;
    viewerEditor.setValue(payload.code || '');
    syncControls();
    renderStep();
    setViewerStatus(`Loaded ${viewerState.steps.length} step${viewerState.steps.length === 1 ? '' : 's'}.`);
  } catch (error) {
    setViewerStatus('Stored trace data is invalid. Go back and trace again.', true);
    viewerEditor.setValue('// Invalid trace data.');
    syncControls();
  }
}

function setStep(index) {
  if (!viewerState.steps.length) {
    return;
  }

  const maxIndex = viewerState.steps.length - 1;
  viewerState.index = Math.max(0, Math.min(index, maxIndex));
  syncControls();
  renderStep();
}

function syncControls() {
  const hasSteps = viewerState.steps.length > 0;
  viewerElements.prevButton.disabled = !hasSteps || viewerState.index === 0;
  viewerElements.nextButton.disabled = !hasSteps || viewerState.index === viewerState.steps.length - 1;
  viewerElements.stepRange.disabled = !hasSteps;
  viewerElements.stepRange.max = Math.max(0, viewerState.steps.length - 1);
  viewerElements.stepRange.value = viewerState.index;
}

function renderStep() {
  if (!viewerState.steps.length) {
    viewerElements.stepLabel.textContent = 'No trace loaded';
    viewerElements.currentFile.textContent = '-';
    viewerElements.currentLine.textContent = '-';
    viewerElements.currentFunction.textContent = '-';
    renderFrames([]);
    renderObjects([]);
    highlightEditorLine(0);
    return;
  }

  const step = viewerState.steps[viewerState.index];
  viewerElements.stepLabel.textContent = `Step ${viewerState.index + 1} / ${viewerState.steps.length}`;
  viewerElements.currentFile.textContent = step.File || '-';
  viewerElements.currentLine.textContent = step.Line || '-';
  viewerElements.currentFunction.textContent = step.Function || 'main';
  renderFrames(step.Frames || []);
  renderObjects(step.Objects || []);
  highlightEditorLine(step.Line);
}

function highlightEditorLine(lineNumber) {
  if (viewerState.activeLineHandle) {
    viewerEditor.removeLineClass(viewerState.activeLineHandle, 'background', 'cm-active-line');
  }

  if (!lineNumber) {
    viewerState.activeLineHandle = null;
    return;
  }

  viewerState.activeLineHandle = viewerEditor.addLineClass(lineNumber - 1, 'background', 'cm-active-line');
  viewerEditor.scrollIntoView({ line: Math.max(lineNumber - 1, 0), ch: 0 }, 120);
}

function renderFrames(frames) {
  viewerElements.framesView.innerHTML = '';

  if (!frames.length) {
    viewerElements.framesView.className = 'stack-view empty';
    viewerElements.framesView.textContent = 'No frames';
    return;
  }

  viewerElements.framesView.className = 'stack-view';
  frames.forEach((frame) => {
    const card = document.createElement('section');
    card.className = 'frame-card';
    if (frame.IsCurrent) {
      card.classList.add('current-frame');
    }

    const title = document.createElement('div');
    title.className = 'frame-title';
    title.textContent = frame.Name || frame.Kind;

    const kind = document.createElement('div');
    kind.className = 'frame-kind';
    kind.textContent = frame.Kind;

    const bindings = renderBindingList(frame.Bindings || [], 'No bindings');
    card.append(title, kind, bindings);
    viewerElements.framesView.appendChild(card);
  });
}

function renderObjects(objects) {
  viewerElements.objectsView.innerHTML = '';

  if (!objects.length) {
    viewerElements.objectsView.className = 'objects-view empty';
    viewerElements.objectsView.textContent = 'No objects';
    return;
  }

  viewerElements.objectsView.className = 'objects-view';
  objects.forEach((object) => {
    const card = document.createElement('section');
    card.className = 'object-card';
    card.id = objectDomID(object.ID);

    const title = document.createElement('div');
    title.className = 'object-title';
    title.textContent = object.Label || object.Type || object.Kind;

    const meta = document.createElement('div');
    meta.className = 'object-meta';
    meta.textContent = objectMeta(object);

    const body = document.createElement('div');
    body.className = 'object-body';
    renderObjectBody(body, object);

    card.append(title, meta, body);
    viewerElements.objectsView.appendChild(card);
  });
}

function renderObjectBody(container, object) {
  if (object.Kind === 'slice') {
    container.appendChild(renderMetaRow('length', renderInlineText(String(object.Len ?? 0))));
    container.appendChild(renderMetaRow('capacity', renderInlineText(String(object.Cap ?? 0))));
    container.appendChild(renderMetaRow('backing array', object.BackingObjectID ? renderObjectRef(object.BackingObjectID, 'array object') : renderInlineText('nil')));
    return;
  }

  if (object.Kind === 'array') {
    container.appendChild(renderValueList(object.Elements || []));
    return;
  }

  if (object.Kind === 'struct') {
    container.appendChild(renderBindingList(object.Fields || [], 'No fields'));
    return;
  }

  if (object.Kind === 'map') {
    const list = document.createElement('div');
    list.className = 'binding-list';
    (object.Entries || []).forEach((entry, index) => {
      const row = document.createElement('div');
      row.className = 'binding-row';

      const key = document.createElement('span');
      key.className = 'binding-name';
      key.textContent = `entry ${index}`;

      const value = document.createElement('div');
      value.className = 'binding-value';
      value.append(renderValue(entry.Key), document.createTextNode(' -> '), renderValue(entry.Value));

      row.append(key, value);
      list.appendChild(row);
    });
    container.appendChild(list);
    return;
  }

  if (object.Kind === 'pointer' || object.Kind === 'interface') {
    container.appendChild(renderMetaRow('target', object.Target ? renderValue(object.Target) : renderInlineText('nil')));
    return;
  }

  const empty = document.createElement('div');
  empty.className = 'empty';
  empty.textContent = 'No renderer';
  container.appendChild(empty);
}

function renderBindingList(bindings, emptyText) {
  const list = document.createElement('div');
  list.className = 'binding-list';

  if (!bindings.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = emptyText;
    list.appendChild(empty);
    return list;
  }

  bindings.forEach((binding) => {
    const row = document.createElement('div');
    row.className = 'binding-row';

    const key = document.createElement('span');
    key.className = 'binding-name';
    key.textContent = binding.Name;

    const value = document.createElement('div');
    value.className = 'binding-value';
    value.appendChild(renderValue(binding.Value));

    row.append(key, value);
    list.appendChild(row);
  });

  return list;
}

function renderValueList(values) {
  const list = document.createElement('div');
  list.className = 'value-list';

  values.forEach((value, index) => {
    const item = document.createElement('div');
    item.className = 'value-chip';
    item.append(document.createTextNode(`[${index}] `), renderValue(value));
    list.appendChild(item);
  });

  return list;
}

function renderMetaRow(label, valueNode) {
  const row = document.createElement('div');
  row.className = 'meta-row';

  const key = document.createElement('span');
  key.className = 'meta-row-label';
  key.textContent = label;

  const value = document.createElement('div');
  value.className = 'meta-row-value';
  value.appendChild(valueNode);

  row.append(key, value);
  return row;
}

function renderValue(value) {
  if (!value) {
    return renderInlineText('nil');
  }

  if (value.Kind === 'object' && value.ObjectID) {
    return renderObjectRef(value.ObjectID, value.Display || value.ObjectID);
  }

  return renderInlineText(value.Display || value.Kind || 'value');
}

function renderInlineText(text) {
  const code = document.createElement('code');
  code.className = 'inline-value';
  code.textContent = text;
  return code;
}

function renderObjectRef(objectID, label) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'object-ref';
  button.textContent = label;
  button.addEventListener('click', () => {
    const target = document.getElementById(objectDomID(objectID));
    if (target) {
      target.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      target.classList.add('object-card-pulse');
      window.setTimeout(() => target.classList.remove('object-card-pulse'), 900);
    }
  });
  return button;
}

function objectDomID(objectID) {
  return `object-${objectID.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function objectMeta(object) {
  const parts = [object.Kind];
  if (object.Type) {
    parts.push(object.Type);
  }
  return parts.join(' · ');
}

function setViewerStatus(message, isError = false) {
  viewerElements.status.textContent = message;
  viewerElements.status.classList.toggle('error', isError);
}
