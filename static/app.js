const state = {
  storageKey: 'peek-a-go:last-trace',
};

const elements = {
  editorInput: document.getElementById('editor-input'),
  runButton: document.getElementById('run-button'),
  buttonLabel: document.querySelector('#run-button .button-label'),
  spinner: document.querySelector('#run-button .spinner'),
  status: document.getElementById('status'),
};

const editor = CodeMirror.fromTextArea(elements.editorInput, {
  mode: 'text/x-go',
  lineNumbers: true,
  indentUnit: 2,
  tabSize: 2,
  indentWithTabs: true,
  lineWrapping: false,
  viewportMargin: Infinity,
  extraKeys: {
    Tab(cm) {
      if (cm.somethingSelected()) {
        cm.indentSelection('add');
        return;
      }

      cm.execCommand('insertSoftTab');
    },
    'Shift-Tab'(cm) {
      cm.indentSelection('subtract');
    },
  },
});

editor.setSize('100%', 640);

elements.runButton.addEventListener('click', runTrace);

async function runTrace() {
  const code = editor.getValue();
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

    sessionStorage.setItem(
      state.storageKey,
      JSON.stringify({
        code,
        steps: data.steps || [],
      }),
    );
    window.location.href = '/viewer.html';
  } catch (error) {
    setStatus(error.message || 'trace failed', true);
  } finally {
    setBusy(false);
  }
}

function setStatus(message, isError = false) {
  elements.status.textContent = message;
  elements.status.classList.toggle('error', isError);
}

function setBusy(isBusy) {
  elements.runButton.disabled = isBusy;
  editor.setOption('readOnly', isBusy);
  elements.runButton.classList.toggle('loading', isBusy);
  elements.buttonLabel.textContent = isBusy ? 'Tracing...' : 'Trace and Open Viewer';
  elements.spinner.setAttribute('aria-hidden', String(!isBusy));
}
