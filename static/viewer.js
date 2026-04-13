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
   const objectsById = buildObjectIndex(step.Objects || []);
   renderFrames(step.Frames || [], objectsById);
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

function buildObjectIndex(objects) {
   const index = {};
   (objects || []).forEach((obj) => {
     index[obj.ID] = obj;
   });
   return index;
}

function renderFrames(frames, objectsById) {
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

     const bindings = renderBindingList(frame.Bindings || [], 'No bindings', objectsById);
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

function renderSliceDiagram(sliceBindings, objectsById) {
   const container = document.createElement('div');
   container.className = 'slice-diagram-container';

   // Collect unique backing arrays and track which vars point to them
   const backingArrays = new Map(); // ID -> { obj, vars: [names] }
   const varToArray = new Map(); // var name -> array ID
   
   sliceBindings.forEach((binding) => {
     const sliceObj = objectsById[binding.Value.ObjectID];
     if (sliceObj && sliceObj.BackingObjectID) {
       const arrayID = sliceObj.BackingObjectID;
       if (!backingArrays.has(arrayID)) {
         const arrayObj = objectsById[arrayID];
         backingArrays.set(arrayID, { obj: arrayObj, vars: [] });
       }
       backingArrays.get(arrayID).vars.push(binding.Name);
       varToArray.set(binding.Name, arrayID);
     }
   });

   // Calculate dimensions
   const varLabelWidth = 50;
   const elementWidth = 40;
   const elementHeight = 40;
   const arrowHeadSize = 8;
   const labelGap = 15;
   const arrayLabelGap = 60;
   const arrayRowHeight = 80;
   const spacing = 15;

   // Max elements to display
   const maxElements = Math.max(...Array.from(backingArrays.values()).map(d => d.obj?.Elements?.length || 0));
   const arrayContentWidth = maxElements * elementWidth + spacing * 2;
   const diagramWidth = varLabelWidth + labelGap + 30 + arrayContentWidth + spacing;
   const diagramHeight = sliceBindings.length * 25 + backingArrays.size * arrayRowHeight + 60;

   const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
   svg.setAttribute('class', 'slice-diagram-svg');
   svg.setAttribute('viewBox', `0 0 ${diagramWidth} ${diagramHeight}`);
   svg.setAttribute('preserveAspectRatio', 'xMinYMin meet');

   let varYPos = 30;
   const arrayPositions = new Map(); // arrayID -> yPosition

   // Draw variable labels and arrows
   Array.from(varToArray.entries()).forEach(([varName, arrayID]) => {
     const varX = 10;
     const varY = varYPos;
     const arrowStartX = varLabelWidth;
     const arrayStartX = varLabelWidth + labelGap + 30;

     // Variable name
     const varText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
     varText.setAttribute('x', varX);
     varText.setAttribute('y', varY + 5);
     varText.setAttribute('class', 'diagram-var-name');
     varText.textContent = varName;
     svg.appendChild(varText);

     // Get array position (compute if first time seeing this array)
     if (!arrayPositions.has(arrayID)) {
       arrayPositions.set(arrayID, Math.max(...Array.from(arrayPositions.values() || [30])) + arrayRowHeight);
     }
     const arrayY = arrayPositions.get(arrayID);
     const arrowY = varY + 2;

     // Arrow line from var to array
     const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
     line.setAttribute('x1', arrowStartX);
     line.setAttribute('y1', arrowY);
     line.setAttribute('x2', arrayStartX - arrowHeadSize - 5);
     line.setAttribute('y2', arrowY);
     line.setAttribute('class', 'diagram-arrow-line');
     svg.appendChild(line);

     // Arrowhead
     const polygon = document.createElementNS('http://www.w3.org/2000/svg', 'polygon');
     polygon.setAttribute('points', `${arrayStartX - arrowHeadSize - 5},${arrowY - 3} ${arrayStartX},${arrowY} ${arrayStartX - arrowHeadSize - 5},${arrowY + 3}`);
     polygon.setAttribute('class', 'diagram-arrowhead');
     svg.appendChild(polygon);

     varYPos += 25;
   });

   // Draw arrays
   backingArrays.forEach((arrayData, arrayID) => {
     const { obj } = arrayData;
     const elements = obj?.Elements || [];
     const arrayY = arrayPositions.get(arrayID);
     const arrayStartX = varLabelWidth + labelGap + 30;

     // Array elements
     elements.forEach((elem, idx) => {
       const elemX = arrayStartX + spacing + idx * elementWidth;
       const elemY = arrayY;

       const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
       rect.setAttribute('x', elemX);
       rect.setAttribute('y', elemY);
       rect.setAttribute('width', elementWidth - 4);
       rect.setAttribute('height', elementHeight);
       rect.setAttribute('class', 'diagram-element');
       svg.appendChild(rect);

       const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
       text.setAttribute('x', elemX + (elementWidth - 4) / 2);
       text.setAttribute('y', elemY + elementHeight / 2 + 5);
       text.setAttribute('class', 'diagram-element-text');
       text.setAttribute('text-anchor', 'middle');
       text.textContent = elem.Display || '?';
       svg.appendChild(text);
     });

     // Array metadata (len, cap)
     const firstVar = arrayData.vars[0];
     const sliceObj = objectsById[sliceBindings.find(b => b.Name === firstVar).Value.ObjectID];
     
     const metaX = arrayStartX + spacing;
     const metaY = arrayY + elementHeight + 20;
     const metaText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
     metaText.setAttribute('x', metaX);
     metaText.setAttribute('y', metaY);
     metaText.setAttribute('class', 'diagram-metadata');
     metaText.textContent = `len=${sliceObj.Len}, cap=${sliceObj.Cap}`;
     svg.appendChild(metaText);
   });

   container.appendChild(svg);
   return container;
}

function renderBindingList(bindings, emptyText, objectsById) {
   const list = document.createElement('div');
   list.className = 'binding-list';

   if (!bindings.length) {
     const empty = document.createElement('div');
     empty.className = 'empty';
     empty.textContent = emptyText;
     list.appendChild(empty);
     return list;
   }

   // Group bindings by whether they're slices
   const sliceBindings = [];
   const otherBindings = [];
   
   bindings.forEach((binding) => {
     const obj = objectsById[binding.Value.ObjectID];
     if (obj && obj.Kind === 'slice') {
       sliceBindings.push(binding);
     } else {
       otherBindings.push(binding);
     }
   });

   // Render slice diagram if there are slices
   if (sliceBindings.length > 0) {
     list.appendChild(renderSliceDiagram(sliceBindings, objectsById));
   }

   // Render other bindings normally
   otherBindings.forEach((binding) => {
     const row = document.createElement('div');
     row.className = 'binding-row';

     const key = document.createElement('span');
     key.className = 'binding-name';
     key.textContent = binding.Name;

     const value = document.createElement('div');
     value.className = 'binding-value';
     value.appendChild(renderValueWithChain(binding.Value, objectsById || {}));

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

function renderValueWithChain(value, objectsById) {
   if (!value) {
     return renderInlineText('nil');
   }

   // For pointer-like types, show the chain: var -> object -> target
   if (value.Kind === 'object' && value.ObjectID) {
     const obj = objectsById[value.ObjectID];
     const container = document.createElement('span');

     // Render the immediate object reference
     container.appendChild(renderObjectRef(value.ObjectID, value.Display || value.ObjectID));

     // For slices, show the backing array
     if (obj && obj.Kind === 'slice' && obj.BackingObjectID) {
       container.appendChild(document.createTextNode(' → '));
       container.appendChild(renderObjectRef(obj.BackingObjectID, 'backing'));
     }

     // For pointers and interfaces, show the target
     if (obj && (obj.Kind === 'pointer' || obj.Kind === 'interface') && obj.Target) {
       container.appendChild(document.createTextNode(' → '));
       container.appendChild(renderValue(obj.Target));
     }

     return container;
   }

   return renderInlineText(value.Display || value.Kind || 'value');
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
