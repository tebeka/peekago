const state = {
  code: '',
  lines: [],
  steps: [],
  index: 0,
};

const elements = {
  codeInput: document.getElementById('code-input'),
  runButton: document.getElementById('run-button'),
  status: document.getElementById('status'),
  prevButton: document.getElementById('prev-button'),
  nextButton: document.getElementById('next-button'),
  stepRange: document.getElementById('step-range'),
  stepLabel: document.getElementById('step-label'),
  currentFile: document.getElementById('current-file'),
  currentLine: document.getElementById('current-line'),
  currentFunction: document.getElementById('current-function'),
  codeView: document.getElementById('code-view'),
  localsView: document.getElementById('locals-view'),
  globalsView: document.getElementById('globals-view'),
};

elements.runButton.addEventListener('click', runTrace);
elements.prevButton.addEventListener('click', () => setStep(state.index - 1));
elements.nextButton.addEventListener('click', () => setStep(state.index + 1));
elements.stepRange.addEventListener('input', (event) => {
  setStep(Number(event.target.value));
});

renderCode([]);
renderStep();

async function runTrace() {
  const code = elements.codeInput.value;
  setStatus('Tracing...');
  setBusy(true);

  try {
    const response = await fetch('/trace', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ code }),
    });

    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || 'trace failed');
    }

    state.code = code;
    state.lines = code.split('\n');
    state.steps = data.steps || [];
    state.index = 0;
    renderCode(state.lines);
    syncControls();
    renderStep();
    setStatus(`Loaded ${state.steps.length} step${state.steps.length === 1 ? '' : 's'}.`);
  } catch (error) {
    state.steps = [];
    state.index = 0;
    syncControls();
    renderStep();
    setStatus(error.message || 'trace failed', true);
  } finally {
    setBusy(false);
  }
}

function setStep(index) {
  if (!state.steps.length) {
    return;
  }

  const maxIndex = state.steps.length - 1;
  state.index = Math.max(0, Math.min(index, maxIndex));
  syncControls();
  renderStep();
}

function syncControls() {
  const hasSteps = state.steps.length > 0;
  elements.prevButton.disabled = !hasSteps || state.index === 0;
  elements.nextButton.disabled = !hasSteps || state.index === state.steps.length - 1;
  elements.stepRange.disabled = !hasSteps;
  elements.stepRange.max = Math.max(0, state.steps.length - 1);
  elements.stepRange.value = state.index;
}

function renderStep() {
  if (!state.steps.length) {
    elements.stepLabel.textContent = 'No trace loaded';
    elements.currentFile.textContent = '-';
    elements.currentLine.textContent = '-';
    elements.currentFunction.textContent = '-';
    renderVars(elements.localsView, {});
    renderVars(elements.globalsView, {});
    highlightLine(0);
    return;
  }

  const step = state.steps[state.index];
  elements.stepLabel.textContent = `Step ${state.index + 1} / ${state.steps.length}`;
  elements.currentFile.textContent = step.File || '-';
  elements.currentLine.textContent = step.Line || '-';
  elements.currentFunction.textContent = step.Function || 'main';
  renderVars(elements.localsView, step.Locals || {});
  renderVars(elements.globalsView, step.Globals || {});
  highlightLine(step.Line);
}

function renderCode(lines) {
  elements.codeView.innerHTML = '';

  if (!lines.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = 'Code will appear here after tracing.';
    elements.codeView.appendChild(empty);
    return;
  }

  lines.forEach((line, index) => {
    const row = document.createElement('div');
    row.className = 'code-line';
    row.dataset.line = String(index + 1);

    const number = document.createElement('span');
    number.className = 'line-number';
    number.textContent = String(index + 1);

    const text = document.createElement('code');
    text.className = 'line-text';
    text.textContent = line || ' ';

    row.append(number, text);
    elements.codeView.appendChild(row);
  });
}

function highlightLine(lineNumber) {
  const rows = elements.codeView.querySelectorAll('.code-line');
  rows.forEach((row) => {
    row.classList.toggle('active', Number(row.dataset.line) === lineNumber);
  });

  const activeRow = elements.codeView.querySelector('.code-line.active');
  if (activeRow) {
    activeRow.scrollIntoView({ block: 'nearest' });
  }
}

function renderVars(container, vars) {
  const names = Object.keys(vars).sort();
  container.innerHTML = '';

  if (!names.length) {
    container.className = 'vars-view empty';
    container.textContent = 'No variables';
    return;
  }

  container.className = 'vars-view';

  names.forEach((name) => {
    const row = document.createElement('div');
    row.className = 'var-row';

    const key = document.createElement('span');
    key.className = 'var-name';
    key.textContent = name;

    const value = document.createElement('code');
    value.className = 'var-value';
    value.textContent = vars[name];

    row.append(key, value);
    container.appendChild(row);
  });
}

function setStatus(message, isError = false) {
  elements.status.textContent = message;
  elements.status.classList.toggle('error', isError);
}

function setBusy(isBusy) {
  elements.runButton.disabled = isBusy;
}
