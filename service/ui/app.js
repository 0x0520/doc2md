(function () {
  'use strict';

  var $ = function (id) { return document.getElementById(id); };

  var list = $('list');
  var stage = $('stage');
  var tabsBar = document.querySelector('.tabs');
  var tabs = Array.prototype.slice.call(document.querySelectorAll('.tab'));
  var foot = document.querySelector('.foot');
  var picker = $('picker');
  var verbatim = $('verbatim');

  var docs = [];
  var picked = null;
  var pane = 'md';
  var seq = 0;
  var maxBytes = 0;

  fetch('api/config').then(function (r) { return r.json(); }).then(function (cfg) {
    maxBytes = cfg.maxBytes || 0;
    if (Array.isArray(cfg.formats) && cfg.formats.length) {
      picker.setAttribute('accept', cfg.formats.join(','));
    }
  }).catch(function () {});

  $('add').addEventListener('click', function () { picker.click(); });
  picker.addEventListener('change', function () {
    add(picker.files);
    picker.value = '';
  });

  var drop = $('drop');
  var depth = 0;
  var paint = function () { drop.classList.toggle('is-over', depth > 0); };
  drop.addEventListener('dragenter', function (e) { e.preventDefault(); depth++; paint(); });
  drop.addEventListener('dragover', function (e) { e.preventDefault(); });
  drop.addEventListener('dragleave', function () { depth = Math.max(0, depth - 1); paint(); });
  drop.addEventListener('drop', function (e) {
    e.preventDefault();
    depth = 0;
    paint();
    if (e.dataTransfer && e.dataTransfer.files) add(e.dataTransfer.files);
  });

  function add(files) {
    Array.prototype.slice.call(files || []).forEach(function (file) {
      var doc = { id: 'd' + (++seq), file: file, name: file.name, size: file.size, state: 'running', result: null, error: '' };
      docs.push(doc);
      if (!picked) picked = doc.id;
      convert(doc);
    });
    render();
  }

  function convert(doc) {
    doc.state = 'running';
    doc.error = '';
    doc.result = null;
    render();

    if (maxBytes && doc.size > maxBytes) {
      doc.state = 'refused';
      doc.error = 'The document exceeds the limit of ' + bytes(maxBytes) + '.';
      render();
      return;
    }

    var body = new FormData();
    body.append('file', doc.file, doc.name);
    if (verbatim.checked) body.append('verbatim', '1');

    fetch('api/convert', { method: 'POST', body: body }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (data) {
        return { status: r.status, data: data };
      });
    }).then(function (res) {
      if (res.status === 200) {
        doc.state = 'done';
        doc.result = res.data;
      } else {
        doc.state = res.status === 415 ? 'refused' : 'failed';
        doc.error = (res.data && res.data.error) || ('The service answered with status ' + res.status + '.');
      }
      render();
    }).catch(function (err) {
      doc.state = 'failed';
      doc.error = 'The service is unreachable: ' + err.message;
      render();
    });
  }

  $('again').addEventListener('click', function () {
    docs.forEach(function (d) { if (d.state !== 'running') convert(d); });
  });

  function render() {
    renderList();
    renderTally();
    renderStage();
    $('again').disabled = docs.length === 0;
  }

  function renderList() {
    list.textContent = '';
    docs.forEach(function (doc) {
      var row = clone('tpl-file').firstElementChild;
      row.setAttribute('data-doc', doc.id);
      row.classList.toggle('is-picked', doc.id === picked);
      row.setAttribute('aria-selected', doc.id === picked ? 'true' : 'false');
      row.querySelector('.fmt').textContent = ext(doc.name) || 'file';

      var name = row.querySelector('.file-name');
      name.textContent = doc.name;
      name.title = doc.name;

      var meta = [bytes(doc.size)];
      if (doc.state === 'done' && doc.result) {
        meta.push(lines(doc.result.markdown) + ' lines');
        meta.push(duration(doc.result.ms));
      }
      row.querySelector('.file-size').textContent = meta.join(' · ');

      var badge = row.querySelector('.badge');
      badge.textContent = stateLabel(doc.state);
      badge.classList.add('badge-' + doc.state);

      if (doc.error) {
        var reason = row.querySelector('.file-error');
        reason.textContent = doc.error;
        reason.hidden = false;
      }

      row.addEventListener('click', function () { pick(doc.id); });
      row.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(doc.id); return; }
        if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
        e.preventDefault();
        var next = docs[docs.indexOf(doc) + (e.key === 'ArrowDown' ? 1 : -1)];
        if (next) { pick(next.id); focusRow(next.id); }
      });
      list.appendChild(row);
    });
  }

  function renderTally() {
    var total = docs.reduce(function (n, d) { return n + d.size; }, 0);
    $('tallySize').textContent = docs.length ? docs.length + ' · ' + bytes(total) : '—';
    $('tallyDone').textContent = count('done');
    $('tallyRunning').textContent = count('running');
    $('tallyFailed').textContent = count('refused') + count('failed');
  }

  function count(state) {
    return docs.filter(function (d) { return d.state === state; }).length;
  }

  function renderStage() {
    var doc = current();
    stage.textContent = '';

    if (!doc || doc.state !== 'done') {
      tabsBar.hidden = true;
      foot.hidden = true;
      stage.appendChild(messageFor(doc));
      return;
    }

    var res = doc.result;
    var images = res.images || [];
    var view = clone('tpl-view');

    view.querySelector('.source').textContent = res.markdown;

    var figs = view.querySelector('.figs');
    images.forEach(function (img) {
      var fig = clone('tpl-fig');
      var el = fig.querySelector('img');
      el.src = 'data:' + mime(img.ext) + ';base64,' + img.dataBase64;
      el.alt = 'Image ' + img.seq;
      fig.querySelector('.seq').textContent = 'Image ' + img.seq;
      fig.querySelector('.sub').textContent = [
        img.page ? 'page ' + img.page : null,
        img.ext,
        bytes(Math.floor(img.dataBase64.length * 3 / 4))
      ].filter(Boolean).join(' · ');
      figs.appendChild(fig);
    });

    var marks = (res.markdown.match(/\[Image: \d+\]/g) || []).length;
    if (!images.length) {
      var note = view.querySelector('.note');
      note.hidden = false;
      note.textContent = marks
        ? 'The text refers to ' + marks + ' image(s), but none were returned.'
        : 'This document contains no extractable raster images. Vector drawings are not rasterised and no OCR is performed.';
    }
    $('imgCount').textContent = String(images.length);

    var metrics = view.querySelector('.metrics');
    metric(metrics, 'Headings', headings(res.markdown), 'levels 1–3');
    metric(metrics, 'Tables', tables(res.markdown), 'in the Markdown');
    metric(metrics, 'Images', String(images.length), marks + ' marker(s) in the text');
    metric(metrics, 'Lines', lines(res.markdown), duration(res.ms));

    var kv = view.querySelector('.kv');
    pair(kv, 'Read as', res.format ? res.format.toUpperCase() : 'detected from content',
      'The format comes from the file extension; without a known extension the leading bytes decide.');
    pair(kv, 'Keep everything', res.verbatim ? 'yes' : 'no',
      res.verbatim
        ? 'Cover page, table of contents and running headers are kept.'
        : 'For a PDF, the cover page, table of contents, running headers and footers and per-page decorations are dropped.');
    pair(kv, 'Size', bytes(res.bytes) + ' → ' + bytes(new Blob([res.markdown]).size) + ' Markdown', '');

    stage.appendChild(view);
    tabsBar.hidden = false;
    foot.hidden = false;
    $('footNote').textContent = doc.name;
    showPane(pane);
  }

  function messageFor(doc) {
    var msg = clone('tpl-message');
    var title = msg.querySelector('h2');
    var text = msg.querySelector('p');
    if (!doc) {
      title.textContent = 'No document yet';
      text.textContent = 'Add a document or drop it here. It is converted right away by this service; the file does not leave it.';
      return msg;
    }
    if (doc.state === 'running') {
      title.textContent = doc.name;
      text.textContent = 'Converting…';
      return msg;
    }
    title.textContent = doc.state === 'refused' ? 'This document was not converted' : 'Conversion failed';
    text.textContent = doc.error;
    var remove = msg.querySelector('[data-act="remove"]');
    remove.hidden = false;
    remove.addEventListener('click', function () {
      docs = docs.filter(function (d) { return d !== doc; });
      if (picked === doc.id) picked = docs.length ? docs[0].id : null;
      render();
    });
    return msg;
  }

  function metric(grid, label, value, sub) {
    var el = clone('tpl-metric');
    el.querySelector('.label').textContent = label;
    el.querySelector('.value').textContent = value;
    el.querySelector('.sub').textContent = sub;
    grid.appendChild(el);
  }

  function pair(kv, term, value, caption) {
    var dt = document.createElement('dt');
    dt.textContent = term;
    var dd = document.createElement('dd');
    dd.textContent = value;
    if (caption) {
      var c = document.createElement('span');
      c.className = 'muted';
      c.textContent = caption;
      dd.appendChild(c);
    }
    kv.appendChild(dt);
    kv.appendChild(dd);
  }

  function pick(id) { picked = id; pane = 'md'; render(); }

  function focusRow(id) {
    var row = list.querySelector('[data-doc="' + id + '"]');
    if (row) row.focus();
  }

  function current() {
    return docs.filter(function (d) { return d.id === picked; })[0] || null;
  }

  function showPane(name) {
    pane = name;
    Array.prototype.slice.call(stage.querySelectorAll('[data-pane]')).forEach(function (p) {
      p.hidden = p.getAttribute('data-pane') !== name;
    });
    tabs.forEach(function (t) {
      var on = t.getAttribute('data-tab') === name;
      t.classList.toggle('is-active', on);
      t.setAttribute('aria-selected', on ? 'true' : 'false');
    });
  }

  tabs.forEach(function (t) {
    t.addEventListener('click', function () { showPane(t.getAttribute('data-tab')); });
  });

  $('copy').addEventListener('click', function () {
    var doc = current();
    if (!doc || !doc.result) return;
    var btn = $('copy');
    var done = function () {
      btn.textContent = 'Copied';
      setTimeout(function () { btn.textContent = 'Copy Markdown'; }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(doc.result.markdown).then(done, function () { fallbackCopy(doc.result.markdown, done); });
    } else {
      fallbackCopy(doc.result.markdown, done);
    }
  });

  function fallbackCopy(text, done) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand('copy');
      done();
    } catch (e) {}
    document.body.removeChild(ta);
  }

  $('download').addEventListener('click', function () {
    var doc = current();
    if (!doc || !doc.result) return;
    var blob = new Blob([doc.result.markdown], { type: 'text/markdown;charset=utf-8' });
    var a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = doc.name.replace(/\.[^.]+$/, '') + '.md';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
  });

  function clone(id) {
    return document.getElementById(id).content.cloneNode(true);
  }

  function ext(name) {
    var m = /\.([A-Za-z0-9]+)$/.exec(name || '');
    return m ? m[1].toLowerCase() : '';
  }

  function bytes(n) {
    if (!n && n !== 0) return '—';
    if (n < 1024) return n + ' B';
    if (n < 1024 * 1024) return (n / 1024).toFixed(0) + ' KB';
    return (n / 1024 / 1024).toFixed(1) + ' MB';
  }

  function duration(ms) {
    if (ms === undefined || ms === null) return '';
    if (ms < 1000) return ms + ' ms';
    return (ms / 1000).toFixed(2) + ' s';
  }

  function lines(md) { return String((md || '').split('\n').length); }

  function headings(md) {
    return String(((md || '').match(/^#{1,3} \S/gm) || []).length);
  }

  function tables(md) {
    return String(((md || '').match(/^\|[\s\-:|]+\|\s*$/gm) || []).length);
  }

  function mime(e) {
    switch ((e || '').toLowerCase()) {
      case 'png': return 'image/png';
      case 'jpg':
      case 'jpeg': return 'image/jpeg';
      case 'gif': return 'image/gif';
      case 'webp': return 'image/webp';
      case 'svg': return 'image/svg+xml';
      case 'tif':
      case 'tiff': return 'image/tiff';
      default: return 'application/octet-stream';
    }
  }

  function stateLabel(state) {
    return { done: 'Done', running: 'Running', refused: 'Refused', failed: 'Failed' }[state] || state;
  }

  render();
})();
